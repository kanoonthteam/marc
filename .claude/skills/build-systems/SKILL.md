---
name: build-systems
description: Dart CLI/build tooling covering file watchers (dart:io watch API), incremental builds with SHA-256 hash-based change detection, build_runner architecture (Builder, BuildStep, phases), source_gen code generation, build graph dependency tracking, artifact caching/invalidation, parallel execution, standalone watcher mode, and build manifest/lockfile patterns
---

# Dart Build Systems & File Watchers

Comprehensive reference for building CLI-grade build tooling in Dart 3.x. Covers file system watching with `dart:io`, hash-based incremental builds, `package:build_runner` architecture, custom `Builder` implementations, `package:source_gen` code generation, build graph dependency tracking, artifact caching, parallel execution strategies, and build manifest patterns.

## Table of Contents

1. [File System Watching with dart:io](#file-system-watching-with-dartio)
2. [File System Event Types](#file-system-event-types)
3. [Event Debouncing and Coalescing](#event-debouncing-and-coalescing)
4. [Hash-Based Incremental Builds](#hash-based-incremental-builds)
5. [Build Artifact Caching and Invalidation](#build-artifact-caching-and-invalidation)
6. [build_runner Architecture](#build_runner-architecture)
7. [Custom Builder Implementation](#custom-builder-implementation)
8. [Code Generation with source_gen](#code-generation-with-source_gen)
9. [Build Graph Dependency Tracking](#build-graph-dependency-tracking)
10. [Parallel Build Execution](#parallel-build-execution)
11. [Standalone Watcher Mode](#standalone-watcher-mode)
12. [Performance Optimization](#performance-optimization)
13. [Build Manifest and Lockfile Patterns](#build-manifest-and-lockfile-patterns)
14. [Best Practices](#best-practices)
15. [Anti-Patterns](#anti-patterns)
16. [Sources & References](#sources--references)

---

## File System Watching with dart:io

Dart's `dart:io` library provides native file system watching through `FileSystemEntity.watch()` and `Directory.watch()`. These APIs expose a `Stream<FileSystemEvent>` that emits events when files or directories change on disk.

### FileSystemEntity.watch()

Every `FileSystemEntity` (File, Directory, Link) exposes a `watch()` method. On macOS it uses FSEvents, on Linux it uses inotify, and on Windows it uses ReadDirectoryChangesW.

Key parameters:
- `events` -- bitmask of `FileSystemEvent` types to listen for (defaults to `FileSystemEvent.all`)
- `recursive` -- whether to watch subdirectories (defaults to `false`)

### Directory.watch()

`Directory.watch()` is the most common entry point for build systems. It monitors an entire directory tree when `recursive: true` is set.

Important considerations:
- Recursive watching on Linux uses one inotify watch per subdirectory. The kernel default limit is 8192 watches (`/proc/sys/fs/inotify/max_user_watches`). Large projects may need this raised.
- On macOS, FSEvents handles recursive watching natively with minimal overhead.
- Symlinks are not followed by default. If your project uses symlinked packages, you must watch each resolved path separately.
- The stream will emit an error and close if the watched directory is deleted.

### Basic Watch Setup

```dart
import 'dart:async';
import 'dart:io';

/// Watches a directory tree for file changes, applying debouncing
/// to coalesce rapid successive events into a single rebuild trigger.
class DirectoryWatcher {
  final Directory _directory;
  final Duration _debounceDuration;
  StreamSubscription<FileSystemEvent>? _subscription;
  Timer? _debounceTimer;
  final Set<String> _pendingChanges = {};
  final void Function(Set<String> changedPaths) _onChanges;

  DirectoryWatcher({
    required String path,
    required void Function(Set<String> changedPaths) onChanges,
    Duration debounceDuration = const Duration(milliseconds: 300),
  })  : _directory = Directory(path),
        _onChanges = onChanges,
        _debounceDuration = debounceDuration;

  /// Starts watching the directory recursively for all event types.
  void start() {
    if (!_directory.existsSync()) {
      throw FileSystemException(
        'Cannot watch non-existent directory',
        _directory.path,
      );
    }

    _subscription = _directory
        .watch(events: FileSystemEvent.all, recursive: true)
        .where(_shouldProcess)
        .listen(
          _handleEvent,
          onError: _handleError,
          onDone: _handleDone,
        );
  }

  /// Filters out events for generated files, hidden directories,
  /// and build output to prevent infinite rebuild loops.
  bool _shouldProcess(FileSystemEvent event) {
    final path = event.path;
    // Skip hidden directories (.dart_tool, .git, etc.)
    if (path.contains('${Platform.pathSeparator}.')) return false;
    // Skip build output directory
    if (path.contains('${Platform.pathSeparator}build${Platform.pathSeparator}')) {
      return false;
    }
    // Skip generated files
    if (path.endsWith('.g.dart') || path.endsWith('.freezed.dart')) {
      return false;
    }
    // Only watch Dart source and config files
    return path.endsWith('.dart') ||
        path.endsWith('.yaml') ||
        path.endsWith('.json');
  }

  void _handleEvent(FileSystemEvent event) {
    _pendingChanges.add(event.path);
    _debounceTimer?.cancel();
    _debounceTimer = Timer(_debounceDuration, _flushChanges);
  }

  void _flushChanges() {
    if (_pendingChanges.isNotEmpty) {
      final changes = Set<String>.from(_pendingChanges);
      _pendingChanges.clear();
      _onChanges(changes);
    }
  }

  void _handleError(Object error) {
    stderr.writeln('Watch error: $error');
  }

  void _handleDone() {
    stderr.writeln('Watch stream closed for ${_directory.path}');
  }

  /// Stops watching and releases resources.
  Future<void> stop() async {
    _debounceTimer?.cancel();
    await _subscription?.cancel();
    _pendingChanges.clear();
  }
}
```

---

## File System Event Types

`dart:io` defines four event types as subclasses of `FileSystemEvent`:

| Event Type | Constant | Value | Description |
|---|---|---|---|
| `FileSystemCreateEvent` | `FileSystemEvent.create` | 1 | File or directory created |
| `FileSystemModifyEvent` | `FileSystemEvent.modify` | 2 | File contents or metadata changed |
| `FileSystemDeleteEvent` | `FileSystemEvent.delete` | 4 | File or directory deleted |
| `FileSystemMoveEvent` | `FileSystemEvent.move` | 8 | File or directory renamed/moved |

### Event Characteristics by Platform

**macOS (FSEvents):**
- Move events report the destination path. The source path is lost unless you track it by inode.
- Modify events may fire multiple times for a single write (editor save strategies vary).
- Events are batched by the OS with a default latency of ~1 second.

**Linux (inotify):**
- Move events fire as paired `IN_MOVED_FROM` / `IN_MOVED_TO` events. Dart surfaces only the final `move` event with the destination path.
- Modify events fire for both content changes and metadata changes (permissions, timestamps).
- Each subdirectory requires a separate inotify watch descriptor.

**Windows (ReadDirectoryChangesW):**
- Move events appear as a delete followed by a create. Dart does not synthesize a `move` event on Windows.
- Buffer overflows under heavy load cause missed events. The watcher silently drops events rather than erroring.

### Selective Event Filtering

You can subscribe to specific event types by combining bitmask values:

```dart
// Watch only for creates and deletes (skip modify and move)
final stream = directory.watch(
  events: FileSystemEvent.create | FileSystemEvent.delete,
  recursive: true,
);
```

For build systems, watching `FileSystemEvent.all` and filtering in the stream handler is usually preferred. This allows different rebuild strategies for different event types (e.g., a full rebuild on delete vs. an incremental rebuild on modify).

---

## Event Debouncing and Coalescing

File system events arrive at high frequency during common operations. A single "Save All" in an IDE can produce dozens of modify events across multiple files within milliseconds. Without debouncing, a build system would trigger redundant rebuilds.

### Debouncing Strategy

Debouncing delays action until a quiet period elapses after the last event. The `DirectoryWatcher` example above uses this approach with a 300ms window. This is the simplest and most common strategy.

Recommended debounce durations:
- **100-200ms** for fast incremental builds (hash checks only)
- **300-500ms** for builds that invoke code generation
- **1000ms+** for full project rebuilds or test re-runs

### Coalescing Strategy

Coalescing groups events by path and keeps only the most recent event type. This prevents processing a file that was created and then immediately deleted, or handling a modify event for a file that no longer exists.

Event priority for coalescing (highest wins):
1. **Delete** -- if the file is gone, nothing else matters
2. **Create** -- if it was deleted then re-created, treat as create
3. **Modify** -- standard content change
4. **Move** -- treat as delete of old path + create of new path

### Coalescing Implementation

```dart
import 'dart:async';
import 'dart:io';

/// Event coalescer that deduplicates and prioritizes file system events
/// within a configurable time window.
class EventCoalescer {
  final Duration window;
  final void Function(Map<String, FileSystemEvent> events) onFlush;

  Timer? _timer;
  final Map<String, FileSystemEvent> _pending = {};

  /// Priority order: delete > create > modify > move.
  /// Higher value = higher priority.
  static const _priority = {
    FileSystemEvent.delete: 4,
    FileSystemEvent.create: 3,
    FileSystemEvent.modify: 2,
    FileSystemEvent.move: 1,
  };

  EventCoalescer({
    required this.onFlush,
    this.window = const Duration(milliseconds: 300),
  });

  void add(FileSystemEvent event) {
    final existing = _pending[event.path];
    if (existing == null || _shouldReplace(existing, event)) {
      _pending[event.path] = event;
    }

    _timer?.cancel();
    _timer = Timer(window, _flush);
  }

  bool _shouldReplace(FileSystemEvent existing, FileSystemEvent incoming) {
    final existingPriority = _priority[existing.type] ?? 0;
    final incomingPriority = _priority[incoming.type] ?? 0;
    return incomingPriority >= existingPriority;
  }

  void _flush() {
    if (_pending.isNotEmpty) {
      final events = Map<String, FileSystemEvent>.from(_pending);
      _pending.clear();
      onFlush(events);
    }
  }

  void dispose() {
    _timer?.cancel();
    _pending.clear();
  }
}
```

### Batch Window vs. Individual Debounce

Two common patterns exist:

**Batch window (preferred for build systems):** Collect all events for N milliseconds after the first event, then process the entire batch. This guarantees a maximum latency of N ms from first event to build start.

**Individual debounce (preferred for single-file tools):** Reset the timer on every event. The build only starts after N ms of silence. This is better when you expect a burst of saves and want to wait for the user to finish.

For `build_runner`-style systems, the batch window approach is preferred because it provides predictable latency. The individual debounce approach can cause unpredictable delays when the user is actively editing -- each keystroke (with auto-save) resets the timer.

---

## Hash-Based Incremental Builds

Timestamp-based change detection is unreliable: cloning a repo, switching branches, or restoring from backup changes timestamps without changing content. Hash-based detection compares SHA-256 digests of file contents to determine what actually changed.

### SHA-256 Fingerprinting

Dart's `package:crypto` provides SHA-256 hashing. For build systems, hash the file content and compare against a stored manifest of previous hashes.

```dart
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';

/// Manages SHA-256 content hashes for incremental build change detection.
/// Stores hashes in a JSON manifest file alongside the build output.
class ContentHashRegistry {
  final String _manifestPath;
  Map<String, String> _hashes = {};

  ContentHashRegistry({required String buildDirectory})
      : _manifestPath = '$buildDirectory/.build_hashes.json';

  /// Loads the hash manifest from disk. Returns false if no manifest exists
  /// (first build scenario).
  bool load() {
    final file = File(_manifestPath);
    if (!file.existsSync()) return false;

    try {
      final json = jsonDecode(file.readAsStringSync()) as Map<String, dynamic>;
      _hashes = json.map((k, v) => MapEntry(k, v as String));
      return true;
    } on FormatException {
      // Corrupt manifest; treat as first build.
      _hashes.clear();
      return false;
    }
  }

  /// Persists the current hash state to disk atomically.
  /// Writes to a temp file first, then renames to avoid partial writes
  /// if the process is killed mid-write.
  void save() {
    final file = File(_manifestPath);
    final tempFile = File('$_manifestPath.tmp');
    tempFile.writeAsStringSync(jsonEncode(_hashes));
    tempFile.renameSync(file.path);
  }

  /// Computes the SHA-256 digest of a file's contents.
  /// Returns the hex-encoded hash string.
  String _computeHash(File file) {
    final bytes = file.readAsBytesSync();
    return sha256.convert(bytes).toString();
  }

  /// Checks whether a file has changed since the last recorded hash.
  /// Returns true if the file is new, modified, or if no previous hash exists.
  bool hasChanged(String filePath) {
    final file = File(filePath);
    if (!file.existsSync()) {
      // File was deleted; it "changed" in the sense that it's gone.
      return _hashes.containsKey(filePath);
    }

    final currentHash = _computeHash(file);
    final previousHash = _hashes[filePath];
    return currentHash != previousHash;
  }

  /// Updates the stored hash for a file after a successful build.
  void recordHash(String filePath) {
    final file = File(filePath);
    if (file.existsSync()) {
      _hashes[filePath] = _computeHash(file);
    } else {
      _hashes.remove(filePath);
    }
  }

  /// Removes entries for files that no longer exist on disk.
  /// Call this during a full clean to prune stale entries.
  void pruneDeleted() {
    _hashes.removeWhere((path, _) => !File(path).existsSync());
  }

  /// Returns the set of all tracked file paths.
  Set<String> get trackedPaths => _hashes.keys.toSet();

  /// Determines which files in a given set have changed since last build.
  Set<String> computeChangedFiles(Iterable<String> filePaths) {
    return filePaths.where(hasChanged).toSet();
  }
}
```

### Streaming Hash for Large Files

For files larger than a few megabytes, avoid loading the entire file into memory. Use streaming digest computation instead:

```dart
import 'dart:io';

import 'package:crypto/crypto.dart';

/// Computes SHA-256 hash of a file using streaming reads.
/// Suitable for large files that should not be loaded entirely into memory.
Future<String> computeStreamingHash(String filePath) async {
  final file = File(filePath);
  final output = AccumulatorSink<Digest>();
  final input = sha256.startChunkedConversion(output);

  await for (final chunk in file.openRead()) {
    input.add(chunk);
  }
  input.close();

  return output.events.single.toString();
}
```

### Multi-Input Hash for Build Steps

When a build step depends on multiple inputs, compute a combined hash from all input files to determine if the step needs re-execution:

```dart
/// Computes a combined hash from multiple input files.
/// The hash changes if any input file changes, is added, or is removed.
String computeCombinedHash(List<String> inputPaths) {
  final sortedPaths = List<String>.from(inputPaths)..sort();
  final output = AccumulatorSink<Digest>();
  final input = sha256.startChunkedConversion(output);

  for (final path in sortedPaths) {
    // Include the path itself so renaming is detected.
    input.add(utf8.encode(path));
    final file = File(path);
    if (file.existsSync()) {
      input.add(file.readAsBytesSync());
    } else {
      // Sentinel value for missing files.
      input.add(utf8.encode('<deleted>'));
    }
  }
  input.close();

  return output.events.single.toString();
}
```

---

## Build Artifact Caching and Invalidation

A build cache stores the outputs of previous build steps keyed by input hash. When the same inputs appear again, the cached output is reused without re-running the build step.

### Cache Structure

A typical on-disk cache layout:

```
.build_cache/
  manifest.json          # Maps input hashes to output paths
  artifacts/
    <sha256>/            # One directory per unique input hash
      output.dart        # Cached build output
      metadata.json      # Timestamp, builder version, input paths
```

### Cache Invalidation Rules

A cached artifact is invalid when:

1. **Input content changed** -- the SHA-256 of any input file differs from the hash used to produce the cached output.
2. **Builder version changed** -- the builder that produced the artifact has been updated. Embed the builder version in the cache key.
3. **Configuration changed** -- build options (e.g., `build.yaml` settings) differ. Hash the relevant config alongside inputs.
4. **Dependency changed** -- a transitive dependency of the input file changed. This requires build graph tracking (covered in a later section).
5. **Cache TTL expired** -- for remote caches, enforce a maximum age to prevent unbounded growth.

### Cache Key Computation

The cache key should include:
- Sorted list of input file paths and their content hashes
- Builder identifier and version string
- Relevant build configuration values
- Dart SDK version (codegen output may differ between SDK versions)

```
cache_key = SHA-256(
  builder_name + ":" + builder_version + "\n" +
  dart_sdk_version + "\n" +
  sorted_input_path_1 + ":" + input_hash_1 + "\n" +
  sorted_input_path_2 + ":" + input_hash_2 + "\n" +
  ...
  config_hash + "\n"
)
```

### Atomic Cache Writes

Always write cache entries atomically. Write to a temporary directory, then rename into the cache. This prevents partial cache entries from being read if the process is interrupted.

---

## build_runner Architecture

`package:build_runner` is Dart's official build system for code generation and asset transformation. It provides a declarative, incremental build pipeline with automatic dependency tracking.

### Core Concepts

**Builder:** A class that implements the `Builder` interface. It declares which file extensions it reads (inputs) and which it writes (outputs). Each builder is a pure function from inputs to outputs.

**BuildStep:** A single invocation of a builder on one primary input asset. The `BuildStep` provides methods to read additional assets, write output assets, and log messages.

**AssetId:** A unique identifier for a file in the build, consisting of a package name and a path within that package (e.g., `my_package|lib/src/model.dart`).

**Phase:** A group of builders that run together. Builders within the same phase run in parallel on independent inputs. Phases run sequentially.

**Resolver:** Provides `package:analyzer` element model access for Dart source files. Used by `source_gen` to inspect annotations, classes, and types.

### build.yaml Configuration

The `build.yaml` file declares builders, their targets, and configuration:

```yaml
targets:
  $default:
    builders:
      my_package|my_builder:
        enabled: true
        generate_for:
          include:
            - lib/src/models/**
          exclude:
            - lib/src/models/legacy/**
        options:
          generate_toString: true
          field_rename: snake_case

builders:
  my_builder:
    import: "package:my_package/builder.dart"
    builder_factories: ["myBuilder"]
    build_extensions: {".dart": [".g.dart"]}
    auto_apply: dependents
    build_to: source
    applies_builders: ["source_gen|combining_builder"]
```

### Key build.yaml Fields

- `import` -- The Dart library containing the builder factory function.
- `builder_factories` -- List of top-level function names that return a `Builder`.
- `build_extensions` -- Maps input extensions to output extensions. `{".dart": [".g.dart"]}` means for every `.dart` input, produce a `.g.dart` output.
- `auto_apply` -- When to automatically apply this builder: `none`, `dependents`, `all_packages`, `root_package`.
- `build_to` -- Where to put output: `source` (next to input file) or `cache` (in `.dart_tool/build`).
- `applies_builders` -- Other builders that must run after this one.
- `required_inputs` -- Extensions this builder needs other builders to produce first.

### Build Phases and Ordering

Builders are organized into phases based on their dependency relationships:

1. **Phase 1:** Builders with no `required_inputs`. These read only source files.
2. **Phase 2:** Builders whose `required_inputs` are produced by Phase 1 builders.
3. **Phase N:** Builders that depend on outputs from Phase N-1.

Within a phase, builders run in parallel across different input files. This is safe because each builder writes to a unique output path determined by its `build_extensions`.

### Running build_runner

Common commands:

```bash
# One-time build
dart run build_runner build

# Watch mode (rebuild on file changes)
dart run build_runner watch

# Clean generated files and build cache
dart run build_runner clean

# Build with verbose output
dart run build_runner build --verbose

# Build specific targets
dart run build_runner build --build-filter="lib/src/models/*"

# Delete conflicting outputs before building
dart run build_runner build --delete-conflicting-outputs
```

---

## Custom Builder Implementation

To create a custom builder, implement the `Builder` interface from `package:build`:

```dart
import 'dart:async';

import 'package:build/build.dart';
import 'package:glob/glob.dart';

/// A builder that generates a barrel file (index) exporting all public
/// Dart files in a directory. Reads all .dart files under lib/src/ and
/// produces a single lib/src/index.g.dart with export statements.
class BarrelFileBuilder implements Builder {
  @override
  Map<String, List<String>> get buildExtensions => {
        r'$lib$': ['src/index.g.dart'],
      };

  @override
  Future<void> build(BuildStep buildStep) async {
    // Find all Dart files under lib/src/, excluding generated files.
    final glob = Glob('lib/src/**.dart');
    final assets = await buildStep.findAssets(glob).toList();

    final exports = <String>[];
    for (final asset in assets) {
      // Skip generated files to avoid circular exports.
      if (asset.path.endsWith('.g.dart') ||
          asset.path.endsWith('.freezed.dart')) {
        continue;
      }
      // Skip private files (starting with underscore).
      final filename = asset.pathSegments.last;
      if (filename.startsWith('_')) continue;

      exports.add("export '${asset.path.replaceFirst('lib/', '')}';");
    }

    exports.sort();

    final output = AssetId(
      buildStep.inputId.package,
      'lib/src/index.g.dart',
    );

    final content = StringBuffer()
      ..writeln('// GENERATED CODE - DO NOT MODIFY BY HAND')
      ..writeln('// Generated by BarrelFileBuilder')
      ..writeln()
      ..writeAll(exports, '\n')
      ..writeln();

    await buildStep.writeAsString(output, content.toString());
  }
}

/// Builder factory function referenced in build.yaml.
Builder barrelFileBuilder(BuilderOptions options) => BarrelFileBuilder();
```

### Builder Factory Function

The builder factory is a top-level function that `build_runner` calls to instantiate your builder. It receives `BuilderOptions` containing configuration from `build.yaml`:

```dart
Builder myBuilder(BuilderOptions options) {
  final generateToString = options.config['generate_toString'] as bool? ?? true;
  final fieldRename = options.config['field_rename'] as String? ?? 'none';

  return MyCodegenBuilder(
    generateToString: generateToString,
    fieldRename: fieldRename,
  );
}
```

### PostProcessBuilder

A `PostProcessBuilder` runs after all regular builders complete. It is used for cleanup, aggregation, or final transformations:

```dart
class CleanupBuilder extends PostProcessBuilder {
  @override
  Iterable<String> get inputExtensions => ['.g.dart'];

  @override
  Future<void> build(PostProcessBuildStep buildStep) async {
    // Post-process the generated file, e.g., add a license header.
    final content = await buildStep.readInputAsString();
    if (!content.startsWith('// Copyright')) {
      // Cannot modify in PostProcessBuilder, but can delete.
      // Use this for cleanup tasks like removing intermediate files.
      buildStep.deletePrimaryInput();
    }
  }
}
```

---

## Code Generation with source_gen

`package:source_gen` builds on top of `package:build` to provide annotation-driven code generation. It handles the boilerplate of reading Dart source, resolving types, and writing output files.

### GeneratorForAnnotation

The most common pattern: generate code for every class annotated with a specific annotation.

```dart
import 'package:analyzer/dart/element/element.dart';
import 'package:build/build.dart';
import 'package:source_gen/source_gen.dart';

/// Annotation to mark classes for data class generation.
class DataClass {
  final bool generateCopyWith;
  final bool generateEquality;

  const DataClass({
    this.generateCopyWith = true,
    this.generateEquality = true,
  });
}

/// Generator that produces copyWith, ==, hashCode, and toString
/// for classes annotated with @DataClass().
class DataClassGenerator extends GeneratorForAnnotation<DataClass> {
  @override
  String generateForAnnotatedElement(
    Element element,
    ConstantReader annotation,
    BuildStep buildStep,
  ) {
    if (element is! ClassElement) {
      throw InvalidGenerationSourceError(
        '@DataClass() can only be applied to classes.',
        element: element,
      );
    }

    final className = element.name;
    final generateCopyWith =
        annotation.read('generateCopyWith').boolValue;
    final generateEquality =
        annotation.read('generateEquality').boolValue;

    final fields = element.fields
        .where((f) => !f.isStatic && !f.isSynthetic)
        .toList();

    final buffer = StringBuffer();

    // Extension on the annotated class with generated methods.
    buffer.writeln('extension \$${className}Extension on $className {');

    if (generateCopyWith) {
      _writeCopyWith(buffer, className, fields);
    }

    if (generateEquality) {
      _writeEquality(buffer, className, fields);
    }

    _writeToString(buffer, className, fields);

    buffer.writeln('}');

    return buffer.toString();
  }

  void _writeCopyWith(
    StringBuffer buffer,
    String className,
    List<FieldElement> fields,
  ) {
    buffer.writeln('  $className copyWith({');
    for (final field in fields) {
      buffer.writeln('    ${field.type}? ${field.name},');
    }
    buffer.writeln('  }) {');
    buffer.writeln('    return $className(');
    for (final field in fields) {
      buffer.writeln('      ${field.name}: ${field.name} ?? this.${field.name},');
    }
    buffer.writeln('    );');
    buffer.writeln('  }');
  }

  void _writeEquality(
    StringBuffer buffer,
    String className,
    List<FieldElement> fields,
  ) {
    buffer.writeln('  bool equals(Object other) {');
    buffer.writeln('    if (identical(this, other)) return true;');
    buffer.writeln('    return other is $className &&');
    for (var i = 0; i < fields.length; i++) {
      final field = fields[i];
      final suffix = i < fields.length - 1 ? ' &&' : ';';
      buffer.writeln(
        '        other.${field.name} == ${field.name}$suffix',
      );
    }
    buffer.writeln('  }');
    buffer.writeln();
    buffer.writeln('  int get computedHashCode => Object.hash(');
    buffer.writeln(fields.map((f) => '        ${f.name}').join(',\n'));
    buffer.writeln('      );');
  }

  void _writeToString(
    StringBuffer buffer,
    String className,
    List<FieldElement> fields,
  ) {
    buffer.writeln("  String toDebugString() => '$className('");
    for (var i = 0; i < fields.length; i++) {
      final field = fields[i];
      final separator = i < fields.length - 1 ? ', ' : '';
      buffer.writeln(
        "      '${field.name}: \${${field.name}}$separator'",
      );
    }
    buffer.writeln("      ')';");
  }
}

/// Builder factory for build.yaml.
Builder dataClassBuilder(BuilderOptions options) =>
    SharedPartBuilder([DataClassGenerator()], 'data_class');
```

### SharedPartBuilder vs. PartBuilder vs. LibraryBuilder

- **SharedPartBuilder** -- Generates code that goes into a shared `.g.dart` part file. Multiple generators can contribute to the same `.g.dart` file. This is the most common choice.
- **PartBuilder** -- Generates code into a dedicated part file (e.g., `.data_class.g.dart`). Use when you want isolation from other generators.
- **LibraryBuilder** -- Generates a standalone library file, not a part file. Use for builders that produce independent files (barrel files, route tables, etc.).

### ConstantReader

`ConstantReader` provides type-safe access to annotation values:

```dart
// Read a string field with a default
final prefix = annotation.peek('prefix')?.stringValue ?? '';

// Read an enum field
final mode = annotation.read('mode').objectValue;
final modeName = mode.getField('_name')!.toStringValue()!;

// Read a list field
final includes = annotation.read('includes')
    .listValue
    .map((e) => e.toStringValue()!)
    .toList();

// Check if a field was explicitly provided
final hasCustomName = !annotation.read('name').isNull;
```

---

## Build Graph Dependency Tracking

A build graph models the relationships between source files, build steps, and output files. Accurate dependency tracking is essential for minimal incremental rebuilds.

### Dependency Graph Structure

The graph has three types of nodes:

1. **Source nodes** -- files on disk that are not produced by any builder.
2. **Generated nodes** -- files produced by a builder. Each has exactly one producing builder.
3. **Builder nodes** -- represent a builder invocation. Each has a set of input edges and output edges.

Edges represent dependencies:
- **Input edges** -- from builder node to the source/generated nodes it reads.
- **Output edges** -- from builder node to the generated nodes it writes.
- **Transitive edges** -- if file A imports file B, and a builder reads A, then the builder transitively depends on B.

### Invalidation Walk

When a source file changes, the invalidation algorithm:

1. Mark the source node as dirty.
2. Find all builder nodes that have an input edge to this source node.
3. Mark those builder nodes as needing re-execution.
4. Mark all output nodes of those builders as dirty.
5. Recursively apply steps 2-4 for generated nodes that are inputs to other builders.

This is a breadth-first walk through the graph starting from the changed source nodes.

### Import Graph for Transitive Dependencies

Dart files have implicit dependencies through `import` and `export` statements. A builder that reads `model.dart` implicitly depends on everything `model.dart` imports, because changes to those imports can change the resolved type information.

`build_runner` uses the Dart analyzer to resolve the import graph and track these transitive dependencies automatically. For custom standalone build systems, you need to parse imports yourself:

```dart
import 'dart:io';

/// Parses import/export URIs from a Dart source file.
/// Does NOT resolve the URIs to file paths -- call resolveUri() separately.
List<String> parseImports(String filePath) {
  final content = File(filePath).readAsStringSync();
  final importPattern = RegExp(
    r'''(?:import|export)\s+['"](.*?)['"]''',
    multiLine: true,
  );

  return importPattern
      .allMatches(content)
      .map((m) => m.group(1)!)
      .where((uri) => !uri.startsWith('dart:')) // Skip SDK imports
      .toList();
}

/// Builds a full transitive dependency graph for a set of root files.
/// Returns a map from file path to all transitive dependencies.
Map<String, Set<String>> buildDependencyGraph(
  List<String> rootFiles,
  String Function(String uri, String fromFile) resolveUri,
) {
  final graph = <String, Set<String>>{};
  final visited = <String>{};

  void visit(String filePath) {
    if (visited.contains(filePath)) return;
    visited.add(filePath);

    final deps = <String>{};
    final imports = parseImports(filePath);

    for (final uri in imports) {
      try {
        final resolved = resolveUri(uri, filePath);
        deps.add(resolved);
        visit(resolved); // Recurse into dependencies
      } on FileSystemException {
        // External package or missing file; skip.
      }
    }

    graph[filePath] = deps;
  }

  for (final root in rootFiles) {
    visit(root);
  }

  return graph;
}

/// Returns all files transitively affected by a change to [changedFile].
Set<String> findAffectedFiles(
  String changedFile,
  Map<String, Set<String>> graph,
) {
  // Build reverse dependency map (dependents).
  final reverseDeps = <String, Set<String>>{};
  for (final entry in graph.entries) {
    for (final dep in entry.value) {
      reverseDeps.putIfAbsent(dep, () => {}).add(entry.key);
    }
  }

  // BFS from changed file through reverse dependencies.
  final affected = <String>{};
  final queue = [changedFile];
  while (queue.isNotEmpty) {
    final current = queue.removeLast();
    if (affected.add(current)) {
      queue.addAll(reverseDeps[current] ?? {});
    }
  }

  return affected;
}
```

---

## Parallel Build Execution

Build steps that operate on independent files can run in parallel. Dart's isolate model and `Future`-based concurrency make this straightforward.

### Concurrency with Future.wait

For I/O-bound build steps (most code generation), `Future.wait` provides simple parallelism:

```dart
/// Runs build steps in parallel with a configurable concurrency limit.
/// Returns a list of results in the same order as the input steps.
Future<List<T>> runParallel<T>(
  List<Future<T> Function()> tasks, {
  int maxConcurrency = 4,
}) async {
  final results = List<T?>.filled(tasks.length, null);
  var nextIndex = 0;

  Future<void> worker() async {
    while (true) {
      final index = nextIndex++;
      if (index >= tasks.length) return;
      results[index] = await tasks[index]();
    }
  }

  final workers = List.generate(
    maxConcurrency.clamp(1, tasks.length),
    (_) => worker(),
  );
  await Future.wait(workers);

  return results.cast<T>();
}
```

### Isolate-Based Parallelism for CPU-Bound Steps

For CPU-bound work (e.g., hashing large files, complex code analysis), use isolates to leverage multiple CPU cores:

```dart
import 'dart:isolate';

/// Runs a pure function in a separate isolate.
/// The function and its argument must be sendable across isolate boundaries.
Future<R> runInIsolate<A, R>(R Function(A) function, A argument) async {
  return await Isolate.run(() => function(argument));
}
```

### Phase-Level Parallelism

Builders within the same phase can execute in parallel because they write to non-overlapping output paths. The execution model:

1. Determine phase ordering from builder dependency declarations.
2. Within each phase, partition input files across available workers.
3. Execute all builders in the current phase in parallel.
4. Wait for all builders in the phase to complete.
5. Move to the next phase.

`build_runner` handles this automatically. For custom build systems, implement a simple phase executor:

```dart
/// Executes build phases sequentially, with parallel execution within each phase.
Future<void> executeBuildPhases(
  List<List<BuildTask>> phases, {
  int concurrency = 4,
}) async {
  for (final (index, phase) in phases.indexed) {
    stdout.writeln('Phase ${index + 1}/${phases.length}: '
        '${phase.length} tasks');

    await runParallel(
      phase.map((task) => () => task.execute()).toList(),
      maxConcurrency: concurrency,
    );
  }
}
```

---

## Standalone Watcher Mode

A standalone watcher mode operates independently of `build_runner`. This is useful for custom build pipelines, asset processing, or non-Dart codegen that does not fit `build_runner`'s model.

### Watcher Architecture

A production-grade standalone watcher combines:
1. **Directory watcher** with event debouncing
2. **Change detector** with hash-based comparison
3. **Dependency graph** for incremental invalidation
4. **Build executor** with parallel phase execution
5. **Error recovery** that continues watching after build failures

### Watcher Lifecycle

1. **Initial build** -- run a full build on startup to ensure all outputs are up to date.
2. **Watch** -- monitor source directories for changes.
3. **Detect** -- debounce events, compute changed files, check hashes.
4. **Invalidate** -- walk the dependency graph to find all affected build steps.
5. **Rebuild** -- execute only the invalidated build steps.
6. **Record** -- update the hash manifest and dependency graph.
7. **Report** -- log results and timing.
8. **Loop** -- return to step 2.

### Graceful Shutdown

The watcher must handle SIGINT (Ctrl+C) and SIGTERM gracefully:

```dart
import 'dart:io';

/// Sets up signal handlers for graceful shutdown.
/// Returns a future that completes when a termination signal is received.
Future<void> awaitTermination() {
  final completer = Completer<void>();

  ProcessSignal.sigint.watch().first.then((_) {
    stdout.writeln('\nReceived SIGINT, shutting down...');
    completer.complete();
  });

  // SIGTERM is not available on Windows.
  if (!Platform.isWindows) {
    ProcessSignal.sigterm.watch().first.then((_) {
      stdout.writeln('\nReceived SIGTERM, shutting down...');
      completer.complete();
    });
  }

  return completer.future;
}
```

### Watch Mode with Hot Reload Support

For Dart CLI tools or server applications, the watcher can trigger a hot reload after rebuilding:

```dart
// After successful rebuild, send a reload signal to the running application
// via the Dart VM service protocol.
Future<void> triggerHotReload(String vmServiceUri) async {
  final client = await vmServiceConnectUri(vmServiceUri);
  final vm = await client.getVM();
  for (final isolateRef in vm.isolates ?? []) {
    await client.reloadSources(isolateRef.id!);
  }
  await client.dispose();
}
```

---

## Performance Optimization

### Minimizing File I/O

File I/O is the primary bottleneck in build systems. Strategies to minimize it:

**Read files once, cache in memory.** During a build, read each file at most once and pass the content to all consumers. Do not re-read a file to compute its hash and then again to parse it.

**Use `readAsBytesSync` over `readAsStringSync`.** Bytes avoid UTF-8 decode overhead when you only need to hash the content. Decode to string only when you need to parse the content.

**Batch stat calls.** Before computing hashes, stat all candidate files to check modification timestamps first. Skip hashing files whose timestamps have not changed since the last build. This is a two-tier strategy: fast timestamp check first, then expensive hash check only for files with changed timestamps.

```dart
/// Two-tier change detection: timestamp first, hash second.
/// This avoids expensive SHA-256 computation for unchanged files.
Set<String> detectChanges(
  Map<String, DateTime> lastModifiedTimes,
  ContentHashRegistry hashRegistry,
  List<String> filePaths,
) {
  final changed = <String>{};

  for (final path in filePaths) {
    final file = File(path);
    if (!file.existsSync()) {
      changed.add(path);
      continue;
    }

    final stat = file.statSync();
    final lastModified = lastModifiedTimes[path];

    // Fast path: timestamp unchanged, skip hash check.
    if (lastModified != null && stat.modified == lastModified) {
      continue;
    }

    // Slow path: timestamp changed, verify with content hash.
    lastModifiedTimes[path] = stat.modified;
    if (hashRegistry.hasChanged(path)) {
      changed.add(path);
    }
  }

  return changed;
}
```

### Memory-Mapped Files

For very large files (multi-megabyte generated code, asset bundles), memory-mapped I/O avoids loading the entire file into the Dart heap. Dart does not provide a built-in mmap API, but you can use `dart:ffi` to call the OS-level `mmap` / `MapViewOfFile`:

Key considerations:
- Memory-mapped files share OS page cache, reducing total memory usage when multiple processes read the same file.
- Mapped regions must be explicitly unmapped to avoid resource leaks.
- For build systems, memory-mapping is only worthwhile for files larger than ~1MB. Below that threshold, the FFI overhead exceeds the benefit.
- The `package:ffi` and `package:native_assets_cli` ecosystems are evolving in Dart 3.x. Check the latest status before committing to an FFI-based approach.

### File System Operation Batching

Group related file operations together to minimize context switches between Dart and the OS:

- **Parallel reads:** Use `Future.wait` to read multiple files concurrently rather than sequentially.
- **Write coalescing:** Buffer all outputs in memory, then write them all at once at the end of a build step.
- **Directory listing cache:** Cache `Directory.listSync()` results for the duration of a single build. Directory contents do not change mid-build.

---

## Build Manifest and Lockfile Patterns

### Build Manifest

A build manifest records the complete state of a build: which files were processed, what outputs were produced, what hashes were computed, and which builder versions were used. This enables:

- **Incremental builds** -- compare current state against manifest to find what changed.
- **Build reproducibility** -- given the same manifest, the same outputs should be produced.
- **Cache validation** -- verify that cached artifacts match the expected state.

Manifest structure (JSON):

```json
{
  "version": 2,
  "dart_sdk": "3.3.0",
  "timestamp": "2026-02-25T10:30:00Z",
  "builders": {
    "my_package|data_class_builder": {
      "version": "1.2.0",
      "config_hash": "a1b2c3d4..."
    }
  },
  "sources": {
    "lib/src/model.dart": {
      "hash": "e5f6a7b8...",
      "modified": "2026-02-25T10:29:50Z"
    }
  },
  "outputs": {
    "lib/src/model.g.dart": {
      "hash": "c9d0e1f2...",
      "builder": "my_package|data_class_builder",
      "inputs": ["lib/src/model.dart"],
      "input_hash": "f3a4b5c6..."
    }
  }
}
```

### Lockfile Patterns

A build lockfile pins the exact versions and configurations used in a build. Unlike a manifest (which records what happened), a lockfile prescribes what should happen. This is analogous to `pubspec.lock` for dependencies.

Build lockfile use cases:
- **CI reproducibility** -- check the lockfile into version control so CI builds use the same builder versions and configurations.
- **Team consistency** -- ensure all developers generate identical output.
- **Upgrade tracking** -- diff the lockfile to see what changed between builds.

### Manifest Loading and Validation

```dart
import 'dart:convert';
import 'dart:io';

/// Represents the state of a previous build, loaded from the manifest file.
class BuildManifest {
  static const int currentVersion = 2;

  final int version;
  final String dartSdk;
  final DateTime timestamp;
  final Map<String, BuilderRecord> builders;
  final Map<String, SourceRecord> sources;
  final Map<String, OutputRecord> outputs;

  BuildManifest({
    required this.version,
    required this.dartSdk,
    required this.timestamp,
    required this.builders,
    required this.sources,
    required this.outputs,
  });

  /// Loads a manifest from disk. Returns null if the manifest does not exist
  /// or is from an incompatible version (requiring a full rebuild).
  static BuildManifest? load(String path) {
    final file = File(path);
    if (!file.existsSync()) return null;

    try {
      final json = jsonDecode(file.readAsStringSync()) as Map<String, dynamic>;
      final version = json['version'] as int;

      if (version != currentVersion) {
        stderr.writeln(
          'Build manifest version $version is incompatible '
          'with current version $currentVersion. '
          'Running full rebuild.',
        );
        return null;
      }

      return BuildManifest(
        version: version,
        dartSdk: json['dart_sdk'] as String,
        timestamp: DateTime.parse(json['timestamp'] as String),
        builders: (json['builders'] as Map<String, dynamic>).map(
          (k, v) => MapEntry(k, BuilderRecord.fromJson(v as Map<String, dynamic>)),
        ),
        sources: (json['sources'] as Map<String, dynamic>).map(
          (k, v) => MapEntry(k, SourceRecord.fromJson(v as Map<String, dynamic>)),
        ),
        outputs: (json['outputs'] as Map<String, dynamic>).map(
          (k, v) => MapEntry(k, OutputRecord.fromJson(v as Map<String, dynamic>)),
        ),
      );
    } on FormatException catch (e) {
      stderr.writeln('Corrupt build manifest: $e');
      return null;
    }
  }

  /// Saves the manifest atomically to disk.
  void save(String path) {
    final json = {
      'version': version,
      'dart_sdk': dartSdk,
      'timestamp': timestamp.toIso8601String(),
      'builders': builders.map((k, v) => MapEntry(k, v.toJson())),
      'sources': sources.map((k, v) => MapEntry(k, v.toJson())),
      'outputs': outputs.map((k, v) => MapEntry(k, v.toJson())),
    };

    final tempPath = '$path.tmp';
    File(tempPath).writeAsStringSync(
      const JsonEncoder.withIndent('  ').convert(json),
    );
    File(tempPath).renameSync(path);
  }

  /// Determines which sources have changed compared to current disk state.
  Set<String> findChangedSources(ContentHashRegistry currentHashes) {
    final changed = <String>{};

    // Check existing sources for modifications.
    for (final entry in sources.entries) {
      if (currentHashes.hasChanged(entry.key)) {
        changed.add(entry.key);
      }
    }

    // Check for new source files not in the manifest.
    for (final path in currentHashes.trackedPaths) {
      if (!sources.containsKey(path)) {
        changed.add(path);
      }
    }

    return changed;
  }
}

class BuilderRecord {
  final String version;
  final String configHash;

  BuilderRecord({required this.version, required this.configHash});

  factory BuilderRecord.fromJson(Map<String, dynamic> json) => BuilderRecord(
        version: json['version'] as String,
        configHash: json['config_hash'] as String,
      );

  Map<String, dynamic> toJson() => {
        'version': version,
        'config_hash': configHash,
      };
}

class SourceRecord {
  final String hash;
  final DateTime modified;

  SourceRecord({required this.hash, required this.modified});

  factory SourceRecord.fromJson(Map<String, dynamic> json) => SourceRecord(
        hash: json['hash'] as String,
        modified: DateTime.parse(json['modified'] as String),
      );

  Map<String, dynamic> toJson() => {
        'hash': hash,
        'modified': modified.toIso8601String(),
      };
}

class OutputRecord {
  final String hash;
  final String builder;
  final List<String> inputs;
  final String inputHash;

  OutputRecord({
    required this.hash,
    required this.builder,
    required this.inputs,
    required this.inputHash,
  });

  factory OutputRecord.fromJson(Map<String, dynamic> json) => OutputRecord(
        hash: json['hash'] as String,
        builder: json['builder'] as String,
        inputs: (json['inputs'] as List).cast<String>(),
        inputHash: json['input_hash'] as String,
      );

  Map<String, dynamic> toJson() => {
        'hash': hash,
        'builder': builder,
        'inputs': inputs,
        'input_hash': inputHash,
      };
}
```

---

## Best Practices

### File Watching

- **Always filter generated files from watch events.** Watching `.g.dart` or `.freezed.dart` files causes infinite rebuild loops where the build output triggers another build.
- **Set recursive: true for directory watches.** Missing subdirectory events is a common source of "works on my machine" bugs where some developers' IDEs trigger events at different directory levels.
- **Handle watch stream errors and closures.** The watch stream closes if the directory is deleted (e.g., during `git clean`). Re-establish the watch after the directory is recreated.
- **Use platform-specific debounce durations.** macOS FSEvents batches events with ~1s latency by default. Linux inotify delivers events near-instantly. Adjust debounce accordingly.

### Incremental Builds

- **Combine timestamp and hash checks.** Use the fast timestamp check as a first pass, then fall back to SHA-256 only for files with changed timestamps. This gives you the reliability of hashes with the speed of timestamps.
- **Include builder version in cache keys.** A builder upgrade may produce different output from the same input. Invalidate cached artifacts when the builder version changes.
- **Persist the hash manifest atomically.** Write to a temporary file and rename. A crash during write must not corrupt the manifest and force a full rebuild.
- **Prune stale entries from the manifest.** Deleted files should be removed from the manifest during each build cycle to prevent unbounded growth.

### Code Generation

- **Use `SharedPartBuilder` for annotation-driven generators.** This allows multiple generators to contribute to the same `.g.dart` file, reducing the number of `part` directives needed.
- **Validate annotations early with clear error messages.** Use `InvalidGenerationSourceError` to report problems with specific elements. Include the element name, the annotation, and what was expected.
- **Keep generated code minimal.** Generate only what is needed. Avoid generating documentation comments, excessive whitespace, or code that could be shared in a runtime library.
- **Test generators with `package:source_gen_test`.** Write golden-file tests that compare generated output against expected output files.

### Build Performance

- **Limit watcher concurrency.** Do not spawn unlimited parallel build tasks. Use a concurrency limit (typically 2-4x CPU cores for I/O-bound work, 1x for CPU-bound work).
- **Cache the Dart analyzer resolver.** Creating a new resolver for each build step is expensive. Reuse the resolver across build steps within the same build phase.
- **Avoid unnecessary `findAssets` calls.** Each `findAssets` glob evaluation scans the file system. Cache results within a build step when possible.

### Manifest and Caching

- **Version your manifest format.** Include a version field and reject incompatible versions with a full rebuild rather than silent corruption.
- **Use deterministic JSON serialization.** Sort map keys before writing JSON to ensure the manifest file is stable across runs (important for version control).
- **Separate the cache from source control.** Add `.build_cache/` and `.build_hashes.json` to `.gitignore`. These are machine-specific artifacts.

---

## Anti-Patterns

### Watching Generated Output

**Problem:** Watching `.g.dart` files triggers a rebuild when the build itself produces those files, creating an infinite loop.

**Fix:** Filter generated file extensions in the watch event handler. Maintain a set of known generated file patterns (`.g.dart`, `.freezed.dart`, `.mocks.dart`, etc.) and skip events matching those patterns.

### Timestamp-Only Change Detection

**Problem:** Relying solely on `File.lastModifiedSync()` for change detection. Timestamps change when switching git branches, restoring backups, or running `touch`. This causes unnecessary full rebuilds. Conversely, copying files from a build cache may preserve content but reset timestamps, causing missed rebuilds.

**Fix:** Use SHA-256 content hashing as the authoritative change detection mechanism. Use timestamps only as a fast pre-filter to skip unchanged files.

### Unbounded Parallelism

**Problem:** Launching `Future.wait` over thousands of build tasks simultaneously. This exhausts file descriptors, causes memory pressure, and often runs slower than a bounded approach due to OS scheduling overhead.

**Fix:** Use a semaphore or worker pool pattern to limit concurrency. A practical limit is `Platform.numberOfProcessors * 2` for I/O-bound tasks.

### Monolithic Builders

**Problem:** A single builder that reads every `.dart` file in the project and generates all outputs at once. Any change to any file invalidates the entire builder and triggers a full regeneration.

**Fix:** Design builders to operate on individual files or small groups. Each invocation should process one primary input and produce one output. This enables granular invalidation and parallel execution.

### Missing Atomic Writes

**Problem:** Writing build outputs and manifests directly to their final paths. If the build process crashes or is killed mid-write, the file is left in a corrupt partial state. The next build reads the corrupt file and either fails or produces incorrect output.

**Fix:** Always write to a temporary file in the same directory, then rename atomically. `File.renameSync()` is atomic on all platforms when source and destination are on the same filesystem.

### Hardcoded Paths

**Problem:** Using absolute paths or platform-specific path separators in build manifests and cache keys. The manifest becomes invalid when the project is moved, checked out in a different location, or built on a different OS.

**Fix:** Store paths relative to the project root in all manifests and cache keys. Use `package:path` for path manipulation instead of string concatenation.

### Ignoring Build Configuration in Cache Keys

**Problem:** Caching build outputs keyed only by input file hash, without including builder configuration. Changing a build option (e.g., `field_rename: snake_case` to `field_rename: camelCase`) returns stale cached output because the input file hash has not changed.

**Fix:** Include a hash of all relevant configuration values in the cache key. When configuration changes, all affected cache entries are automatically invalidated.

### Synchronous File Operations in Async Builders

**Problem:** Using `readAsStringSync()` and `writeAsStringSync()` inside async builders. This blocks the event loop and prevents other async operations (network, other file I/O) from progressing. In a parallel build system, synchronous I/O serializes execution despite using `Future.wait`.

**Fix:** Use asynchronous file operations (`readAsString()`, `writeAsString()`) in async contexts. Reserve synchronous operations for truly sequential code paths where no other async work can proceed.

### Not Handling Circular Dependencies

**Problem:** The dependency graph walker enters an infinite loop when file A imports B and B imports A (which is valid in Dart). The BFS/DFS traversal visits the same nodes repeatedly without terminating.

**Fix:** Track visited nodes in a `Set` and skip nodes already visited. This is shown in the `findAffectedFiles` example above where `affected.add(current)` returns `false` for already-visited nodes, preventing re-queuing.

---

## Sources & References

- Dart `dart:io` FileSystemEntity API documentation: https://api.dart.dev/stable/dart-io/FileSystemEntity-class.html
- `package:build` Builder interface and BuildStep API: https://pub.dev/packages/build
- `package:build_runner` usage guide and CLI reference: https://pub.dev/packages/build_runner
- `package:source_gen` annotation-driven code generation: https://pub.dev/packages/source_gen
- `package:crypto` SHA-256 and other hash algorithms for Dart: https://pub.dev/packages/crypto
- Dart build system design document and architecture overview: https://github.com/dart-lang/build/blob/master/docs/writing_a_builder.md
- `package:watcher` for cross-platform file system watching: https://pub.dev/packages/watcher
