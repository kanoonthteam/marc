---
name: python-data-processing
description: Python data processing pipelines — file I/O with pathlib, subprocess management, tempfile handling, binary data manipulation with struct/memoryview, base64 encoding, CSV/JSON/pandas processing, streaming large files, magic byte detection
---

# Python Data Processing Pipelines

Production-quality data processing patterns for Python 3.11+ backend systems. Covers end-to-end file processing pipelines (read, transform, write), subprocess management with security hardening, temporary file handling, binary data manipulation using struct and memoryview, base64 encoding/decoding, CSV and JSON processing with pandas integration, file format detection via magic bytes, streaming large file processing with generators, and robust error handling with retries and partial failure recovery.

## Table of Contents

1. [File Processing Pipelines](#file-processing-pipelines)
2. [Subprocess Management](#subprocess-management)
3. [Subprocess Security](#subprocess-security)
4. [Temporary File Handling](#temporary-file-handling)
5. [Binary Data Manipulation](#binary-data-manipulation)
6. [Base64 Encoding and Decoding](#base64-encoding-and-decoding)
7. [Pathlib for File Operations](#pathlib-for-file-operations)
8. [CSV Processing](#csv-processing)
9. [JSON Processing](#json-processing)
10. [Pandas Integration](#pandas-integration)
11. [File Format Detection](#file-format-detection)
12. [Streaming Large File Processing](#streaming-large-file-processing)
13. [Context Managers for Resource Cleanup](#context-managers-for-resource-cleanup)
14. [Error Handling in File Pipelines](#error-handling-in-file-pipelines)
15. [Best Practices](#best-practices)
16. [Anti-Patterns](#anti-patterns)
17. [Sources & References](#sources--references)

---

## File Processing Pipelines

File processing pipelines follow the read-transform-write pattern. Each stage is isolated, testable, and composable. Use generators to keep memory usage constant regardless of file size.

### Pipeline Architecture

A well-designed pipeline separates concerns into discrete stages: ingestion (reading raw data), transformation (parsing, validating, enriching), and output (writing to the destination format). Each stage should accept an iterator and yield results, enabling lazy evaluation and bounded memory consumption.

### Core Pipeline Pattern

```python
from __future__ import annotations

import csv
import json
import logging
from collections.abc import Generator, Iterable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Protocol, TypeVar

logger = logging.getLogger(__name__)

T = TypeVar("T")
U = TypeVar("U")


# --- Domain types ---

@dataclass(frozen=True, slots=True)
class RawRecord:
    """A single row read from an input source."""
    line_number: int
    fields: dict[str, str]


@dataclass(frozen=True, slots=True)
class ProcessedRecord:
    """A validated and transformed record ready for output."""
    id: str
    name: str
    email: str
    score: float
    tags: list[str] = field(default_factory=list)


@dataclass(frozen=True, slots=True)
class PipelineResult:
    """Summary of a pipeline run."""
    total_read: int
    total_written: int
    total_errors: int
    error_details: list[str] = field(default_factory=list)


# --- Stage protocol ---

class TransformStage(Protocol[T, U]):
    """Protocol for a pipeline transformation stage."""
    def __call__(self, records: Iterable[T]) -> Generator[U, None, None]: ...


# --- Read stage ---

def read_csv_records(path: Path) -> Generator[RawRecord, None, None]:
    """Read CSV rows lazily, yielding RawRecord instances."""
    with path.open("r", encoding="utf-8", newline="") as fh:
        reader = csv.DictReader(fh)
        for line_number, row in enumerate(reader, start=2):  # header is line 1
            yield RawRecord(line_number=line_number, fields=dict(row))


# --- Transform stage ---

def validate_and_transform(
    records: Iterable[RawRecord],
) -> Generator[ProcessedRecord | None, None, None]:
    """Validate raw records and transform into ProcessedRecord.

    Yields None for records that fail validation so the pipeline
    can track error counts without stopping.
    """
    for record in records:
        try:
            fields = record.fields
            score = float(fields.get("score", "0"))
            if score < 0 or score > 100:
                raise ValueError(f"Score {score} out of range [0, 100]")
            tags_raw = fields.get("tags", "")
            tags = [t.strip() for t in tags_raw.split(";") if t.strip()]
            yield ProcessedRecord(
                id=fields["id"],
                name=fields["name"].strip(),
                email=fields["email"].strip().lower(),
                score=score,
                tags=tags,
            )
        except (KeyError, ValueError) as exc:
            logger.warning("Skipping line %d: %s", record.line_number, exc)
            yield None


# --- Write stage ---

def write_json_output(
    records: Iterable[ProcessedRecord | None],
    output_path: Path,
) -> PipelineResult:
    """Write valid records to a JSON-lines file, collecting stats."""
    total_read = 0
    total_written = 0
    errors: list[str] = []

    with output_path.open("w", encoding="utf-8") as fh:
        for record in records:
            total_read += 1
            if record is None:
                errors.append(f"Record #{total_read} failed validation")
                continue
            json_line = json.dumps(
                {
                    "id": record.id,
                    "name": record.name,
                    "email": record.email,
                    "score": record.score,
                    "tags": record.tags,
                },
                ensure_ascii=False,
            )
            fh.write(json_line + "\n")
            total_written += 1

    return PipelineResult(
        total_read=total_read,
        total_written=total_written,
        total_errors=len(errors),
        error_details=errors,
    )


# --- Pipeline orchestrator ---

def run_pipeline(input_csv: Path, output_jsonl: Path) -> PipelineResult:
    """Execute the full read -> transform -> write pipeline."""
    raw_records = read_csv_records(input_csv)
    transformed = validate_and_transform(raw_records)
    result = write_json_output(transformed, output_jsonl)
    logger.info(
        "Pipeline complete: read=%d written=%d errors=%d",
        result.total_read,
        result.total_written,
        result.total_errors,
    )
    return result
```

Key principles in this pipeline:

- **Generators everywhere**: each stage yields items one at a time, so memory stays constant even for multi-gigabyte files.
- **Frozen dataclasses with slots**: immutable value objects with minimal memory footprint.
- **None sentinel for errors**: allows the write stage to count failures without breaking the iterator chain.
- **Separation of I/O and logic**: the transform stage has no file handles, making it trivially testable.

---

## Subprocess Management

The `subprocess` module is the standard way to run external programs. Always use `subprocess.run()` with explicit arguments rather than the legacy `os.system()` or `subprocess.Popen` unless you need streaming I/O.

### subprocess.run() with check, timeout, capture_output

```python
from __future__ import annotations

import logging
import subprocess
from dataclasses import dataclass
from pathlib import Path

logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class CommandResult:
    """Structured result from running an external command."""
    return_code: int
    stdout: str
    stderr: str
    command: list[str]


def run_command(
    args: list[str],
    *,
    cwd: Path | None = None,
    timeout_seconds: int = 30,
    env: dict[str, str] | None = None,
    input_data: str | None = None,
) -> CommandResult:
    """Run an external command safely and return structured output.

    Args:
        args: Command and arguments as a list (never a shell string).
        cwd: Working directory for the command.
        timeout_seconds: Maximum seconds before killing the process.
        env: Optional environment variable overrides.
        input_data: Optional string piped to stdin.

    Returns:
        CommandResult with captured stdout, stderr, and return code.

    Raises:
        subprocess.CalledProcessError: If the command exits non-zero.
        subprocess.TimeoutExpired: If the command exceeds the timeout.
    """
    logger.debug("Running command: %s (cwd=%s, timeout=%ds)", args, cwd, timeout_seconds)

    completed = subprocess.run(
        args,
        capture_output=True,
        text=True,
        check=True,
        timeout=timeout_seconds,
        cwd=cwd,
        env=env,
        input=input_data,
    )

    result = CommandResult(
        return_code=completed.returncode,
        stdout=completed.stdout,
        stderr=completed.stderr,
        command=args,
    )
    logger.debug("Command succeeded: rc=%d, stdout_len=%d", result.return_code, len(result.stdout))
    return result


def run_command_tolerant(
    args: list[str],
    *,
    cwd: Path | None = None,
    timeout_seconds: int = 30,
) -> CommandResult:
    """Run an external command, returning the result even on non-zero exit."""
    try:
        return run_command(args, cwd=cwd, timeout_seconds=timeout_seconds)
    except subprocess.CalledProcessError as exc:
        logger.warning("Command failed (rc=%d): %s", exc.returncode, args)
        return CommandResult(
            return_code=exc.returncode,
            stdout=exc.stdout or "",
            stderr=exc.stderr or "",
            command=args,
        )
    except subprocess.TimeoutExpired as exc:
        logger.error("Command timed out after %ds: %s", timeout_seconds, args)
        return CommandResult(
            return_code=-1,
            stdout=exc.stdout.decode("utf-8", errors="replace") if exc.stdout else "",
            stderr=exc.stderr.decode("utf-8", errors="replace") if exc.stderr else "",
            command=args,
        )


# --- Usage examples ---

def convert_image(input_path: Path, output_path: Path, quality: int = 85) -> CommandResult:
    """Convert an image using ImageMagick."""
    return run_command(
        ["magick", "convert", "-quality", str(quality), str(input_path), str(output_path)],
        timeout_seconds=60,
    )


def get_ffprobe_metadata(video_path: Path) -> CommandResult:
    """Extract video metadata using ffprobe."""
    return run_command(
        [
            "ffprobe",
            "-v", "quiet",
            "-print_format", "json",
            "-show_format",
            "-show_streams",
            str(video_path),
        ],
        timeout_seconds=15,
    )
```

### Important subprocess.run() parameters

| Parameter | Purpose | Recommendation |
|---|---|---|
| `check=True` | Raises `CalledProcessError` on non-zero exit | Always use unless you handle the return code yourself |
| `timeout` | Kills the process after N seconds | Always set to avoid hanging processes |
| `capture_output=True` | Captures stdout and stderr | Use when you need the output; equivalent to `stdout=PIPE, stderr=PIPE` |
| `text=True` | Decodes stdout/stderr as strings | Use for text output; omit for binary |
| `shell=False` | Default; runs command directly | Never set to True in production code |
| `cwd` | Set working directory | Use instead of chaining `cd` commands |
| `env` | Override environment variables | Pass a complete dict; does not merge with current env unless you build it yourself |
| `input` | Feed data to stdin | Use instead of `Popen` + `communicate()` for simple cases |

---

## Subprocess Security

Shell injection is one of the most common and dangerous vulnerabilities in backend systems that invoke external processes. Python's `subprocess` module is safe by default when used correctly.

### Rules

1. **Never use `shell=True`** unless you fully control the command string and understand the implications. When `shell=True`, the entire command string is parsed by `/bin/sh`, meaning metacharacters like `;`, `|`, `&&`, `$()`, and backticks are interpreted.

2. **Always pass arguments as a list**, not a single string. Each element in the list becomes exactly one argument to the target program with no shell interpretation.

3. **Sanitize all external input** before including it in command arguments. Validate that filenames do not contain path traversal components. Restrict characters to known-safe sets when possible.

4. **Use `shlex.quote()` only as a last resort** when you must construct a shell command string (e.g., for SSH remote commands). Prefer list-based arguments.

5. **Limit the `env` dict** to only the variables the subprocess needs. Do not pass the full parent environment with secrets appended.

### Argument Sanitization

```python
from __future__ import annotations

import re
import shlex
from pathlib import Path, PurePosixPath


class UnsafeInputError(Exception):
    """Raised when user input fails sanitization."""


def sanitize_filename(raw: str) -> str:
    """Sanitize a user-provided filename.

    Allows alphanumerics, hyphens, underscores, and dots.
    Strips path separators and null bytes.

    Raises:
        UnsafeInputError: If the filename is empty or starts with a dot.
    """
    cleaned = re.sub(r"[^\w.\-]", "_", raw)
    cleaned = cleaned.strip(".")
    if not cleaned:
        raise UnsafeInputError(f"Filename sanitization produced empty string from: {raw!r}")
    if cleaned.startswith("."):
        raise UnsafeInputError(f"Hidden files not allowed: {cleaned!r}")
    return cleaned


def validate_path_no_traversal(user_path: str, allowed_root: Path) -> Path:
    """Resolve a user-provided path and ensure it stays within allowed_root.

    Args:
        user_path: The untrusted path string from user input.
        allowed_root: The directory the resolved path must reside under.

    Returns:
        The resolved absolute Path.

    Raises:
        UnsafeInputError: If the path escapes allowed_root.
    """
    resolved = (allowed_root / user_path).resolve()
    if not resolved.is_relative_to(allowed_root.resolve()):
        raise UnsafeInputError(
            f"Path traversal detected: {user_path!r} resolves outside {allowed_root}"
        )
    return resolved


def build_safe_ssh_command(host: str, remote_cmd: list[str]) -> list[str]:
    """Build an SSH command list with properly quoted remote arguments.

    When executing commands over SSH, the remote side interprets the
    command through a shell. Use shlex.quote() on each argument to
    prevent injection.
    """
    quoted_remote = " ".join(shlex.quote(arg) for arg in remote_cmd)
    return ["ssh", "-o", "BatchMode=yes", host, quoted_remote]
```

### Common shell injection vectors to defend against

- **Path traversal**: `../../etc/passwd` in filenames
- **Command injection**: `; rm -rf /` or `$(malicious)` in arguments
- **Null bytes**: `file.txt\x00.jpg` to trick extension checks
- **Newlines**: `file\nmalicious` to break line-oriented protocols
- **Glob expansion**: `*` or `?` in filenames when shell=True

---

## Temporary File Handling

The `tempfile` module provides secure creation of temporary files and directories. Always use context managers to guarantee cleanup.

### TemporaryDirectory

Use `tempfile.TemporaryDirectory` when you need a scratch workspace for multi-step processing. The directory and all contents are deleted when the context manager exits.

```python
from __future__ import annotations

import json
import shutil
import tempfile
from pathlib import Path
from typing import Any


def process_upload_with_tempdir(
    raw_bytes: bytes,
    original_filename: str,
) -> dict[str, Any]:
    """Process an uploaded file in an isolated temporary directory.

    The temporary directory is automatically cleaned up, even if
    processing raises an exception.
    """
    with tempfile.TemporaryDirectory(prefix="upload_") as tmpdir:
        tmp_path = Path(tmpdir)

        # Write the upload to a temp file
        input_file = tmp_path / sanitize_filename(original_filename)
        input_file.write_bytes(raw_bytes)

        # Perform processing stages in the temp directory
        intermediate_file = tmp_path / "intermediate.json"
        output_file = tmp_path / "result.json"

        # Stage 1: extract metadata
        metadata = _extract_metadata(input_file)
        intermediate_file.write_text(
            json.dumps(metadata, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )

        # Stage 2: transform
        result = _transform(metadata)
        output_file.write_text(
            json.dumps(result, ensure_ascii=False),
            encoding="utf-8",
        )

        # Read the final result before the tmpdir is cleaned up
        return json.loads(output_file.read_text(encoding="utf-8"))


def _extract_metadata(path: Path) -> dict[str, Any]:
    """Placeholder metadata extraction."""
    stat = path.stat()
    return {
        "filename": path.name,
        "size_bytes": stat.st_size,
        "suffix": path.suffix,
    }


def _transform(metadata: dict[str, Any]) -> dict[str, Any]:
    """Placeholder transformation."""
    return {**metadata, "processed": True}
```

### NamedTemporaryFile

Use `NamedTemporaryFile` when you need a temporary file that external tools can access by path. Set `delete=False` on Windows or when passing the path to a subprocess, then manually clean up.

```python
import subprocess
import tempfile
from pathlib import Path


def convert_with_external_tool(
    input_data: bytes,
    output_format: str = "png",
) -> bytes:
    """Write data to a named temp file, run an external converter, read result."""
    with tempfile.NamedTemporaryFile(
        suffix=".svg",
        delete=True,
        mode="wb",
    ) as src_file:
        src_file.write(input_data)
        src_file.flush()  # Ensure data is on disk before subprocess reads it

        src_path = Path(src_file.name)
        dst_path = src_path.with_suffix(f".{output_format}")

        try:
            subprocess.run(
                ["rsvg-convert", "-f", output_format, "-o", str(dst_path), str(src_path)],
                check=True,
                timeout=30,
                capture_output=True,
            )
            return dst_path.read_bytes()
        finally:
            dst_path.unlink(missing_ok=True)
```

### mkdtemp for Long-Lived Directories

Use `tempfile.mkdtemp()` when you need a temporary directory whose lifetime extends beyond a single function. You are responsible for cleanup.

```python
import atexit
import tempfile
from pathlib import Path


def create_session_workspace() -> Path:
    """Create a temp directory for the session, registered for cleanup at exit."""
    workspace = Path(tempfile.mkdtemp(prefix="session_"))
    atexit.register(shutil.rmtree, workspace, ignore_errors=True)
    return workspace
```

---

## Binary Data Manipulation

Python provides `bytes`, `bytearray`, `struct`, and `memoryview` for working with binary data at the byte level. These are essential for parsing binary file formats, network protocols, and hardware interfaces.

### bytes vs bytearray

- `bytes` is immutable. Use for data that should not change after creation (file contents, network payloads, hash inputs).
- `bytearray` is mutable. Use when you need to modify binary data in place (building packets, buffered I/O).

### struct module

The `struct` module packs and unpacks binary data according to format strings. It is the standard way to read/write C-style binary structs.

```python
from __future__ import annotations

import struct
from dataclasses import dataclass
from pathlib import Path
from typing import BinaryIO


# --- BMP file header parsing ---

BMP_HEADER_FORMAT = "<2sIHHI"  # signature(2), filesize(4), reserved(2+2), offset(4)
BMP_HEADER_SIZE = struct.calcsize(BMP_HEADER_FORMAT)

BMP_INFO_HEADER_FORMAT = "<IiiHHIIiiII"  # DIB header (BITMAPINFOHEADER)
BMP_INFO_HEADER_SIZE = struct.calcsize(BMP_INFO_HEADER_FORMAT)


@dataclass(frozen=True, slots=True)
class BmpHeader:
    """Parsed BMP file header."""
    signature: bytes
    file_size: int
    reserved1: int
    reserved2: int
    pixel_offset: int
    width: int
    height: int
    bits_per_pixel: int
    compression: int
    image_size: int


def parse_bmp_header(fp: BinaryIO) -> BmpHeader:
    """Parse BMP file and DIB headers from a binary file object.

    Raises:
        ValueError: If the file does not start with the BMP signature.
        struct.error: If there is not enough data to unpack.
    """
    raw_header = fp.read(BMP_HEADER_SIZE)
    signature, file_size, reserved1, reserved2, pixel_offset = struct.unpack(
        BMP_HEADER_FORMAT, raw_header
    )
    if signature != b"BM":
        raise ValueError(f"Not a BMP file: signature={signature!r}")

    raw_info = fp.read(BMP_INFO_HEADER_SIZE)
    (
        _header_size, width, height, _planes, bits_per_pixel,
        compression, image_size, _x_ppm, _y_ppm, _colors_used, _colors_important,
    ) = struct.unpack(BMP_INFO_HEADER_FORMAT, raw_info)

    return BmpHeader(
        signature=signature,
        file_size=file_size,
        reserved1=reserved1,
        reserved2=reserved2,
        pixel_offset=pixel_offset,
        width=width,
        height=height,
        bits_per_pixel=bits_per_pixel,
        compression=compression,
        image_size=image_size,
    )


# --- Custom binary protocol ---

PACKET_HEADER_FORMAT = "!BBH I"  # version(1), type(1), length(2), sequence(4)
PACKET_HEADER_SIZE = struct.calcsize(PACKET_HEADER_FORMAT)


@dataclass(frozen=True, slots=True)
class Packet:
    """A binary protocol packet."""
    version: int
    packet_type: int
    payload_length: int
    sequence: int
    payload: bytes


def encode_packet(packet: Packet) -> bytes:
    """Encode a Packet into wire format."""
    header = struct.pack(
        PACKET_HEADER_FORMAT,
        packet.version,
        packet.packet_type,
        packet.payload_length,
        packet.sequence,
    )
    return header + packet.payload


def decode_packet(data: bytes) -> Packet:
    """Decode a Packet from wire format."""
    if len(data) < PACKET_HEADER_SIZE:
        raise ValueError(f"Data too short: {len(data)} < {PACKET_HEADER_SIZE}")
    version, packet_type, payload_length, sequence = struct.unpack(
        PACKET_HEADER_FORMAT, data[:PACKET_HEADER_SIZE]
    )
    payload = data[PACKET_HEADER_SIZE:PACKET_HEADER_SIZE + payload_length]
    if len(payload) != payload_length:
        raise ValueError(
            f"Payload truncated: expected {payload_length}, got {len(payload)}"
        )
    return Packet(
        version=version,
        packet_type=packet_type,
        payload_length=payload_length,
        sequence=sequence,
        payload=payload,
    )
```

### memoryview for Zero-Copy Slicing

`memoryview` allows you to slice binary data without copying. This is critical when processing large buffers.

```python
def extract_frames_zero_copy(buffer: bytes, frame_size: int) -> list[bytes]:
    """Extract fixed-size frames from a buffer without intermediate copies.

    memoryview slicing does not copy data. Only the final bytes() call
    creates a copy for each frame, minimizing memory allocations.
    """
    view = memoryview(buffer)
    frames: list[bytes] = []
    offset = 0
    while offset + frame_size <= len(view):
        frame = bytes(view[offset:offset + frame_size])
        frames.append(frame)
        offset += frame_size
    return frames


def patch_bytes_in_place(data: bytearray, offset: int, patch: bytes) -> None:
    """Overwrite bytes in a mutable buffer using memoryview for zero-copy write."""
    view = memoryview(data)
    view[offset:offset + len(patch)] = patch
```

---

## Base64 Encoding and Decoding

Base64 encodes binary data as ASCII text, used for embedding binary content in JSON payloads, data URIs, email attachments, and API responses.

### Standard base64 operations

```python
from __future__ import annotations

import base64
import hashlib
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True, slots=True)
class EncodedBlob:
    """A base64-encoded binary blob with metadata."""
    data_b64: str
    mime_type: str
    size_bytes: int
    sha256: str


def encode_file_to_base64(path: Path, mime_type: str = "application/octet-stream") -> EncodedBlob:
    """Read a file and encode it as base64 with integrity metadata."""
    raw = path.read_bytes()
    return EncodedBlob(
        data_b64=base64.b64encode(raw).decode("ascii"),
        mime_type=mime_type,
        size_bytes=len(raw),
        sha256=hashlib.sha256(raw).hexdigest(),
    )


def decode_base64_to_file(blob: EncodedBlob, output_path: Path) -> Path:
    """Decode a base64 blob back to a file, verifying integrity."""
    raw = base64.b64decode(blob.data_b64)
    actual_hash = hashlib.sha256(raw).hexdigest()
    if actual_hash != blob.sha256:
        raise ValueError(
            f"Integrity check failed: expected {blob.sha256}, got {actual_hash}"
        )
    if len(raw) != blob.size_bytes:
        raise ValueError(
            f"Size mismatch: expected {blob.size_bytes}, got {len(raw)}"
        )
    output_path.write_bytes(raw)
    return output_path


def build_data_uri(raw: bytes, mime_type: str) -> str:
    """Build an RFC 2397 data URI from raw bytes."""
    encoded = base64.b64encode(raw).decode("ascii")
    return f"data:{mime_type};base64,{encoded}"


def decode_data_uri(uri: str) -> tuple[bytes, str]:
    """Parse a data URI and return (raw_bytes, mime_type)."""
    if not uri.startswith("data:"):
        raise ValueError("Not a data URI")
    header, encoded = uri.split(",", maxsplit=1)
    # header format: data:[<mime>][;base64]
    mime_part = header.removeprefix("data:").removesuffix(";base64")
    mime_type = mime_part or "application/octet-stream"
    return base64.b64decode(encoded), mime_type
```

### Base64 URL-safe variant

Use `base64.urlsafe_b64encode` and `base64.urlsafe_b64decode` when the encoded string appears in URLs, filenames, or other contexts where `+` and `/` are problematic. This variant replaces `+` with `-` and `/` with `_`.

```python
def encode_url_safe(data: bytes) -> str:
    """Encode bytes to URL-safe base64 without padding."""
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def decode_url_safe(encoded: str) -> bytes:
    """Decode URL-safe base64 with or without padding."""
    padded = encoded + "=" * (-len(encoded) % 4)
    return base64.urlsafe_b64decode(padded)
```

---

## Pathlib for File Operations

`pathlib.Path` is the modern, object-oriented API for filesystem operations. Prefer it over `os.path` for all new code.

### Common patterns

```python
from __future__ import annotations

from pathlib import Path


def ensure_output_directory(base: Path, subdirectory: str) -> Path:
    """Create an output directory if it does not exist, returning the Path."""
    output = base / subdirectory
    output.mkdir(parents=True, exist_ok=True)
    return output


def list_files_by_extension(directory: Path, extension: str) -> list[Path]:
    """List all files with a given extension, sorted by name.

    Args:
        directory: The directory to search (non-recursive).
        extension: File extension including dot, e.g. ".csv".
    """
    return sorted(directory.glob(f"*{extension}"))


def list_files_recursive(directory: Path, pattern: str = "**/*") -> list[Path]:
    """Recursively list all files matching a glob pattern."""
    return sorted(p for p in directory.glob(pattern) if p.is_file())


def safe_write_text(path: Path, content: str, encoding: str = "utf-8") -> None:
    """Atomically write text to a file using a write-then-rename pattern.

    Writes to a sibling temp file first, then renames. This prevents
    partial writes from corrupting the target file on crash.
    """
    tmp_path = path.with_suffix(path.suffix + ".tmp")
    try:
        tmp_path.write_text(content, encoding=encoding)
        tmp_path.replace(path)  # Atomic on POSIX
    except BaseException:
        tmp_path.unlink(missing_ok=True)
        raise


def compute_relative_path(file_path: Path, root: Path) -> str:
    """Compute a POSIX-style relative path from root to file_path."""
    return file_path.relative_to(root).as_posix()


def get_file_info(path: Path) -> dict[str, object]:
    """Gather basic file metadata."""
    stat = path.stat()
    return {
        "name": path.name,
        "stem": path.stem,
        "suffix": path.suffix,
        "size_bytes": stat.st_size,
        "is_file": path.is_file(),
        "is_dir": path.is_dir(),
        "parent": str(path.parent),
        "absolute": str(path.resolve()),
    }
```

### pathlib vs os.path cheat sheet

| Task | os.path | pathlib |
|---|---|---|
| Join paths | `os.path.join(a, b)` | `Path(a) / b` |
| Get extension | `os.path.splitext(p)[1]` | `Path(p).suffix` |
| Get filename | `os.path.basename(p)` | `Path(p).name` |
| Check existence | `os.path.exists(p)` | `Path(p).exists()` |
| Read text | `open(p).read()` | `Path(p).read_text()` |
| Resolve symlinks | `os.path.realpath(p)` | `Path(p).resolve()` |
| Create directory | `os.makedirs(p, exist_ok=True)` | `Path(p).mkdir(parents=True, exist_ok=True)` |
| Iterate files | `os.listdir(p)` | `Path(p).iterdir()` |
| Glob match | `glob.glob(pattern)` | `Path(p).glob(pattern)` |

---

## CSV Processing

The `csv` module in the standard library handles CSV parsing with proper quoting, escaping, and dialect detection. Use it for correctness; use pandas for analysis.

### Reading and writing CSV

```python
from __future__ import annotations

import csv
import io
from collections.abc import Generator
from pathlib import Path
from typing import Any


def read_csv_as_dicts(
    path: Path,
    encoding: str = "utf-8",
    delimiter: str = ",",
) -> list[dict[str, str]]:
    """Read an entire CSV file into a list of dictionaries."""
    with path.open("r", encoding=encoding, newline="") as fh:
        reader = csv.DictReader(fh, delimiter=delimiter)
        return list(reader)


def stream_csv_rows(
    path: Path,
    encoding: str = "utf-8",
) -> Generator[dict[str, str], None, None]:
    """Stream CSV rows as dictionaries without loading the entire file."""
    with path.open("r", encoding=encoding, newline="") as fh:
        reader = csv.DictReader(fh)
        yield from reader


def write_csv(
    path: Path,
    rows: list[dict[str, Any]],
    fieldnames: list[str] | None = None,
    encoding: str = "utf-8",
) -> int:
    """Write a list of dictionaries to a CSV file.

    Returns:
        Number of rows written.
    """
    if not rows:
        return 0
    fields = fieldnames or list(rows[0].keys())
    with path.open("w", encoding=encoding, newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)
    return len(rows)


def detect_csv_dialect(sample: str) -> csv.Dialect:
    """Detect the CSV dialect (delimiter, quoting) from a sample string."""
    sniffer = csv.Sniffer()
    return sniffer.sniff(sample)


def csv_to_string(rows: list[dict[str, Any]], fieldnames: list[str]) -> str:
    """Serialize rows to an in-memory CSV string."""
    buffer = io.StringIO()
    writer = csv.DictWriter(buffer, fieldnames=fieldnames)
    writer.writeheader()
    writer.writerows(rows)
    return buffer.getvalue()
```

---

## JSON Processing

Python's `json` module handles serialization and deserialization. For large files, use streaming with `ijson` or line-delimited JSON (JSONL).

### Standard JSON operations

```python
from __future__ import annotations

import json
from collections.abc import Generator
from datetime import datetime, date
from decimal import Decimal
from pathlib import Path
from typing import Any


class ExtendedEncoder(json.JSONEncoder):
    """JSON encoder that handles common Python types not supported by default."""

    def default(self, obj: object) -> Any:
        if isinstance(obj, (datetime, date)):
            return obj.isoformat()
        if isinstance(obj, Decimal):
            return str(obj)
        if isinstance(obj, bytes):
            return obj.hex()
        if isinstance(obj, set):
            return sorted(obj)
        if isinstance(obj, Path):
            return str(obj)
        return super().default(obj)


def read_json(path: Path) -> Any:
    """Read and parse a JSON file."""
    return json.loads(path.read_text(encoding="utf-8"))


def write_json(path: Path, data: Any, *, pretty: bool = False) -> None:
    """Write data to a JSON file with the extended encoder."""
    indent = 2 if pretty else None
    text = json.dumps(data, cls=ExtendedEncoder, ensure_ascii=False, indent=indent)
    path.write_text(text + "\n", encoding="utf-8")


def read_jsonl(path: Path) -> Generator[dict[str, Any], None, None]:
    """Stream JSON Lines file, yielding one parsed object per line."""
    with path.open("r", encoding="utf-8") as fh:
        for line_number, line in enumerate(fh, start=1):
            stripped = line.strip()
            if not stripped:
                continue
            try:
                yield json.loads(stripped)
            except json.JSONDecodeError as exc:
                raise ValueError(
                    f"Invalid JSON at line {line_number}: {exc}"
                ) from exc


def write_jsonl(path: Path, records: list[dict[str, Any]]) -> int:
    """Write records to a JSON Lines file. Returns count written."""
    with path.open("w", encoding="utf-8") as fh:
        for record in records:
            fh.write(json.dumps(record, cls=ExtendedEncoder, ensure_ascii=False) + "\n")
    return len(records)
```

---

## Pandas Integration

Use pandas for structured data analysis, aggregation, and format conversion. The `csv` module is preferred for simple row-by-row processing; pandas excels at columnar operations.

```python
from __future__ import annotations

from pathlib import Path
from typing import Any

import pandas as pd


def csv_to_parquet(csv_path: Path, parquet_path: Path) -> int:
    """Convert a CSV file to Parquet format, returning row count."""
    df = pd.read_csv(csv_path, encoding="utf-8")
    df.to_parquet(parquet_path, engine="pyarrow", index=False)
    return len(df)


def aggregate_csv(
    path: Path,
    group_by: str,
    agg_column: str,
    agg_func: str = "sum",
) -> pd.DataFrame:
    """Read a CSV and return an aggregated DataFrame."""
    df = pd.read_csv(path, encoding="utf-8")
    return df.groupby(group_by)[agg_column].agg(agg_func).reset_index()


def read_large_csv_in_chunks(
    path: Path,
    chunk_size: int = 10_000,
    usecols: list[str] | None = None,
) -> pd.DataFrame:
    """Read a large CSV in chunks to limit peak memory."""
    chunks: list[pd.DataFrame] = []
    for chunk in pd.read_csv(
        path, encoding="utf-8", chunksize=chunk_size, usecols=usecols
    ):
        # Apply per-chunk filtering or transformation here
        chunks.append(chunk)
    return pd.concat(chunks, ignore_index=True)


def dataframe_to_json_records(df: pd.DataFrame) -> list[dict[str, Any]]:
    """Convert a DataFrame to a list of dictionaries (JSON-serializable)."""
    return df.to_dict(orient="records")
```

---

## File Format Detection

Detect file formats by reading magic bytes (file signatures) rather than relying on file extensions, which can be wrong or spoofed.

### Magic byte signatures

```python
from __future__ import annotations

import mimetypes
from dataclasses import dataclass
from pathlib import Path

# Common magic byte signatures (offset, signature, format name, MIME type)
MAGIC_SIGNATURES: list[tuple[int, bytes, str, str]] = [
    (0, b"\x89PNG\r\n\x1a\n", "PNG", "image/png"),
    (0, b"\xff\xd8\xff", "JPEG", "image/jpeg"),
    (0, b"GIF87a", "GIF87a", "image/gif"),
    (0, b"GIF89a", "GIF89a", "image/gif"),
    (0, b"PK\x03\x04", "ZIP", "application/zip"),
    (0, b"PK\x05\x06", "ZIP (empty)", "application/zip"),
    (0, b"%PDF-", "PDF", "application/pdf"),
    (0, b"\x7fELF", "ELF", "application/x-elf"),
    (0, b"BM", "BMP", "image/bmp"),
    (0, b"RIFF", "RIFF", "application/octet-stream"),  # Could be WAV, AVI, WebP
    (0, b"\x1f\x8b", "GZIP", "application/gzip"),
    (0, b"\xfd7zXZ\x00", "XZ", "application/x-xz"),
    (0, b"SQLite format 3\x00", "SQLite", "application/x-sqlite3"),
]


@dataclass(frozen=True, slots=True)
class FileTypeInfo:
    """Detected file type information."""
    format_name: str
    mime_type: str
    confidence: str  # "magic", "extension", "unknown"


def detect_file_type(path: Path) -> FileTypeInfo:
    """Detect file type using magic bytes, falling back to extension.

    Reads only the first 32 bytes of the file, making this safe for
    files of any size.
    """
    try:
        header = path.read_bytes()[:32]
    except (OSError, PermissionError):
        return FileTypeInfo("unknown", "application/octet-stream", "unknown")

    for offset, signature, format_name, mime_type in MAGIC_SIGNATURES:
        end = offset + len(signature)
        if len(header) >= end and header[offset:end] == signature:
            # Special handling for RIFF container (check sub-type)
            if format_name == "RIFF" and len(header) >= 12:
                sub_type = header[8:12]
                if sub_type == b"WEBP":
                    return FileTypeInfo("WebP", "image/webp", "magic")
                if sub_type == b"WAVE":
                    return FileTypeInfo("WAV", "audio/wav", "magic")
                if sub_type == b"AVI ":
                    return FileTypeInfo("AVI", "video/x-msvideo", "magic")
            return FileTypeInfo(format_name, mime_type, "magic")

    # Fall back to extension-based detection
    mime_guess, _ = mimetypes.guess_type(str(path))
    if mime_guess:
        return FileTypeInfo(path.suffix.lstrip(".").upper(), mime_guess, "extension")

    return FileTypeInfo("unknown", "application/octet-stream", "unknown")


def validate_upload_type(
    path: Path,
    allowed_mimes: set[str],
) -> FileTypeInfo:
    """Detect file type and raise if it is not in the allowed set."""
    info = detect_file_type(path)
    if info.mime_type not in allowed_mimes:
        raise ValueError(
            f"File type {info.format_name} ({info.mime_type}) not allowed. "
            f"Accepted: {sorted(allowed_mimes)}"
        )
    return info
```

---

## Streaming Large File Processing

When files are too large to fit in memory, use chunked reads and generator pipelines to process them with bounded memory.

### Chunked binary file reading

```python
from __future__ import annotations

import hashlib
from collections.abc import Generator
from pathlib import Path

DEFAULT_CHUNK_SIZE = 8 * 1024 * 1024  # 8 MiB


def stream_file_chunks(
    path: Path,
    chunk_size: int = DEFAULT_CHUNK_SIZE,
) -> Generator[bytes, None, None]:
    """Yield fixed-size chunks from a file.

    The last chunk may be smaller than chunk_size.
    """
    with path.open("rb") as fh:
        while True:
            chunk = fh.read(chunk_size)
            if not chunk:
                break
            yield chunk


def compute_streaming_hash(
    path: Path,
    algorithm: str = "sha256",
    chunk_size: int = DEFAULT_CHUNK_SIZE,
) -> str:
    """Compute a file hash without loading the entire file into memory."""
    hasher = hashlib.new(algorithm)
    for chunk in stream_file_chunks(path, chunk_size):
        hasher.update(chunk)
    return hasher.hexdigest()


def streaming_copy_with_transform(
    source: Path,
    destination: Path,
    transform: callable[[bytes], bytes] | None = None,
    chunk_size: int = DEFAULT_CHUNK_SIZE,
) -> int:
    """Copy a file in chunks, optionally transforming each chunk.

    Returns:
        Total bytes written.
    """
    total_written = 0
    with (
        source.open("rb") as src,
        destination.open("wb") as dst,
    ):
        while True:
            chunk = src.read(chunk_size)
            if not chunk:
                break
            if transform is not None:
                chunk = transform(chunk)
            dst.write(chunk)
            total_written += len(chunk)
    return total_written


def count_lines_streaming(path: Path, chunk_size: int = DEFAULT_CHUNK_SIZE) -> int:
    """Count lines in a file without loading it entirely into memory."""
    count = 0
    for chunk in stream_file_chunks(path, chunk_size):
        count += chunk.count(b"\n")
    return count
```

### Generator-based text line processing

```python
from __future__ import annotations

import re
from collections.abc import Generator
from pathlib import Path


def stream_lines(path: Path, encoding: str = "utf-8") -> Generator[str, None, None]:
    """Yield stripped lines from a text file."""
    with path.open("r", encoding=encoding) as fh:
        for line in fh:
            yield line.rstrip("\n")


def filter_lines(
    lines: Generator[str, None, None],
    pattern: str,
) -> Generator[str, None, None]:
    """Yield only lines matching a regex pattern."""
    compiled = re.compile(pattern)
    for line in lines:
        if compiled.search(line):
            yield line


def batch_lines(
    lines: Generator[str, None, None],
    batch_size: int = 1000,
) -> Generator[list[str], None, None]:
    """Collect lines into fixed-size batches for bulk operations."""
    batch: list[str] = []
    for line in lines:
        batch.append(line)
        if len(batch) >= batch_size:
            yield batch
            batch = []
    if batch:
        yield batch
```

---

## Context Managers for Resource Cleanup

Context managers ensure deterministic resource cleanup. Use them for file handles, database connections, temporary files, locks, and any resource that must be released.

### Custom context managers

```python
from __future__ import annotations

import logging
import time
from contextlib import contextmanager
from collections.abc import Generator
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)


@contextmanager
def timed_operation(label: str) -> Generator[dict[str, float], None, None]:
    """Context manager that measures elapsed wall-clock time.

    Usage:
        with timed_operation("data export") as timing:
            do_export()
        print(f"Took {timing['elapsed_seconds']:.2f}s")
    """
    result: dict[str, float] = {}
    start = time.perf_counter()
    try:
        yield result
    finally:
        result["elapsed_seconds"] = time.perf_counter() - start
        logger.info("%s completed in %.3fs", label, result["elapsed_seconds"])


@contextmanager
def atomic_file_write(
    target: Path,
    mode: str = "w",
    encoding: str = "utf-8",
) -> Generator[Any, None, None]:
    """Write to a temp file and atomically rename on success.

    If the body raises, the temp file is removed and the target
    is untouched.
    """
    tmp_path = target.with_suffix(target.suffix + ".tmp")
    try:
        with tmp_path.open(mode, encoding=encoding if "b" not in mode else None) as fh:
            yield fh
        tmp_path.replace(target)
    except BaseException:
        tmp_path.unlink(missing_ok=True)
        raise


@contextmanager
def managed_file_pair(
    input_path: Path,
    output_path: Path,
    input_mode: str = "rb",
    output_mode: str = "wb",
) -> Generator[tuple[Any, Any], None, None]:
    """Open an input and output file together, closing both on exit."""
    with (
        input_path.open(input_mode) as src,
        output_path.open(output_mode) as dst,
    ):
        yield src, dst
```

---

## Error Handling in File Pipelines

Production file pipelines must handle partial failures, transient errors, and corrupt data gracefully. Use structured error tracking, retries with exponential backoff, and dead-letter queues for unprocessable items.

### Retry decorator with exponential backoff

```python
from __future__ import annotations

import logging
import random
import time
from collections.abc import Callable
from functools import wraps
from typing import Any, ParamSpec, TypeVar

logger = logging.getLogger(__name__)

P = ParamSpec("P")
R = TypeVar("R")


def retry(
    max_attempts: int = 3,
    base_delay: float = 1.0,
    max_delay: float = 60.0,
    retryable_exceptions: tuple[type[Exception], ...] = (OSError, TimeoutError),
    jitter: bool = True,
) -> Callable[[Callable[P, R]], Callable[P, R]]:
    """Decorator that retries a function with exponential backoff.

    Args:
        max_attempts: Maximum number of attempts (including the first).
        base_delay: Initial delay in seconds between retries.
        max_delay: Maximum delay in seconds (caps exponential growth).
        retryable_exceptions: Tuple of exception types that trigger a retry.
        jitter: Add randomized jitter to prevent thundering herd.
    """
    def decorator(func: Callable[P, R]) -> Callable[P, R]:
        @wraps(func)
        def wrapper(*args: P.args, **kwargs: P.kwargs) -> R:
            last_exception: Exception | None = None
            for attempt in range(1, max_attempts + 1):
                try:
                    return func(*args, **kwargs)
                except retryable_exceptions as exc:
                    last_exception = exc
                    if attempt == max_attempts:
                        logger.error(
                            "Function %s failed after %d attempts: %s",
                            func.__name__, max_attempts, exc,
                        )
                        raise
                    delay = min(base_delay * (2 ** (attempt - 1)), max_delay)
                    if jitter:
                        delay *= 0.5 + random.random()
                    logger.warning(
                        "Attempt %d/%d for %s failed (%s), retrying in %.1fs",
                        attempt, max_attempts, func.__name__, exc, delay,
                    )
                    time.sleep(delay)
            raise last_exception  # type: ignore[misc]  # unreachable but satisfies mypy
        return wrapper
    return decorator
```

### Pipeline error tracking with dead-letter queue

```python
from __future__ import annotations

import json
import logging
from collections.abc import Generator, Iterable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, TypeVar

logger = logging.getLogger(__name__)

T = TypeVar("T")


@dataclass
class PipelineErrorTracker:
    """Track errors during pipeline processing with dead-letter output."""

    max_errors: int = 100
    errors: list[dict[str, Any]] = field(default_factory=list)
    total_processed: int = 0
    total_errors: int = 0

    def record_error(
        self,
        item: Any,
        error: Exception,
        stage: str,
    ) -> None:
        """Record a processing error."""
        self.total_errors += 1
        if len(self.errors) < self.max_errors:
            self.errors.append({
                "stage": stage,
                "error_type": type(error).__name__,
                "error_message": str(error),
                "item_repr": repr(item)[:500],
            })
        if self.total_errors >= self.max_errors:
            logger.error(
                "Error limit reached (%d). Pipeline may need to abort.",
                self.max_errors,
            )

    def record_success(self) -> None:
        """Record a successfully processed item."""
        self.total_processed += 1

    @property
    def error_rate(self) -> float:
        """Current error rate as a fraction."""
        total = self.total_processed + self.total_errors
        if total == 0:
            return 0.0
        return self.total_errors / total

    def write_dead_letter(self, path: Path) -> None:
        """Write failed items to a dead-letter JSON file."""
        path.write_text(
            json.dumps(
                {
                    "total_processed": self.total_processed,
                    "total_errors": self.total_errors,
                    "error_rate": round(self.error_rate, 4),
                    "errors": self.errors,
                },
                indent=2,
                ensure_ascii=False,
            ),
            encoding="utf-8",
        )


def resilient_pipeline(
    items: Iterable[T],
    process_fn: callable[[T], Any],
    tracker: PipelineErrorTracker,
    stage_name: str = "process",
    max_error_rate: float = 0.1,
) -> Generator[Any, None, None]:
    """Process items with error tracking, stopping if error rate exceeds threshold.

    Yields successfully processed items. Errors are recorded in the tracker
    rather than propagated, allowing the pipeline to continue.

    Args:
        items: Input iterable.
        process_fn: Function to apply to each item.
        tracker: Error tracker instance.
        stage_name: Label for error reporting.
        max_error_rate: Stop processing if error rate exceeds this (0.0-1.0).

    Yields:
        Successfully processed items.

    Raises:
        RuntimeError: If the error rate exceeds max_error_rate.
    """
    for item in items:
        try:
            result = process_fn(item)
            tracker.record_success()
            yield result
        except Exception as exc:
            tracker.record_error(item, exc, stage_name)
            if tracker.error_rate > max_error_rate and tracker.total_errors >= 10:
                raise RuntimeError(
                    f"Error rate {tracker.error_rate:.1%} exceeds threshold "
                    f"{max_error_rate:.1%} after {tracker.total_errors} errors"
                ) from exc
```

### Checkpoint-based recovery

```python
from __future__ import annotations

import json
from pathlib import Path


@dataclass
class PipelineCheckpoint:
    """Tracks pipeline progress for resumable processing."""

    checkpoint_file: Path
    last_processed_index: int = 0
    metadata: dict[str, Any] = field(default_factory=dict)

    def load(self) -> None:
        """Load checkpoint from disk if it exists."""
        if self.checkpoint_file.exists():
            data = json.loads(self.checkpoint_file.read_text(encoding="utf-8"))
            self.last_processed_index = data.get("last_processed_index", 0)
            self.metadata = data.get("metadata", {})
            logger.info("Resumed from checkpoint: index=%d", self.last_processed_index)

    def save(self) -> None:
        """Persist checkpoint to disk atomically."""
        tmp = self.checkpoint_file.with_suffix(".tmp")
        tmp.write_text(
            json.dumps({
                "last_processed_index": self.last_processed_index,
                "metadata": self.metadata,
            }),
            encoding="utf-8",
        )
        tmp.replace(self.checkpoint_file)

    def advance(self, index: int, save_every: int = 100) -> None:
        """Advance the checkpoint, saving periodically."""
        self.last_processed_index = index
        if index % save_every == 0:
            self.save()
```

---

## Best Practices

### File I/O

1. **Always specify encoding explicitly.** Never rely on the platform default. Use `encoding="utf-8"` for text files.
2. **Use `newline=""` when opening CSV files.** The csv module handles line endings internally; without this, you get double newlines on Windows.
3. **Prefer `pathlib.Path` over `os.path`.** It is more readable, composable, and catches errors at the type level.
4. **Use atomic writes for critical files.** Write to a temp file then rename. This prevents corruption from partial writes during crashes.
5. **Close files deterministically with context managers.** Do not rely on garbage collection to close file handles.

### Subprocess Management

6. **Always pass `check=True`** unless you explicitly handle non-zero exit codes.
7. **Always set `timeout`** to prevent runaway child processes from blocking your application.
8. **Never use `shell=True`** in production code unless you have a specific, justified reason and have sanitized all inputs.
9. **Pass arguments as lists, not strings.** This is the single most important defense against command injection.
10. **Capture output with `capture_output=True`** and log stderr on failure for debugging.

### Binary Data

11. **Use `struct` for fixed-format binary protocols.** It is faster and less error-prone than manual byte slicing.
12. **Use `memoryview` when slicing large buffers.** It avoids O(n) copies for each slice.
13. **Prefer `bytes` over `bytearray`** unless you need in-place mutation.

### Streaming and Memory

14. **Use generators for pipelines.** Each stage should accept an iterable and yield results.
15. **Process large files in chunks.** Never call `.read()` without a size argument on files that might be large.
16. **Use `pandas.read_csv(chunksize=N)`** for large CSV analysis instead of loading the entire file.

### Error Handling

17. **Track errors, do not silently swallow them.** Use an error tracker with configurable thresholds.
18. **Implement checkpoint-based recovery** for long-running pipelines so you can resume after failure.
19. **Set a maximum error rate** and abort if the pipeline encounters too many failures.
20. **Write dead-letter files** for unprocessable items so they can be inspected and reprocessed.

### Security

21. **Validate file types by magic bytes, not extension.** Extensions can be spoofed.
22. **Sanitize user-provided filenames.** Strip path separators, null bytes, and shell metacharacters.
23. **Validate paths against a root directory** to prevent path traversal attacks.
24. **Limit subprocess environment variables** to only what the child process needs.

---

## Anti-Patterns

### 1. Shell=True with user input

```python
# WRONG: shell injection vulnerability
import subprocess
filename = user_input()
subprocess.run(f"convert {filename} output.png", shell=True)

# CORRECT: argument list, no shell
subprocess.run(["convert", filename, "output.png"], check=True, timeout=30)
```

### 2. Reading entire large files into memory

```python
# WRONG: loads the entire file into memory
data = Path("huge_file.csv").read_text()
lines = data.splitlines()

# CORRECT: stream line by line
with Path("huge_file.csv").open("r", encoding="utf-8") as fh:
    for line in fh:
        process(line)
```

### 3. Relying on file extensions for type detection

```python
# WRONG: extension can be spoofed
if path.suffix == ".jpg":
    process_image(path)

# CORRECT: check magic bytes
info = detect_file_type(path)
if info.mime_type == "image/jpeg":
    process_image(path)
```

### 4. Forgetting to flush before subprocess reads

```python
# WRONG: data may still be in Python's buffer
tmp = tempfile.NamedTemporaryFile(suffix=".txt", mode="w")
tmp.write(content)
# subprocess may read an incomplete file
subprocess.run(["tool", tmp.name], check=True)

# CORRECT: flush (or use context manager exit) before subprocess
tmp = tempfile.NamedTemporaryFile(suffix=".txt", mode="w", delete=False)
try:
    tmp.write(content)
    tmp.flush()
    subprocess.run(["tool", tmp.name], check=True, timeout=30)
finally:
    Path(tmp.name).unlink(missing_ok=True)
```

### 5. Catching broad exceptions silently

```python
# WRONG: hides bugs and makes debugging impossible
try:
    result = process(data)
except Exception:
    pass

# CORRECT: catch specific exceptions, log, and track
try:
    result = process(data)
except (ValueError, KeyError) as exc:
    logger.warning("Processing failed for %r: %s", data_id, exc)
    error_tracker.record_error(data, exc, stage="process")
```

### 6. Not setting encoding on open()

```python
# WRONG: uses platform default encoding (cp1252 on Windows!)
with open("data.csv") as f:
    content = f.read()

# CORRECT: explicit UTF-8
with open("data.csv", encoding="utf-8") as f:
    content = f.read()
```

### 7. Manual string concatenation for paths

```python
# WRONG: breaks on different OS path separators
path = base_dir + "/" + subdir + "/" + filename

# CORRECT: pathlib handles separators
path = Path(base_dir) / subdir / filename
```

### 8. Ignoring subprocess stderr

```python
# WRONG: no visibility into why the command failed
result = subprocess.run(["tool", "arg"], capture_output=True)
if result.returncode != 0:
    raise RuntimeError("Tool failed")

# CORRECT: include stderr in the error
result = subprocess.run(["tool", "arg"], capture_output=True, text=True)
if result.returncode != 0:
    raise RuntimeError(f"Tool failed (rc={result.returncode}): {result.stderr}")
```

### 9. Using os.system() or os.popen()

```python
# WRONG: deprecated, insecure, no error handling
import os
os.system(f"convert {input_file} {output_file}")

# CORRECT: subprocess with full safety
subprocess.run(
    ["convert", str(input_file), str(output_file)],
    check=True,
    timeout=60,
    capture_output=True,
)
```

### 10. Not using newline="" for CSV files

```python
# WRONG: may produce double newlines on Windows
with open("out.csv", "w") as f:
    writer = csv.writer(f)
    writer.writerows(data)

# CORRECT: newline="" lets csv module handle line endings
with open("out.csv", "w", newline="", encoding="utf-8") as f:
    writer = csv.writer(f)
    writer.writerows(data)
```

---

## Sources & References

- [Python subprocess documentation (3.11+)](https://docs.python.org/3/library/subprocess.html) -- Official reference for subprocess.run(), Popen, and security considerations.
- [Python tempfile documentation](https://docs.python.org/3/library/tempfile.html) -- NamedTemporaryFile, TemporaryDirectory, mkdtemp, and security of temporary file creation.
- [Python struct module documentation](https://docs.python.org/3/library/struct.html) -- Format strings, byte order, alignment, and packing/unpacking binary data.
- [Python pathlib documentation](https://docs.python.org/3/library/pathlib.html) -- Pure and concrete path objects for filesystem operations.
- [Python csv module documentation](https://docs.python.org/3/library/csv.html) -- CSV reading, writing, dialect detection, and the DictReader/DictWriter interfaces.
- [Python base64 module documentation](https://docs.python.org/3/library/base64.html) -- Standard, URL-safe, and binary-to-ASCII encoding schemes.
- [Real Python: Working with Files in Python](https://realpython.com/working-with-files-in-python/) -- Practical guide covering pathlib, reading/writing, and directory operations.
- [OWASP Command Injection Prevention](https://cheatsheetseries.owasp.org/cheatsheets/OS_Command_Injection_Defense_Cheat_Sheet.html) -- Security guidance for safely invoking external processes.
