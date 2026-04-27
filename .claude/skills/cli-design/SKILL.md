---
name: cli-design
description: Dart CLI/Build tooling patterns — package:args ArgParser, CommandRunner hierarchy, subcommands (watch, build, export, init), ANSI colorized output, progress indicators, error formatting, help text, exit codes, signal handling, env vars, shell completion, and CLI testing
---

# Dart CLI Design & Build Tooling

Production-quality CLI application design in Dart 3.x. Covers `package:args` for argument parsing, CommandRunner for hierarchical command structures, ANSI escape codes for terminal styling, progress indicators for long-running operations, structured error output with actionable remediation, help text generation, exit codes, POSIX signal handling, stdin/stdout/stderr discipline, environment variable integration, shell completion generation, and testing strategies with process_run and stdout capture.

## Table of Contents

1. [ArgParser and ArgResults API](#argparser-and-argresults-api)
2. [CommandRunner Pattern](#commandrunner-pattern)
3. [Subcommand Implementation](#subcommand-implementation)
4. [Flag and Option Parsing](#flag-and-option-parsing)
5. [ANSI Escape Codes for Colorized Output](#ansi-escape-codes-for-colorized-output)
6. [Progress Indicators](#progress-indicators)
7. [Error Formatting with Remediation](#error-formatting-with-remediation)
8. [Help Text Generation](#help-text-generation)
9. [Exit Codes and Signal Handling](#exit-codes-and-signal-handling)
10. [stdin stdout stderr Usage](#stdin-stdout-stderr-usage)
11. [Environment Variable Integration](#environment-variable-integration)
12. [Shell Completion Generation](#shell-completion-generation)
13. [CLI Testing Strategies](#cli-testing-strategies)
14. [Best Practices](#best-practices)
15. [Anti-Patterns](#anti-patterns)
16. [Sources & References](#sources--references)

---

## ArgParser and ArgResults API

`package:args` provides `ArgParser` for declaring expected flags, options, and positional arguments, and `ArgResults` for reading parsed values in a type-safe manner.

### Core Concepts

- **Flags** are boolean switches: `--verbose`, `--no-color`. They default to `false` unless `defaultsTo` is set.
- **Options** take a string value: `--output=build/`, `--format json`. They can have `allowed` value lists, `defaultsTo`, and abbreviations.
- **Multi-value options** accept repeated occurrences: `--define=FOO --define=BAR` produces `['FOO', 'BAR']`.
- **Commands** create sub-parsers scoped to a named subcommand.
- **Rest arguments** are positional arguments captured after all flags and options have been consumed. Access them via `argResults.rest`.

### ArgParser Setup

```dart
import 'dart:io';
import 'package:args/args.dart';

/// Build a top-level ArgParser for a CLI tool called `dartool`.
ArgParser buildRootParser() {
  final parser = ArgParser(usageLineLength: 80);

  // Global flags available to all commands.
  parser.addFlag(
    'verbose',
    abbr: 'v',
    help: 'Enable verbose logging to stderr.',
    defaultsTo: false,
    negatable: false,
  );

  parser.addFlag(
    'color',
    help: 'Colorize terminal output.',
    defaultsTo: stdout.hasTerminal,
    negatable: true, // allows --no-color
  );

  parser.addOption(
    'config',
    abbr: 'c',
    help: 'Path to the configuration file.',
    valueHelp: 'path',
    defaultsTo: 'dartool.yaml',
  );

  parser.addOption(
    'log-level',
    help: 'Minimum log level.',
    allowed: ['debug', 'info', 'warn', 'error'],
    allowedHelp: {
      'debug': 'Show all messages including internal traces.',
      'info': 'Show informational messages and above.',
      'warn': 'Show warnings and errors only.',
      'error': 'Show errors only.',
    },
    defaultsTo: 'info',
  );

  parser.addMultiOption(
    'define',
    abbr: 'D',
    help: 'Define a key=value variable passed to the build.',
    valueHelp: 'key=value',
  );

  return parser;
}

/// Parse arguments and handle --help before dispatching.
ArgResults parseRootArgs(List<String> args) {
  final parser = buildRootParser();
  try {
    final results = parser.parse(args);
    return results;
  } on FormatException catch (e) {
    stderr.writeln('Error: ${e.message}');
    stderr.writeln();
    stderr.writeln('Usage: dartool [options] <command> [arguments]');
    stderr.writeln(parser.usage);
    exit(64); // EX_USAGE from sysexits.h
  }
}
```

### Reading ArgResults

`ArgResults` provides `[]` operator access, typed getters via generics, `.rest` for positional arguments, `.command` for the matched subcommand, and `.wasParsed()` to distinguish explicit user input from defaults.

Key patterns:
- `results['verbose'] as bool` -- reads a flag.
- `results['config'] as String` -- reads a single option.
- `results['define'] as List<String>` -- reads a multi option.
- `results.wasParsed('config')` -- returns `true` only if the user explicitly supplied `--config`.
- `results.rest` -- positional arguments after flags.
- `results.command` -- nested `ArgResults` for matched subcommand.

---

## CommandRunner Pattern

`package:args` provides `CommandRunner<T>` as the standard Dart pattern for building CLI tools with hierarchical subcommands. Each `Command<T>` declares its own argument parser, description, and `run()` method.

### Architecture

```
dartool (CommandRunner)
  |-- init (Command)
  |-- build (Command)
  |     |-- build web (Command)  -- nested subcommand
  |     |-- build ios (Command)
  |-- watch (Command)
  |-- export (Command)
        |-- export pdf (Command)
        |-- export csv (Command)
```

`CommandRunner` automatically generates help text, routes to the correct command, validates arguments, and produces formatted usage output when `--help` is passed.

### Implementation

```dart
import 'dart:async';
import 'dart:io';
import 'package:args/command_runner.dart';

/// Entry point for the `dartool` CLI.
///
/// The generic parameter [int] is the return type of each command's run().
/// Using int lets commands signal exit codes back to the runner.
class DartoolRunner extends CommandRunner<int> {
  DartoolRunner()
      : super(
          'dartool',
          'A CLI build tool for Dart projects.\n\n'
              'Run "dartool help <command>" for more information about a command.',
        ) {
    // Register global options on the runner's argParser.
    argParser.addFlag(
      'verbose',
      abbr: 'v',
      help: 'Enable verbose logging.',
      negatable: false,
    );
    argParser.addFlag(
      'color',
      help: 'Colorize output (auto-detected from terminal).',
      defaultsTo: stdout.hasTerminal,
      negatable: true,
    );
    argParser.addOption(
      'config',
      abbr: 'c',
      help: 'Path to configuration file.',
      valueHelp: 'path',
      defaultsTo: 'dartool.yaml',
    );

    // Register top-level commands.
    addCommand(InitCommand());
    addCommand(BuildCommand());
    addCommand(WatchCommand());
    addCommand(ExportCommand());
  }

  @override
  Future<int> run(Iterable<String> args) async {
    try {
      final result = await super.run(args);
      return result ?? 0;
    } on UsageException catch (e) {
      stderr.writeln('${_red('Error:')} ${e.message}');
      stderr.writeln();
      stderr.writeln(e.usage);
      return 64; // EX_USAGE
    } on FormatException catch (e) {
      stderr.writeln('${_red('Error:')} ${e.message}');
      return 64;
    } catch (e, st) {
      stderr.writeln('${_red('Unexpected error:')} $e');
      if (globalResults?['verbose'] == true) {
        stderr.writeln(st);
      }
      return 70; // EX_SOFTWARE
    }
  }

  String _red(String text) => '\x1B[31m$text\x1B[0m';
}

/// bin/dartool.dart
Future<void> main(List<String> args) async {
  final exitCode = await DartoolRunner().run(args);
  exit(exitCode);
}
```

### Accessing Global Options from Commands

Every `Command` has a `globalResults` getter that returns the `ArgResults` from the runner's top-level parser. Use this to read global flags like `--verbose` from within any command:

```dart
bool get verbose => globalResults?['verbose'] as bool? ?? false;
bool get useColor => globalResults?['color'] as bool? ?? stdout.hasTerminal;
String get configPath => globalResults?['config'] as String? ?? 'dartool.yaml';
```

---

## Subcommand Implementation

Each subcommand is a class extending `Command<int>`. Below are four production subcommands that cover initialization, compilation, file watching, and data export.

### InitCommand

```dart
import 'dart:io';
import 'package:args/command_runner.dart';
import 'package:path/path.dart' as p;

class InitCommand extends Command<int> {
  @override
  final String name = 'init';

  @override
  final String description = 'Initialize a new dartool project in the current '
      'or specified directory.';

  @override
  final String invocation = 'dartool init [directory]';

  InitCommand() {
    argParser.addFlag(
      'force',
      abbr: 'f',
      help: 'Overwrite existing configuration files.',
      negatable: false,
    );
    argParser.addOption(
      'template',
      abbr: 't',
      help: 'Project template to scaffold from.',
      allowed: ['default', 'minimal', 'monorepo'],
      defaultsTo: 'default',
    );
  }

  @override
  Future<int> run() async {
    final force = argResults!['force'] as bool;
    final template = argResults!['template'] as String;
    final targetDir = argResults!.rest.isEmpty
        ? Directory.current.path
        : p.normalize(argResults!.rest.first);

    final configFile = File(p.join(targetDir, 'dartool.yaml'));
    if (configFile.existsSync() && !force) {
      stderr.writeln(
        'Error: dartool.yaml already exists in $targetDir.\n'
        'Use --force to overwrite.',
      );
      return 1;
    }

    // Scaffold project files based on template.
    final dir = Directory(targetDir);
    if (!dir.existsSync()) {
      dir.createSync(recursive: true);
    }

    await configFile.writeAsString(_templateContent(template));
    stdout.writeln('Initialized dartool project in $targetDir '
        '(template: $template)');
    return 0;
  }

  String _templateContent(String template) => switch (template) {
        'minimal' => 'name: my_project\nminimal: true\n',
        'monorepo' => 'name: my_project\nworkspaces:\n  - packages/*\n',
        _ => 'name: my_project\nbuild:\n  output: build/\n',
      };
}
```

### BuildCommand with Nested Subcommands

```dart
class BuildCommand extends Command<int> {
  @override
  final String name = 'build';

  @override
  final String description = 'Compile the project for a target platform.';

  BuildCommand() {
    argParser.addOption(
      'output',
      abbr: 'o',
      help: 'Output directory for compiled artifacts.',
      valueHelp: 'dir',
      defaultsTo: 'build/',
    );
    argParser.addFlag(
      'release',
      help: 'Build in release mode with optimizations.',
      negatable: false,
    );
    argParser.addMultiOption(
      'define',
      abbr: 'D',
      help: 'Compile-time variable definitions.',
      valueHelp: 'key=value',
    );

    // Nested subcommands for platform-specific builds.
    addSubcommand(BuildWebCommand());
    addSubcommand(BuildIosCommand());
  }

  @override
  Future<int> run() async {
    // If no subcommand is given, perform a default build.
    final output = argResults!['output'] as String;
    final release = argResults!['release'] as bool;
    final defines = argResults!['define'] as List<String>;

    stdout.writeln('Building project...');
    stdout.writeln('  Output:  $output');
    stdout.writeln('  Mode:    ${release ? 'release' : 'debug'}');
    if (defines.isNotEmpty) {
      stdout.writeln('  Defines: ${defines.join(', ')}');
    }

    // ... actual build logic ...
    return 0;
  }
}

class BuildWebCommand extends Command<int> {
  @override
  final String name = 'web';

  @override
  final String description = 'Build for web deployment (dart2js/dart2wasm).';

  BuildWebCommand() {
    argParser.addOption(
      'compiler',
      help: 'Web compiler backend.',
      allowed: ['dart2js', 'dart2wasm'],
      defaultsTo: 'dart2js',
    );
    argParser.addFlag(
      'minify',
      help: 'Minify JavaScript output.',
      defaultsTo: true,
      negatable: true,
    );
  }

  @override
  Future<int> run() async {
    final compiler = argResults!['compiler'] as String;
    final minify = argResults!['minify'] as bool;
    stdout.writeln('Building for web with $compiler '
        '(minify: $minify)...');
    // ... web build implementation ...
    return 0;
  }
}

class BuildIosCommand extends Command<int> {
  @override
  final String name = 'ios';

  @override
  final String description = 'Build for iOS (requires macOS and Xcode).';

  @override
  Future<int> run() async {
    if (!Platform.isMacOS) {
      stderr.writeln('Error: iOS builds require macOS with Xcode installed.');
      return 1;
    }
    stdout.writeln('Building for iOS...');
    return 0;
  }
}
```

### WatchCommand

```dart
import 'dart:async';
import 'dart:io';
import 'package:args/command_runner.dart';
import 'package:watcher/watcher.dart';

class WatchCommand extends Command<int> {
  @override
  final String name = 'watch';

  @override
  final String description = 'Watch source files and rebuild on changes.';

  WatchCommand() {
    argParser.addOption(
      'directory',
      abbr: 'd',
      help: 'Directory to watch for changes.',
      valueHelp: 'dir',
      defaultsTo: 'lib/',
    );
    argParser.addMultiOption(
      'extension',
      abbr: 'e',
      help: 'File extensions to watch.',
      defaultsTo: ['.dart'],
    );
    argParser.addOption(
      'debounce',
      help: 'Debounce interval in milliseconds.',
      valueHelp: 'ms',
      defaultsTo: '300',
    );
  }

  @override
  Future<int> run() async {
    final dir = argResults!['directory'] as String;
    final extensions = argResults!['extension'] as List<String>;
    final debounceMs = int.tryParse(argResults!['debounce'] as String) ?? 300;
    final debounce = Duration(milliseconds: debounceMs);

    stdout.writeln('Watching $dir for changes '
        '(extensions: ${extensions.join(', ')})...');
    stdout.writeln('Press Ctrl+C to stop.\n');

    final watcher = DirectoryWatcher(dir);
    Timer? debounceTimer;

    final subscription = watcher.events.listen((event) {
      final matchesExtension = extensions.any(
        (ext) => event.path.endsWith(ext),
      );
      if (!matchesExtension) return;

      debounceTimer?.cancel();
      debounceTimer = Timer(debounce, () {
        stdout.writeln('[${_timestamp()}] Change detected: ${event.path}');
        _rebuild();
      });
    });

    // Block until SIGINT or SIGTERM.
    final completer = Completer<int>();
    ProcessSignal.sigint.watch().listen((_) {
      stdout.writeln('\nStopping watcher...');
      subscription.cancel();
      debounceTimer?.cancel();
      completer.complete(0);
    });

    return completer.future;
  }

  String _timestamp() {
    final now = DateTime.now();
    return '${now.hour.toString().padLeft(2, '0')}:'
        '${now.minute.toString().padLeft(2, '0')}:'
        '${now.second.toString().padLeft(2, '0')}';
  }

  void _rebuild() {
    stdout.writeln('Rebuilding...');
    // ... trigger incremental build ...
  }
}
```

### ExportCommand

```dart
class ExportCommand extends Command<int> {
  @override
  final String name = 'export';

  @override
  final String description = 'Export project data to various formats.';

  ExportCommand() {
    addSubcommand(ExportPdfCommand());
    addSubcommand(ExportCsvCommand());
  }
}

class ExportPdfCommand extends Command<int> {
  @override
  final String name = 'pdf';

  @override
  final String description = 'Export project report as a PDF document.';

  ExportPdfCommand() {
    argParser.addOption(
      'output',
      abbr: 'o',
      help: 'Output file path.',
      valueHelp: 'file',
      mandatory: true,
    );
    argParser.addFlag(
      'open',
      help: 'Open the generated PDF after export.',
      negatable: false,
    );
  }

  @override
  Future<int> run() async {
    final outputPath = argResults!['output'] as String;
    final shouldOpen = argResults!['open'] as bool;

    stdout.writeln('Exporting PDF to $outputPath...');
    // ... PDF generation logic ...

    if (shouldOpen) {
      // Platform-aware open command.
      final openCmd = Platform.isMacOS
          ? 'open'
          : Platform.isLinux
              ? 'xdg-open'
              : 'start';
      await Process.run(openCmd, [outputPath]);
    }

    return 0;
  }
}

class ExportCsvCommand extends Command<int> {
  @override
  final String name = 'csv';

  @override
  final String description = 'Export project data as CSV.';

  ExportCsvCommand() {
    argParser.addOption(
      'output',
      abbr: 'o',
      help: 'Output file path.',
      valueHelp: 'file',
      mandatory: true,
    );
    argParser.addOption(
      'delimiter',
      help: 'Field delimiter character.',
      defaultsTo: ',',
    );
  }

  @override
  Future<int> run() async {
    final outputPath = argResults!['output'] as String;
    final delimiter = argResults!['delimiter'] as String;

    stdout.writeln('Exporting CSV to $outputPath '
        '(delimiter: "${delimiter}")...');
    // ... CSV export logic ...
    return 0;
  }
}
```

---

## Flag and Option Parsing

### Boolean Flags

Boolean flags produce `true`/`false` values. Key behaviors:
- `negatable: true` (the default) generates both `--flag` and `--no-flag` variants.
- `negatable: false` means only `--flag` is accepted; it acts as a simple switch.
- `defaultsTo` sets the value when the flag is not supplied.

```dart
parser.addFlag('verbose', abbr: 'v', negatable: false);         // --verbose only
parser.addFlag('color', defaultsTo: true, negatable: true);      // --color / --no-color
parser.addFlag('dry-run', help: 'Preview without writing.', negatable: false);
```

### Single-Value Options

Options take a single string value. Use `allowed` for enum-like restrictions, `mandatory` to require the option, and `valueHelp` for usage display.

```dart
parser.addOption(
  'format',
  abbr: 'f',
  help: 'Output format.',
  allowed: ['json', 'yaml', 'toml'],
  defaultsTo: 'json',
);

parser.addOption(
  'port',
  help: 'Server port number.',
  valueHelp: 'number',
  defaultsTo: '8080',
);

// Mandatory option -- parser throws FormatException if missing.
parser.addOption(
  'project',
  help: 'Project name.',
  mandatory: true,
);
```

### Multi-Value Options

Multi options collect repeated flag uses into a `List<String>`:

```dart
parser.addMultiOption(
  'exclude',
  help: 'Glob patterns to exclude from processing.',
  valueHelp: 'pattern',
  defaultsTo: ['*.g.dart', '*.freezed.dart'],
  splitCommas: true, // --exclude=a,b parses as ['a', 'b']
);
```

Read them as: `final excludes = results['exclude'] as List<String>;`

### Separator (--) for Rest Arguments

Double dash `--` terminates option parsing. Everything after it lands in `results.rest`:

```
dartool build -- --some-passthrough-arg file.dart
```

This lets you forward arguments to child processes without the parent parser consuming them.

### Parsing Key=Value Pairs from Multi Options

A common pattern for `--define` flags:

```dart
Map<String, String> parseDefines(List<String> defines) {
  return {
    for (final d in defines)
      if (d.contains('='))
        d.substring(0, d.indexOf('=')): d.substring(d.indexOf('=') + 1)
      else
        d: 'true', // bare key treated as boolean true
  };
}
```

---

## ANSI Escape Codes for Colorized Output

ANSI escape sequences control text color, weight, and style in terminal emulators. Always gate color output behind a `--color` / `--no-color` flag, defaulting to `stdout.hasTerminal`.

### Color Constants

```dart
/// ANSI escape helpers. Disable by setting [enabled] to false
/// (e.g., when --no-color is passed or stdout is not a TTY).
class Ansi {
  Ansi({bool? enabled}) : enabled = enabled ?? stdout.hasTerminal;

  final bool enabled;

  String _wrap(String text, String code) =>
      enabled ? '\x1B[${code}m$text\x1B[0m' : text;

  // Foreground colors
  String red(String t)     => _wrap(t, '31');
  String green(String t)   => _wrap(t, '32');
  String yellow(String t)  => _wrap(t, '33');
  String blue(String t)    => _wrap(t, '34');
  String magenta(String t) => _wrap(t, '35');
  String cyan(String t)    => _wrap(t, '36');
  String white(String t)   => _wrap(t, '37');
  String gray(String t)    => _wrap(t, '90');

  // Styles
  String bold(String t)      => _wrap(t, '1');
  String dim(String t)       => _wrap(t, '2');
  String italic(String t)    => _wrap(t, '3');
  String underline(String t) => _wrap(t, '4');
  String strikethrough(String t) => _wrap(t, '9');

  // Compound styles
  String error(String t)   => bold(red(t));
  String warning(String t) => bold(yellow(t));
  String success(String t) => bold(green(t));
  String info(String t)    => bold(cyan(t));
  String hint(String t)    => dim(gray(t));

  // Background colors
  String bgRed(String t)    => _wrap(t, '41');
  String bgGreen(String t)  => _wrap(t, '42');
  String bgYellow(String t) => _wrap(t, '43');
  String bgBlue(String t)   => _wrap(t, '44');

  // Cursor control
  String get clearLine    => enabled ? '\x1B[2K\r' : '';
  String get cursorUp     => enabled ? '\x1B[1A' : '';
  String get hideCursor   => enabled ? '\x1B[?25l' : '';
  String get showCursor   => enabled ? '\x1B[?25h' : '';
  String cursorUpN(int n) => enabled ? '\x1B[${n}A' : '';
}
```

### 256-Color and True-Color Support

For richer output (CI dashboards, modern terminals):

```dart
// 256-color: \x1B[38;5;{n}m  (foreground), \x1B[48;5;{n}m (background)
String color256(String text, int colorCode) =>
    '\x1B[38;5;${colorCode}m$text\x1B[0m';

// True-color (24-bit): \x1B[38;2;{r};{g};{b}m
String trueColor(String text, int r, int g, int b) =>
    '\x1B[38;2;$r;$g;${b}m$text\x1B[0m';
```

### Detecting Color Support

```dart
bool supportsColor() {
  if (Platform.environment.containsKey('NO_COLOR')) return false;
  if (Platform.environment['TERM'] == 'dumb') return false;
  if (!stdout.hasTerminal) return false;
  final colorTerm = Platform.environment['COLORTERM'] ?? '';
  if (colorTerm == 'truecolor' || colorTerm == '24bit') return true;
  return stdout.supportsAnsiEscapes;
}
```

---

## Progress Indicators

Long-running operations (builds, exports, network fetches) need visual feedback. Two primary patterns: spinners for indeterminate progress and progress bars for measurable work.

### Spinner

```dart
import 'dart:async';
import 'dart:io';

class Spinner {
  Spinner({
    required this.message,
    this.frames = const ['|', '/', '-', '\\'],
    this.interval = const Duration(milliseconds: 80),
  });

  final String message;
  final List<String> frames;
  final Duration interval;
  final Ansi _ansi = Ansi();

  Timer? _timer;
  int _index = 0;

  void start() {
    stdout.write(_ansi.hideCursor);
    _timer = Timer.periodic(interval, (_) {
      stdout.write('${_ansi.clearLine}${_ansi.cyan(frames[_index])} $message');
      _index = (_index + 1) % frames.length;
    });
  }

  void stop({String? finalMessage, bool success = true}) {
    _timer?.cancel();
    final icon = success ? _ansi.success('done') : _ansi.error('fail');
    stdout.writeln(
      '${_ansi.clearLine}$icon ${finalMessage ?? message}',
    );
    stdout.write(_ansi.showCursor);
  }
}

// Usage:
// final spinner = Spinner(message: 'Compiling assets...');
// spinner.start();
// await compileAssets();
// spinner.stop(finalMessage: 'Assets compiled (1.2s)');
```

### Progress Bar

```dart
class ProgressBar {
  ProgressBar({
    required this.total,
    this.width = 40,
    this.completeChar = '\u2588', // full block
    this.incompleteChar = '\u2591', // light shade
  });

  final int total;
  final int width;
  final String completeChar;
  final String incompleteChar;
  final Ansi _ansi = Ansi();
  final Stopwatch _stopwatch = Stopwatch();

  int _current = 0;

  void start() {
    _stopwatch.start();
    _render();
  }

  void update(int current) {
    _current = current.clamp(0, total);
    _render();
  }

  void increment([int amount = 1]) => update(_current + amount);

  void complete() {
    _current = total;
    _render();
    stdout.writeln();
  }

  void _render() {
    final fraction = _current / total;
    final filled = (fraction * width).round();
    final empty = width - filled;
    final percent = (fraction * 100).toStringAsFixed(0).padLeft(3);
    final elapsed = _stopwatch.elapsed;
    final eta = _current > 0
        ? Duration(
            milliseconds:
                (elapsed.inMilliseconds * (total - _current) / _current)
                    .round())
        : Duration.zero;

    final bar = '${_ansi.green(completeChar * filled)}'
        '${_ansi.dim(incompleteChar * empty)}';

    stdout.write(
      '${_ansi.clearLine}'
      '  $bar $percent% '
      '[$_current/$total] '
      'ETA: ${_formatDuration(eta)} ',
    );
  }

  String _formatDuration(Duration d) {
    if (d.inMinutes > 0) {
      return '${d.inMinutes}m ${d.inSeconds.remainder(60)}s';
    }
    return '${d.inSeconds}s';
  }
}

// Usage:
// final bar = ProgressBar(total: files.length);
// bar.start();
// for (final file in files) {
//   await processFile(file);
//   bar.increment();
// }
// bar.complete();
```

### Multi-Line Status Display

For parallel tasks, use cursor movement to update multiple lines simultaneously:

```dart
void renderMultiStatus(Ansi ansi, List<(String label, String status)> tasks) {
  // Move cursor up to overwrite previous render.
  for (var i = 0; i < tasks.length; i++) {
    stdout.write(ansi.clearLine);
    final (label, status) = tasks[i];
    stdout.writeln('  $label: $status');
  }
  // Move cursor back up for next render cycle.
  stdout.write(ansi.cursorUpN(tasks.length));
}
```

---

## Error Formatting with Remediation

CLI tools must produce errors that are immediately actionable. Every error message should answer three questions: (1) What happened? (2) Why? (3) How to fix it.

### Structured Error Output

```dart
import 'dart:io';

class CliError {
  const CliError({
    required this.message,
    this.details,
    this.remediation,
    this.exitCode = 1,
    this.context = const {},
  });

  final String message;
  final String? details;
  final String? remediation;
  final int exitCode;
  final Map<String, String> context;

  void render(Ansi ansi) {
    stderr.writeln();
    stderr.writeln('${ansi.error('Error:')} $message');

    if (details != null) {
      stderr.writeln();
      for (final line in details!.split('\n')) {
        stderr.writeln('  ${ansi.dim(line)}');
      }
    }

    if (context.isNotEmpty) {
      stderr.writeln();
      for (final MapEntry(:key, :value) in context.entries) {
        stderr.writeln('  ${ansi.bold(key)}: $value');
      }
    }

    if (remediation != null) {
      stderr.writeln();
      stderr.writeln('${ansi.info('Fix:')} $remediation');
    }

    stderr.writeln();
  }
}

// Usage:
// CliError(
//   message: 'Configuration file not found.',
//   details: 'Expected file at: ./dartool.yaml',
//   context: {
//     'Working directory': Directory.current.path,
//     'Config flag': '--config dartool.yaml',
//   },
//   remediation: 'Run "dartool init" to create a configuration file, '
//       'or use --config to specify a different path.',
//   exitCode: 66, // EX_NOINPUT
// ).render(Ansi());
```

### Error Categories

Map error types to appropriate exit codes and remediation templates:

```dart
sealed class ToolError {
  int get exitCode;
  String get remediation;
}

class ConfigNotFound extends ToolError {
  ConfigNotFound(this.path);
  final String path;

  @override
  int get exitCode => 66; // EX_NOINPUT

  @override
  String get remediation =>
      'Run "dartool init" to create a default configuration, '
      'or specify the correct path with --config <path>.';
}

class InvalidConfig extends ToolError {
  InvalidConfig(this.path, this.parseError);
  final String path;
  final String parseError;

  @override
  int get exitCode => 78; // EX_CONFIG

  @override
  String get remediation =>
      'Check $path for YAML syntax errors. '
      'Run "dartool config validate" to diagnose.';
}

class BuildFailed extends ToolError {
  BuildFailed(this.target, this.reason);
  final String target;
  final String reason;

  @override
  int get exitCode => 70; // EX_SOFTWARE

  @override
  String get remediation =>
      'Review the error output above. '
      'Run with --verbose for detailed compiler diagnostics.';
}

class PermissionDenied extends ToolError {
  PermissionDenied(this.path);
  final String path;

  @override
  int get exitCode => 77; // EX_NOPERM

  @override
  String get remediation =>
      'Check file permissions on $path. '
      'You may need to run with elevated privileges or fix ownership.';
}
```

---

## Help Text Generation

`CommandRunner` and `ArgParser` produce help text automatically, but you can customize it extensively.

### Customizing Help Output

The `usage` getter on `ArgParser` returns the formatted help string. Control its layout with `usageLineLength`, `allowedHelp`, and `valueHelp`:

```dart
final parser = ArgParser(usageLineLength: 80);

parser.addOption(
  'format',
  help: 'Output format for generated files.',
  allowed: ['json', 'yaml', 'toml'],
  allowedHelp: {
    'json': 'Standard JSON (default, recommended for CI).',
    'yaml': 'YAML for human-readable configurations.',
    'toml': 'TOML for Rust ecosystem interop.',
  },
  defaultsTo: 'json',
);
```

This produces:

```
--format    Output format for generated files.

            [json] (default)  Standard JSON (default, recommended for CI).
            [yaml]            YAML for human-readable configurations.
            [toml]            TOML for Rust ecosystem interop.
```

### Custom Help Command

Override the default help for richer output:

```dart
class CustomHelpCommand extends Command<int> {
  @override
  final String name = 'help';

  @override
  final String description = 'Display help information.';

  @override
  Future<int> run() async {
    final ansi = Ansi();
    final runner = parent as CommandRunner<int>;

    stdout.writeln();
    stdout.writeln(ansi.bold('dartool') +
        ansi.dim(' - A CLI build tool for Dart projects'));
    stdout.writeln();
    stdout.writeln(ansi.underline('USAGE'));
    stdout.writeln('  dartool <command> [options] [arguments]');
    stdout.writeln();
    stdout.writeln(ansi.underline('COMMANDS'));

    final maxLen = runner.commands.keys
        .fold<int>(0, (m, k) => k.length > m ? k.length : m);

    for (final entry in runner.commands.entries) {
      if (entry.value.hidden) continue;
      final padded = entry.key.padRight(maxLen + 2);
      stdout.writeln('  ${ansi.cyan(padded)} ${entry.value.description}');
    }

    stdout.writeln();
    stdout.writeln(ansi.underline('GLOBAL OPTIONS'));
    stdout.writeln(runner.argParser.usage);
    stdout.writeln();
    stdout.writeln(
      ansi.dim('Run "dartool help <command>" for command-specific help.'),
    );
    stdout.writeln();

    return 0;
  }
}
```

### Category Grouping

Group commands into categories for clarity in help output:

```dart
class CategorizedHelpCommand extends Command<int> {
  @override
  final String name = 'help';

  @override
  final String description = 'Display help information.';

  static const categories = {
    'Project': ['init'],
    'Build': ['build', 'watch'],
    'Export': ['export'],
  };

  @override
  Future<int> run() async {
    final ansi = Ansi();
    final runner = parent as CommandRunner<int>;

    for (final MapEntry(:key, :value) in categories.entries) {
      stdout.writeln(ansi.bold(key));
      for (final cmdName in value) {
        final cmd = runner.commands[cmdName];
        if (cmd == null || cmd.hidden) continue;
        stdout.writeln('  ${ansi.cyan(cmdName.padRight(12))} '
            '${cmd.description}');
      }
      stdout.writeln();
    }

    return 0;
  }
}
```

---

## Exit Codes and Signal Handling

### Standard Exit Codes

Follow BSD `sysexits.h` conventions for machine-readable exit codes:

| Code | Name          | Meaning                                      |
|------|---------------|----------------------------------------------|
| 0    | EX_OK         | Success                                      |
| 1    | (General)     | General unspecified error                    |
| 2    | (Misuse)      | Misuse of shell command                      |
| 64   | EX_USAGE      | Invalid command-line arguments               |
| 65   | EX_DATAERR    | Input data format error                      |
| 66   | EX_NOINPUT    | Input file not found or unreadable           |
| 69   | EX_UNAVAILABLE| Required service unavailable                 |
| 70   | EX_SOFTWARE   | Internal software error                      |
| 73   | EX_CANTCREAT  | Cannot create output file                    |
| 74   | EX_IOERR      | I/O error during operation                   |
| 77   | EX_NOPERM     | Insufficient permissions                     |
| 78   | EX_CONFIG     | Configuration error                          |

Define these as constants for consistency:

```dart
abstract final class ExitCode {
  static const ok = 0;
  static const error = 1;
  static const usage = 64;
  static const dataError = 65;
  static const noInput = 66;
  static const unavailable = 69;
  static const software = 70;
  static const cantCreate = 73;
  static const ioError = 74;
  static const noPermission = 77;
  static const config = 78;
}
```

### Signal Handling

Handle POSIX signals for graceful shutdown and cleanup. `SIGINT` (Ctrl+C) and `SIGTERM` are the most important for CLI tools.

```dart
import 'dart:async';
import 'dart:io';

/// Manages graceful shutdown on SIGINT and SIGTERM.
class SignalHandler {
  final List<Future<void> Function()> _cleanupTasks = [];
  final List<StreamSubscription<ProcessSignal>> _subscriptions = [];
  bool _shuttingDown = false;

  void register() {
    // SIGINT is sent on Ctrl+C.
    _subscriptions.add(
      ProcessSignal.sigint.watch().listen((_) => _shutdown('SIGINT')),
    );

    // SIGTERM is sent by process managers, orchestrators, CI.
    // Note: SIGTERM is not available on Windows.
    if (!Platform.isWindows) {
      _subscriptions.add(
        ProcessSignal.sigterm.watch().listen((_) => _shutdown('SIGTERM')),
      );
    }
  }

  /// Register a cleanup task to run on shutdown.
  /// Tasks run in LIFO order (last registered runs first).
  void onShutdown(Future<void> Function() task) {
    _cleanupTasks.add(task);
  }

  Future<void> _shutdown(String signal) async {
    if (_shuttingDown) return; // prevent re-entrant shutdown
    _shuttingDown = true;

    stderr.writeln('\nReceived $signal. Shutting down gracefully...');

    // Run cleanup tasks in reverse order.
    for (final task in _cleanupTasks.reversed) {
      try {
        await task().timeout(const Duration(seconds: 5));
      } catch (e) {
        stderr.writeln('Cleanup error: $e');
      }
    }

    // Cancel signal subscriptions.
    for (final sub in _subscriptions) {
      await sub.cancel();
    }

    exit(0);
  }

  void dispose() {
    for (final sub in _subscriptions) {
      sub.cancel();
    }
  }
}

// Usage in main():
// final signals = SignalHandler()..register();
// signals.onShutdown(() async {
//   await tempDir.delete(recursive: true);
// });
// signals.onShutdown(() async {
//   await logFile.close();
// });
```

### Handling Unhandled Exceptions

Install a top-level error handler so uncaught exceptions produce structured output:

```dart
Future<void> main(List<String> args) async {
  // Catch synchronous errors.
  runZonedGuarded(
    () async {
      final exitCode = await DartoolRunner().run(args);
      exit(exitCode);
    },
    (error, stackTrace) {
      final ansi = Ansi();
      stderr.writeln(ansi.error('Unhandled exception:'));
      stderr.writeln('  $error');
      stderr.writeln();
      stderr.writeln(ansi.dim(stackTrace.toString()));
      stderr.writeln();
      stderr.writeln(ansi.info('This is a bug in dartool. Please report it:'));
      stderr.writeln(
          '  https://github.com/your-org/dartool/issues/new');
      exit(ExitCode.software);
    },
  );
}
```

---

## stdin stdout stderr Usage

Proper stream discipline is essential for composable CLI tools. Follow the Unix convention: data goes to stdout, diagnostics go to stderr.

### Rules

1. **stdout** -- Program output that could be piped to another tool. JSON results, CSV data, file contents.
2. **stderr** -- Logging, progress indicators, errors, warnings, prompts. Never pipe stderr data to downstream tools.
3. **stdin** -- Read piped input when no file argument is provided. Check `stdin.hasTerminal` to detect pipe vs. interactive mode.

### Reading from stdin

```dart
import 'dart:convert';
import 'dart:io';

/// Read input from either a file argument or stdin pipe.
Future<String> readInput(ArgResults results) async {
  if (results.rest.isNotEmpty) {
    // Read from file argument.
    final file = File(results.rest.first);
    if (!file.existsSync()) {
      throw CliError(
        message: 'File not found: ${results.rest.first}',
        exitCode: ExitCode.noInput,
      );
    }
    return file.readAsString();
  }

  if (stdin.hasTerminal) {
    // Interactive mode -- no pipe, no file.
    throw CliError(
      message: 'No input provided.',
      remediation: 'Provide a file argument or pipe data to stdin.\n'
          '  dartool format input.dart\n'
          '  cat input.dart | dartool format',
      exitCode: ExitCode.usage,
    );
  }

  // Read from piped stdin.
  return await stdin.transform(utf8.decoder).join();
}
```

### Line-by-Line stdin Processing

For streaming large inputs without buffering everything in memory:

```dart
Future<void> processStdinLines() async {
  await for (final line in stdin
      .transform(utf8.decoder)
      .transform(const LineSplitter())) {
    final processed = transformLine(line);
    stdout.writeln(processed);
  }
}
```

### JSON Output for Machine Consumption

When `--format json` is specified, output structured JSON to stdout and keep all human-readable messages on stderr:

```dart
import 'dart:convert';

void outputResult({
  required String format,
  required Map<String, dynamic> data,
  required Ansi ansi,
}) {
  switch (format) {
    case 'json':
      stdout.writeln(const JsonEncoder.withIndent('  ').convert(data));
    case 'text':
      for (final MapEntry(:key, :value) in data.entries) {
        stdout.writeln('${ansi.bold(key)}: $value');
      }
    default:
      stderr.writeln(ansi.warning('Unknown format "$format", using text.'));
      outputResult(format: 'text', data: data, ansi: ansi);
  }
}
```

---

## Environment Variable Integration

CLI tools should read configuration from environment variables as a fallback behind explicit flags, following the 12-factor app methodology.

### Precedence Order

1. Explicit command-line flags (highest priority)
2. Environment variables
3. Configuration file values
4. Built-in defaults (lowest priority)

### Implementation Pattern

```dart
/// Resolve a configuration value using the standard precedence chain.
String resolveConfig({
  required ArgResults results,
  required String flagName,
  required String envVar,
  required Map<String, dynamic> fileConfig,
  required String configKey,
  required String defaultValue,
}) {
  // 1. Explicit CLI flag takes priority.
  if (results.wasParsed(flagName)) {
    return results[flagName] as String;
  }

  // 2. Environment variable.
  final envValue = Platform.environment[envVar];
  if (envValue != null && envValue.isNotEmpty) {
    return envValue;
  }

  // 3. Configuration file.
  final fileValue = fileConfig[configKey];
  if (fileValue is String && fileValue.isNotEmpty) {
    return fileValue;
  }

  // 4. Default.
  return defaultValue;
}

// Usage:
// final apiKey = resolveConfig(
//   results: argResults!,
//   flagName: 'api-key',
//   envVar: 'DARTOOL_API_KEY',
//   fileConfig: loadedConfig,
//   configKey: 'api_key',
//   defaultValue: '',
// );
```

### Standard Environment Variables

Adopt naming conventions for your tool's environment variables:

```dart
abstract final class EnvVars {
  /// DARTOOL_CONFIG -- path to config file.
  static const config = 'DARTOOL_CONFIG';

  /// DARTOOL_LOG_LEVEL -- override log verbosity.
  static const logLevel = 'DARTOOL_LOG_LEVEL';

  /// DARTOOL_NO_COLOR -- disable color output (also honors NO_COLOR).
  static const noColor = 'DARTOOL_NO_COLOR';

  /// DARTOOL_HOME -- base directory for tool caches and data.
  static const home = 'DARTOOL_HOME';

  /// CI -- standard flag set by CI systems (GitHub Actions, GitLab CI, etc.).
  static const ci = 'CI';

  /// Read the DARTOOL_HOME path or fall back to ~/.dartool.
  static String get homePath {
    return Platform.environment[home] ??
        '${Platform.environment['HOME']}/.dartool';
  }

  /// Detect if running in a CI environment.
  static bool get isCI {
    return Platform.environment.containsKey(ci) ||
        Platform.environment.containsKey('GITHUB_ACTIONS') ||
        Platform.environment.containsKey('GITLAB_CI') ||
        Platform.environment.containsKey('CIRCLECI');
  }
}
```

---

## Shell Completion Generation

Provide shell completion scripts so users get tab-completion for commands, flags, and options.

### Bash Completion Script Generator

```dart
class BashCompletionGenerator {
  BashCompletionGenerator(this.runner);

  final CommandRunner<int> runner;

  String generate() {
    final buffer = StringBuffer();
    final toolName = runner.executableName;

    buffer.writeln('# Bash completion for $toolName');
    buffer.writeln('# Add to ~/.bashrc or ~/.bash_profile:');
    buffer.writeln('#   eval "\$($toolName completion bash)"');
    buffer.writeln();
    buffer.writeln('_${toolName}_completions() {');
    buffer.writeln('  local cur prev commands');
    buffer.writeln('  cur="\${COMP_WORDS[COMP_CWORD]}"');
    buffer.writeln('  prev="\${COMP_WORDS[COMP_CWORD-1]}"');
    buffer.writeln();

    // Top-level commands.
    final commands = runner.commands.keys.where((k) =>
        !runner.commands[k]!.hidden).toList();
    buffer.writeln('  commands="${commands.join(' ')}"');
    buffer.writeln();
    buffer.writeln('  if [[ \${COMP_CWORD} -eq 1 ]]; then');
    buffer.writeln('    COMPREPLY=(\$(compgen -W "\${commands}" -- "\${cur}"))');
    buffer.writeln('    return 0');
    buffer.writeln('  fi');
    buffer.writeln();

    // Per-command flag completion.
    for (final entry in runner.commands.entries) {
      if (entry.value.hidden) continue;
      final flags = entry.value.argParser.options.keys
          .map((o) => '--$o')
          .join(' ');
      buffer.writeln('  case "\${COMP_WORDS[1]}" in');
      buffer.writeln('    ${entry.key})');
      buffer.writeln(
          '      COMPREPLY=(\$(compgen -W "$flags" -- "\${cur}"))');
      buffer.writeln('      return 0');
      buffer.writeln('      ;;');
      buffer.writeln('  esac');
    }

    buffer.writeln('}');
    buffer.writeln();
    buffer.writeln('complete -F _${toolName}_completions $toolName');

    return buffer.toString();
  }
}
```

### Zsh Completion Script Generator

```dart
class ZshCompletionGenerator {
  ZshCompletionGenerator(this.runner);

  final CommandRunner<int> runner;

  String generate() {
    final buffer = StringBuffer();
    final toolName = runner.executableName;

    buffer.writeln('#compdef $toolName');
    buffer.writeln();
    buffer.writeln('_$toolName() {');
    buffer.writeln('  local -a commands');
    buffer.writeln('  commands=(');

    for (final entry in runner.commands.entries) {
      if (entry.value.hidden) continue;
      final escaped = entry.value.description.replaceAll("'", "''");
      buffer.writeln("    '${entry.key}:$escaped'");
    }

    buffer.writeln('  )');
    buffer.writeln();
    buffer.writeln('  _arguments -C \\');
    buffer.writeln("    '(-v --verbose)'{-v,--verbose}'[Enable verbose]' \\");
    buffer.writeln("    '--no-color[Disable colorized output]' \\");
    buffer.writeln("    '1:command:->command' \\");
    buffer.writeln("    '*::arg:->args'");
    buffer.writeln();
    buffer.writeln('  case \$state in');
    buffer.writeln('    command)');
    buffer.writeln('      _describe "command" commands');
    buffer.writeln('      ;;');
    buffer.writeln('  esac');
    buffer.writeln('}');
    buffer.writeln();
    buffer.writeln('_$toolName');

    return buffer.toString();
  }
}
```

### Completion Install Command

```dart
class CompletionCommand extends Command<int> {
  @override
  final String name = 'completion';

  @override
  final String description = 'Generate shell completion scripts.';

  CompletionCommand() {
    argParser.addOption(
      'shell',
      help: 'Target shell for completion script.',
      allowed: ['bash', 'zsh', 'fish'],
      defaultsTo: _detectShell(),
    );
  }

  static String _detectShell() {
    final shell = Platform.environment['SHELL'] ?? '';
    if (shell.endsWith('/zsh')) return 'zsh';
    if (shell.endsWith('/fish')) return 'fish';
    return 'bash';
  }

  @override
  Future<int> run() async {
    final shell = argResults!['shell'] as String;
    final runner = parent as CommandRunner<int>;

    final script = switch (shell) {
      'bash' => BashCompletionGenerator(runner).generate(),
      'zsh' => ZshCompletionGenerator(runner).generate(),
      _ => throw UsageException(
          'Unsupported shell: $shell', argParser.usage),
    };

    stdout.write(script);
    return 0;
  }
}
```

---

## CLI Testing Strategies

### Unit Testing ArgParser Configurations

Test that parsers accept valid input and reject invalid input without running the full tool:

```dart
import 'package:args/args.dart';
import 'package:test/test.dart';

void main() {
  group('ArgParser', () {
    late ArgParser parser;

    setUp(() {
      parser = buildRootParser();
    });

    test('parses verbose flag', () {
      final results = parser.parse(['--verbose']);
      expect(results['verbose'], isTrue);
    });

    test('verbose defaults to false', () {
      final results = parser.parse([]);
      expect(results['verbose'], isFalse);
    });

    test('parses config option', () {
      final results = parser.parse(['--config', 'custom.yaml']);
      expect(results['config'], equals('custom.yaml'));
    });

    test('config defaults to dartool.yaml', () {
      final results = parser.parse([]);
      expect(results['config'], equals('dartool.yaml'));
    });

    test('parses multi-value define option', () {
      final results = parser.parse(['-D', 'A=1', '-D', 'B=2']);
      expect(results['define'], equals(['A=1', 'B=2']));
    });

    test('rejects invalid log-level', () {
      expect(
        () => parser.parse(['--log-level', 'trace']),
        throwsA(isA<FormatException>()),
      );
    });

    test('accepts allowed log-level values', () {
      for (final level in ['debug', 'info', 'warn', 'error']) {
        final results = parser.parse(['--log-level', level]);
        expect(results['log-level'], equals(level));
      }
    });

    test('wasParsed distinguishes explicit from default', () {
      final defaultResults = parser.parse([]);
      expect(defaultResults.wasParsed('config'), isFalse);

      final explicitResults = parser.parse(['--config', 'dartool.yaml']);
      expect(explicitResults.wasParsed('config'), isTrue);
    });

    test('rest captures positional arguments', () {
      final results = parser.parse(['--verbose', 'file1.dart', 'file2.dart']);
      expect(results.rest, equals(['file1.dart', 'file2.dart']));
    });

    test('double-dash stops option parsing', () {
      final results = parser.parse(['--', '--not-a-flag']);
      expect(results.rest, equals(['--not-a-flag']));
    });
  });
}
```

### Integration Testing with Process Execution

Use `dart:io` `Process.run` or `package:process_run` to test the compiled CLI end-to-end:

```dart
import 'dart:io';
import 'package:test/test.dart';

void main() {
  group('CLI integration', () {
    test('init creates config file', () async {
      final tempDir = await Directory.systemTemp.createTemp('dartool_test_');

      try {
        final result = await Process.run(
          'dart',
          ['run', 'bin/dartool.dart', 'init', tempDir.path],
          workingDirectory: Directory.current.path,
        );

        expect(result.exitCode, equals(0));
        expect(result.stdout, contains('Initialized dartool project'));
        expect(
          File('${tempDir.path}/dartool.yaml').existsSync(),
          isTrue,
        );
      } finally {
        await tempDir.delete(recursive: true);
      }
    });

    test('exits 64 on invalid arguments', () async {
      final result = await Process.run(
        'dart',
        ['run', 'bin/dartool.dart', '--log-level', 'invalid'],
        workingDirectory: Directory.current.path,
      );

      expect(result.exitCode, equals(64));
      expect(result.stderr, contains('Error'));
    });

    test('help flag prints usage', () async {
      final result = await Process.run(
        'dart',
        ['run', 'bin/dartool.dart', '--help'],
        workingDirectory: Directory.current.path,
      );

      expect(result.exitCode, equals(0));
      expect(result.stdout, contains('dartool'));
      expect(result.stdout, contains('--verbose'));
      expect(result.stdout, contains('--config'));
    });

    test('outputs valid JSON with --format json', () async {
      final result = await Process.run(
        'dart',
        ['run', 'bin/dartool.dart', 'export', 'csv', '-o', '/dev/null',
         '--format', 'json'],
        workingDirectory: Directory.current.path,
      );

      if (result.exitCode == 0) {
        // Verify stdout is valid JSON.
        expect(
          () => jsonDecode(result.stdout as String),
          returnsNormally,
        );
      }
    });

    test('no-color flag disables ANSI codes', () async {
      final result = await Process.run(
        'dart',
        ['run', 'bin/dartool.dart', '--no-color', 'build'],
        workingDirectory: Directory.current.path,
      );

      // ANSI escape code should not appear.
      expect(result.stdout, isNot(contains('\x1B[')));
      expect(result.stderr, isNot(contains('\x1B[')));
    });
  });
}
```

### Testing with Captured stdout/stderr

For unit-level testing of command output, inject custom `IOSink` objects or use `IOOverrides`:

```dart
import 'dart:io';
import 'package:test/test.dart';

/// Captures stdout and stderr during a test callback.
Future<({String stdout, String stderr})> captureOutput(
  Future<void> Function() action,
) async {
  final outBuffer = StringBuffer();
  final errBuffer = StringBuffer();

  await IOOverrides.runZoned(
    () async => await action(),
    stdout: () => _BufferSink(outBuffer),
    stderr: () => _BufferSink(errBuffer),
  );

  return (stdout: outBuffer.toString(), stderr: errBuffer.toString());
}

class _BufferSink implements Stdout {
  _BufferSink(this._buffer);
  final StringBuffer _buffer;

  @override
  void write(Object? object) => _buffer.write(object);

  @override
  void writeln([Object? object = '']) {
    _buffer.write(object);
    _buffer.write('\n');
  }

  @override
  void writeAll(Iterable objects, [String sep = '']) =>
      _buffer.writeAll(objects, sep);

  @override
  void writeCharCode(int charCode) =>
      _buffer.writeCharCode(charCode);

  // Stub remaining Stdout members for test purposes.
  @override
  bool get hasTerminal => false;

  @override
  bool get supportsAnsiEscapes => false;

  @override
  int get terminalColumns => 80;

  @override
  int get terminalLines => 24;

  @override
  IOSink get nonBlocking => this as IOSink;

  @override
  Encoding get encoding => utf8;

  @override
  set encoding(Encoding e) {}

  @override
  void add(List<int> data) => _buffer.write(utf8.decode(data));

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future addStream(Stream<List<int>> stream) async {
    await for (final data in stream) {
      add(data);
    }
  }

  @override
  Future flush() async {}

  @override
  Future close() async {}

  @override
  Future get done => Future.value();
}

// Usage in tests:
// test('build command prints output directory', () async {
//   final output = await captureOutput(() async {
//     await BuildCommand().run();
//   });
//   expect(output.stdout, contains('Output:'));
// });
```

### Testing with process_run

`package:process_run` provides a richer API for process execution in tests, including `shell: true` support and environment overrides:

```dart
import 'package:process_run/process_run.dart';
import 'package:test/test.dart';

void main() {
  group('process_run tests', () {
    test('build command respects DARTOOL_CONFIG env var', () async {
      final result = await runExecutableArguments(
        'dart',
        ['run', 'bin/dartool.dart', 'build'],
        environment: {'DARTOOL_CONFIG': 'test/fixtures/custom.yaml'},
        workingDirectory: Directory.current.path,
      );

      expect(result.exitCode, equals(0));
    });

    test('piped stdin input works', () async {
      final result = await runExecutableArguments(
        'dart',
        ['run', 'bin/dartool.dart', 'format'],
        stdin: 'void main() { print("hello"); }',
        workingDirectory: Directory.current.path,
      );

      expect(result.exitCode, equals(0));
      expect(result.outText, contains('void main()'));
    });
  });
}
```

---

## Best Practices

### Argument Design

- **Short flags for common options only.** Reserve single-letter abbreviations (`-v`, `-o`, `-f`) for the most frequently used flags. Obscure options should be long-form only.
- **Use `--no-` prefix for negatable boolean flags.** Dart's `ArgParser` generates this automatically when `negatable: true`. Document both forms in help text.
- **Make destructive actions require confirmation.** Use `--force` or `--yes` to skip interactive prompts. Default to safe behavior.
- **Support `--` for passthrough arguments.** Users expect double-dash to stop option parsing and pass remaining args to child processes.
- **Use `mandatory: true` sparingly.** Prefer sensible defaults and environment variable fallbacks over requiring flags.

### Output Discipline

- **Separate data from diagnostics.** Machine-readable output (JSON, CSV) goes to stdout. Progress, warnings, and errors go to stderr.
- **Default to human-friendly output.** When stdout is a TTY, use colors and formatting. When piped, output plain text or structured data.
- **Support `--format` for structured output.** Offer `json`, `text`, and `yaml` output modes for scriptability.
- **Honor `NO_COLOR` environment variable.** This is a cross-tool standard (https://no-color.org/). Check it alongside `--no-color` and `stdout.hasTerminal`.
- **Keep progress indicators on stderr.** Spinners and progress bars must not corrupt stdout data when output is piped.

### Error Handling

- **Every error message must include a remediation step.** Tell the user exactly what to do next.
- **Use specific exit codes.** Map error categories to BSD sysexits codes so scripts can react programmatically.
- **Show file paths, line numbers, and context.** When reporting config or input errors, point to the exact location.
- **Verbose mode shows stack traces.** Hide internal details by default but make them available with `--verbose`.
- **Never swallow exceptions silently.** Log to stderr even in quiet mode.

### Performance

- **Lazy-load heavy dependencies.** Only import and initialize subsystems needed by the invoked command.
- **Use async I/O throughout.** Never block the event loop with synchronous file reads in production CLI code.
- **Stream large outputs.** Do not buffer entire results in memory; write to stdout incrementally.
- **Cache expensive computations.** Store derived data in `DARTOOL_HOME` with invalidation based on source file timestamps.

### Signal Handling

- **Always handle SIGINT.** Users expect Ctrl+C to perform a clean shutdown, not leave temp files behind.
- **Register cleanup tasks in LIFO order.** Resources acquired last should be released first.
- **Set a cleanup timeout.** If cleanup takes more than 5 seconds, force-exit to avoid hanging.
- **Re-raise signals after cleanup.** Exit with the correct signal-derived exit code (128 + signal number) so parent processes detect the signal.

### Testing

- **Test argument parsing separately from command logic.** Parse flags in unit tests; run full CLI in integration tests.
- **Use temp directories for file-producing commands.** Always clean up in `tearDown`.
- **Test both exit code and output.** Verify that `exitCode == 0`, stdout contains expected data, and stderr is clean (or contains expected warnings).
- **Test with `--no-color` in CI.** Assertions against output strings are simpler without ANSI escape codes.
- **Test environment variable precedence.** Verify that explicit flags override env vars, which override config files.

---

## Anti-Patterns

### 1. Printing Errors to stdout

```dart
// WRONG: Errors on stdout corrupt piped data.
stdout.writeln('Error: file not found');

// CORRECT: Errors always go to stderr.
stderr.writeln('Error: file not found');
```

### 2. Hard-Coding Colors Without a Toggle

```dart
// WRONG: Breaks piped output and screen readers.
stdout.writeln('\x1B[31mError\x1B[0m');

// CORRECT: Gate color behind a flag or terminal detection.
final ansi = Ansi(enabled: useColor);
stderr.writeln(ansi.error('Error'));
```

### 3. Using exit() Deep in Library Code

```dart
// WRONG: Calling exit() from a utility function makes code untestable.
void validateConfig(String path) {
  if (!File(path).existsSync()) {
    exit(1); // untestable, kills the test runner
  }
}

// CORRECT: Throw a typed exception; let the runner handle exit codes.
void validateConfig(String path) {
  if (!File(path).existsSync()) {
    throw ConfigNotFound(path);
  }
}
```

### 4. Ignoring stdin Pipe Detection

```dart
// WRONG: Blocks forever if no input is piped and no file is given.
final input = await stdin.transform(utf8.decoder).join();

// CORRECT: Check if stdin is a terminal first.
if (stdin.hasTerminal && argResults!.rest.isEmpty) {
  stderr.writeln('Error: No input. Provide a file or pipe data.');
  return ExitCode.usage;
}
```

### 5. Monolithic run() Methods

```dart
// WRONG: 300-line run() method doing parsing, validation, I/O, formatting.
@override
Future<int> run() async {
  // ... everything in one method ...
}

// CORRECT: Decompose into focused methods.
@override
Future<int> run() async {
  final config = _parseConfig();
  final input = await _readInput();
  final result = _process(input, config);
  _writeOutput(result);
  return 0;
}
```

### 6. Ignoring wasParsed() for Default Overrides

```dart
// WRONG: Cannot distinguish user intent from defaults.
final port = results['port'] as String; // always '8080' even if user didn't set it

// CORRECT: Use wasParsed to apply env var fallback only when flag is not explicit.
final port = results.wasParsed('port')
    ? results['port'] as String
    : Platform.environment['DARTOOL_PORT'] ?? '8080';
```

### 7. No Cleanup on Signals

```dart
// WRONG: Temp files survive Ctrl+C.
final tempDir = await Directory.systemTemp.createTemp('build_');
// ... long operation that might be interrupted ...

// CORRECT: Register cleanup before starting work.
final signals = SignalHandler()..register();
final tempDir = await Directory.systemTemp.createTemp('build_');
signals.onShutdown(() => tempDir.delete(recursive: true));
```

### 8. Synchronous File I/O in Async Commands

```dart
// WRONG: Blocks the event loop, prevents concurrent operations.
final content = File('large_file.dat').readAsStringSync();

// CORRECT: Use async I/O.
final content = await File('large_file.dat').readAsString();
```

### 9. Missing Help Text on Options

```dart
// WRONG: Users see a raw flag name with no explanation.
parser.addOption('mode');

// CORRECT: Always provide help text, valueHelp, and ideally allowedHelp.
parser.addOption(
  'mode',
  help: 'Processing mode for the compiler.',
  allowed: ['jit', 'aot', 'kernel'],
  allowedHelp: {
    'jit': 'Just-in-time compilation (faster startup, slower peak).',
    'aot': 'Ahead-of-time compilation (slower startup, faster peak).',
    'kernel': 'Kernel snapshot for VM.',
  },
  defaultsTo: 'aot',
);
```

### 10. Not Testing Error Paths

```dart
// WRONG: Only testing the happy path.
test('build succeeds', () async {
  final result = await Process.run('dart', ['run', 'bin/dartool.dart', 'build']);
  expect(result.exitCode, 0);
});

// CORRECT: Test error paths with specific exit codes and messages.
test('build fails with missing config', () async {
  final result = await Process.run(
    'dart',
    ['run', 'bin/dartool.dart', 'build', '--config', 'nonexistent.yaml'],
  );
  expect(result.exitCode, equals(66)); // EX_NOINPUT
  expect(result.stderr, contains('not found'));
  expect(result.stderr, contains('dartool init')); // remediation hint
});
```

---

## Sources & References

- [Dart package:args API Reference](https://pub.dev/documentation/args/latest/args/args-library.html)
- [Dart CommandRunner Documentation](https://pub.dev/documentation/args/latest/command_runner/CommandRunner-class.html)
- [Dart dart:io Process and Signal Handling](https://api.dart.dev/stable/dart-io/ProcessSignal-class.html)
- [Command Line Interface Guidelines](https://clig.dev/)
- [NO_COLOR Standard for CLI Tools](https://no-color.org/)
- [ANSI Escape Codes Reference](https://en.wikipedia.org/wiki/ANSI_escape_code)
- [BSD sysexits.h Exit Code Conventions](https://man.freebsd.org/cgi/man.cgi?query=sysexits)
- [Dart process_run Package](https://pub.dev/packages/process_run)
- [Dart watcher Package for File System Events](https://pub.dev/packages/watcher)
