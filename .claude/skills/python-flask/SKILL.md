---
name: python-flask
description: Flask web framework patterns — application factory, blueprints, middleware hooks, error handling, CORS, file uploads, streaming responses, SQLAlchemy integration, configuration management, extensions ecosystem, and performance tuning
---

# Flask Web Framework Patterns

Production-ready Flask patterns for Python 3.11+ backend services. Covers the application factory pattern, blueprint-based domain organization, middleware and request lifecycle hooks, custom error handling, CORS configuration, secure file uploads, streaming responses, SQLAlchemy integration, configuration management across environments, the Flask extensions ecosystem, and performance optimization with connection pooling and response compression.

## Table of Contents

1. [Application Factory Pattern](#application-factory-pattern)
2. [Blueprint Organization by Domain](#blueprint-organization-by-domain)
3. [Request/Response Lifecycle](#requestresponse-lifecycle)
4. [Middleware and Before/After Request Hooks](#middleware-and-beforeafter-request-hooks)
5. [Error Handling](#error-handling)
6. [CORS Configuration with flask-cors](#cors-configuration-with-flask-cors)
7. [File Upload Handling](#file-upload-handling)
8. [Streaming Responses for Large Files](#streaming-responses-for-large-files)
9. [Flask Configuration Management](#flask-configuration-management)
10. [SQLAlchemy Integration with Flask](#sqlalchemy-integration-with-flask)
11. [Flask Extensions Ecosystem](#flask-extensions-ecosystem)
12. [Performance: Connection Pooling and Response Compression](#performance-connection-pooling-and-response-compression)
13. [Best Practices](#best-practices)
14. [Anti-Patterns](#anti-patterns)
15. [Sources & References](#sources--references)

---

## Application Factory Pattern

The application factory is the canonical way to create Flask applications. It avoids module-level app instances, enables multiple configurations for testing and production, and prevents circular imports.

### Why Use the Factory Pattern

- Enables creating multiple app instances with different configs (testing, staging, production).
- Avoids circular imports caused by a module-level `app` object that other modules import.
- Makes the app fully configurable at creation time rather than at import time.
- Required for proper integration with Flask extensions that call `init_app()`.

### Full Factory Implementation

```python
# src/app/__init__.py
from __future__ import annotations

import logging
from typing import Any

from flask import Flask
from flask_cors import CORS
from flask_compress import Compress
from flask_migrate import Migrate
from flask_sqlalchemy import SQLAlchemy

db = SQLAlchemy()
migrate = Migrate()
compress = Compress()


def create_app(config_name: str = "development") -> Flask:
    """Application factory for creating the Flask app.

    Args:
        config_name: One of 'development', 'testing', 'staging', 'production'.

    Returns:
        A fully configured Flask application instance.
    """
    app = Flask(__name__, instance_relative_config=True)

    # Load configuration from object based on environment
    from app.config import config_by_name

    app.config.from_object(config_by_name[config_name])

    # Optionally override from instance/config.py (not in VCS)
    app.config.from_pyfile("config.py", silent=True)

    # Initialize extensions
    _register_extensions(app)

    # Register blueprints
    _register_blueprints(app)

    # Register error handlers
    _register_error_handlers(app)

    # Register shell context for flask shell
    _register_shell_context(app)

    # Register CLI commands
    _register_cli_commands(app)

    # Configure logging
    _configure_logging(app)

    return app


def _register_extensions(app: Flask) -> None:
    """Initialize Flask extensions with the app instance."""
    db.init_app(app)
    migrate.init_app(app, db)
    compress.init_app(app)

    # CORS — configure per-environment allowed origins
    CORS(
        app,
        origins=app.config.get("CORS_ORIGINS", ["http://localhost:3000"]),
        supports_credentials=True,
    )


def _register_blueprints(app: Flask) -> None:
    """Import and register all domain blueprints."""
    from app.domains.auth.routes import auth_bp
    from app.domains.users.routes import users_bp
    from app.domains.files.routes import files_bp
    from app.domains.health.routes import health_bp

    app.register_blueprint(health_bp, url_prefix="/api/health")
    app.register_blueprint(auth_bp, url_prefix="/api/auth")
    app.register_blueprint(users_bp, url_prefix="/api/users")
    app.register_blueprint(files_bp, url_prefix="/api/files")


def _register_error_handlers(app: Flask) -> None:
    """Register global error handlers."""
    from app.errors import register_error_handlers

    register_error_handlers(app)


def _register_shell_context(app: Flask) -> None:
    """Add objects to the flask shell context for debugging."""

    @app.shell_context_processor
    def make_shell_context() -> dict[str, Any]:
        return {"db": db, "app": app}


def _register_cli_commands(app: Flask) -> None:
    """Register custom CLI commands accessible via `flask <command>`."""
    from app.cli import register_commands

    register_commands(app)


def _configure_logging(app: Flask) -> None:
    """Set up structured logging based on configuration."""
    log_level = app.config.get("LOG_LEVEL", "INFO")
    logging.basicConfig(
        level=getattr(logging, log_level),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%Y-%m-%dT%H:%M:%S%z",
    )
    app.logger.setLevel(getattr(logging, log_level))
```

This factory function is the entry point for everything. The `create_app()` call happens in your WSGI entry point (`wsgi.py`), in your test fixtures, and in your CLI configuration.

---

## Blueprint Organization by Domain

Blueprints let you group related routes, templates, and static files into self-contained modules. The preferred approach is organization by domain (feature), not by layer (all routes in one folder, all models in another).

### Recommended Project Structure

```
src/
  app/
    __init__.py              # create_app factory
    config.py                # Configuration classes
    errors.py                # Global error handlers
    cli.py                   # Custom CLI commands
    extensions.py            # Extension instances (alternative to __init__)
    domains/
      auth/
        __init__.py
        routes.py            # Blueprint + route definitions
        services.py          # Business logic
        models.py            # SQLAlchemy models
        schemas.py           # Marshmallow / Pydantic schemas
        exceptions.py        # Domain-specific exceptions
      users/
        __init__.py
        routes.py
        services.py
        models.py
        schemas.py
      files/
        __init__.py
        routes.py
        services.py
        validators.py        # File validation logic
      health/
        __init__.py
        routes.py
    shared/
      decorators.py          # Shared decorators (auth_required, etc.)
      pagination.py          # Pagination helpers
      responses.py           # Standardized JSON response builders
tests/
  conftest.py                # Shared fixtures
  test_auth/
  test_users/
  test_files/
```

### Blueprint Definition

Each domain defines its own blueprint. Keep the blueprint object in the `routes.py` file and register routes against it:

```python
# src/app/domains/users/routes.py
from __future__ import annotations

from http import HTTPStatus
from typing import Any

from flask import Blueprint, jsonify, request, Response
from marshmallow import ValidationError

from app.domains.users.schemas import (
    UserCreateSchema,
    UserResponseSchema,
    UserUpdateSchema,
)
from app.domains.users.services import UserService
from app.shared.decorators import auth_required
from app.shared.pagination import paginate_query

users_bp = Blueprint("users", __name__)

user_create_schema = UserCreateSchema()
user_update_schema = UserUpdateSchema()
user_response_schema = UserResponseSchema()


@users_bp.route("/", methods=["GET"])
@auth_required
def list_users() -> tuple[Response, int]:
    """List users with cursor-based pagination."""
    page = request.args.get("page", 1, type=int)
    per_page = request.args.get("per_page", 20, type=int)
    per_page = min(per_page, 100)  # Cap at 100

    query = UserService.get_users_query()
    result = paginate_query(query, page=page, per_page=per_page)

    return jsonify({
        "data": user_response_schema.dump(result.items, many=True),
        "meta": {
            "page": result.page,
            "per_page": result.per_page,
            "total": result.total,
            "pages": result.pages,
        },
    }), HTTPStatus.OK


@users_bp.route("/", methods=["POST"])
@auth_required
def create_user() -> tuple[Response, int]:
    """Create a new user."""
    json_data = request.get_json(silent=True)
    if json_data is None:
        return jsonify({"error": "Request body must be JSON"}), HTTPStatus.BAD_REQUEST

    try:
        data: dict[str, Any] = user_create_schema.load(json_data)
    except ValidationError as err:
        return jsonify({"error": "Validation failed", "details": err.messages}), HTTPStatus.UNPROCESSABLE_ENTITY

    user = UserService.create_user(data)
    return jsonify({"data": user_response_schema.dump(user)}), HTTPStatus.CREATED


@users_bp.route("/<int:user_id>", methods=["GET"])
@auth_required
def get_user(user_id: int) -> tuple[Response, int]:
    """Retrieve a single user by ID."""
    user = UserService.get_user_or_404(user_id)
    return jsonify({"data": user_response_schema.dump(user)}), HTTPStatus.OK


@users_bp.route("/<int:user_id>", methods=["PATCH"])
@auth_required
def update_user(user_id: int) -> tuple[Response, int]:
    """Partially update a user."""
    json_data = request.get_json(silent=True)
    if json_data is None:
        return jsonify({"error": "Request body must be JSON"}), HTTPStatus.BAD_REQUEST

    try:
        data: dict[str, Any] = user_update_schema.load(json_data)
    except ValidationError as err:
        return jsonify({"error": "Validation failed", "details": err.messages}), HTTPStatus.UNPROCESSABLE_ENTITY

    user = UserService.update_user(user_id, data)
    return jsonify({"data": user_response_schema.dump(user)}), HTTPStatus.OK


@users_bp.route("/<int:user_id>", methods=["DELETE"])
@auth_required
def delete_user(user_id: int) -> tuple[Response, int]:
    """Soft-delete a user."""
    UserService.delete_user(user_id)
    return jsonify({"message": "User deleted"}), HTTPStatus.OK
```

### Blueprint-Scoped Hooks

Blueprints can register their own `before_request` and `after_request` hooks that only apply to routes in that blueprint:

```python
@users_bp.before_request
def log_users_request() -> None:
    """Log every request to the users domain."""
    app_logger = current_app.logger
    app_logger.info("Users request: %s %s", request.method, request.path)
```

---

## Request/Response Lifecycle

Understanding the Flask request/response lifecycle is critical for writing correct middleware and debugging unexpected behavior.

### Lifecycle Order

1. **WSGI server** receives HTTP request, creates `environ` dict.
2. **`Flask.__call__`** is invoked, creating the request context.
3. **`before_request`** hooks run (app-level first, then blueprint-level). If any returns a response, remaining hooks and the view function are skipped.
4. **URL routing** resolves the view function.
5. **View function** executes and returns a response (or raises an exception).
6. **`after_request`** hooks run on the response object (blueprint-level first, then app-level). These hooks receive the response and must return a response.
7. **`teardown_request`** hooks run regardless of whether an exception occurred. Used for cleanup (closing connections, releasing locks).
8. **Response** is sent back through the WSGI server.

### Context Locals

Flask uses context-local proxies to provide `request`, `g`, `current_app`, and `session` without explicit parameter passing:

- **`request`** — the current HTTP request object. Only available inside a request context.
- **`g`** — per-request namespace for storing data shared across hooks and views within a single request. Reset between requests.
- **`current_app`** — proxy to the active Flask application. Use this instead of importing the app directly.
- **`session`** — the signed cookie-based session dictionary.

### The `g` Object

The `g` object is the correct place to store per-request state like the current authenticated user, a request-scoped database connection, or a correlation ID:

```python
from flask import g, request

@app.before_request
def load_current_user() -> None:
    token = request.headers.get("Authorization", "").removeprefix("Bearer ")
    if token:
        g.current_user = AuthService.verify_token(token)
    else:
        g.current_user = None
```

---

## Middleware and Before/After Request Hooks

Flask does not use the term "middleware" in the same way as Express or Django. Instead, it provides hooks at the app and blueprint level.

### App-Level Hooks

```python
# src/app/middleware.py
from __future__ import annotations

import time
import uuid
from typing import TYPE_CHECKING

from flask import Flask, g, request, Response

if TYPE_CHECKING:
    from typing import Optional


def register_middleware(app: Flask) -> None:
    """Register app-level before/after request hooks."""

    @app.before_request
    def assign_request_id() -> None:
        """Assign a unique request ID for tracing and logging."""
        g.request_id = request.headers.get(
            "X-Request-ID", str(uuid.uuid4())
        )
        g.request_start_time = time.perf_counter()

    @app.before_request
    def reject_oversized_payloads() -> Optional[tuple[dict, int]]:
        """Reject requests whose Content-Length exceeds the configured max."""
        max_content_length = app.config.get("MAX_CONTENT_LENGTH")
        if max_content_length and request.content_length:
            if request.content_length > max_content_length:
                return {"error": "Payload too large"}, 413
        return None

    @app.after_request
    def add_security_headers(response: Response) -> Response:
        """Add security-related headers to every response."""
        response.headers["X-Request-ID"] = g.get("request_id", "unknown")
        response.headers["X-Content-Type-Options"] = "nosniff"
        response.headers["X-Frame-Options"] = "DENY"
        response.headers["Strict-Transport-Security"] = (
            "max-age=63072000; includeSubDomains; preload"
        )
        response.headers["Cache-Control"] = "no-store"
        return response

    @app.after_request
    def log_request_duration(response: Response) -> Response:
        """Log how long each request took to process."""
        start = g.get("request_start_time")
        if start is not None:
            duration_ms = (time.perf_counter() - start) * 1000
            app.logger.info(
                "method=%s path=%s status=%d duration_ms=%.2f request_id=%s",
                request.method,
                request.path,
                response.status_code,
                duration_ms,
                g.get("request_id", "unknown"),
            )
        return response

    @app.teardown_request
    def teardown_cleanup(exception: BaseException | None = None) -> None:
        """Cleanup resources at the end of every request."""
        if exception:
            app.logger.error(
                "Request teardown with exception: %s", exception
            )
```

### True WSGI Middleware

For cases where you need to intercept the request before Flask sees it (e.g., rewriting paths, adding proxy headers), you can write WSGI middleware:

```python
# src/app/wsgi_middleware.py
from __future__ import annotations

from typing import Any, Callable, Iterable


class ProxyFixMiddleware:
    """Trust X-Forwarded-* headers from a known reverse proxy.

    For production, prefer Werkzeug's built-in ProxyFix:
        from werkzeug.middleware.proxy_fix import ProxyFix
        app.wsgi_app = ProxyFix(app.wsgi_app, x_for=1, x_proto=1, x_host=1)

    This example illustrates the WSGI middleware interface.
    """

    def __init__(self, app: Callable[..., Iterable[bytes]]) -> None:
        self.app = app

    def __call__(
        self,
        environ: dict[str, Any],
        start_response: Callable[..., Any],
    ) -> Iterable[bytes]:
        # Trust the X-Forwarded-Proto header from the proxy
        if environ.get("HTTP_X_FORWARDED_PROTO") == "https":
            environ["wsgi.url_scheme"] = "https"
        return self.app(environ, start_response)
```

---

## Error Handling

### Custom Error Handlers

Register error handlers in a centralized module. Flask lets you register handlers for HTTP status codes and specific exception classes.

```python
# src/app/errors.py
from __future__ import annotations

import traceback
from http import HTTPStatus
from typing import Any

from flask import Flask, jsonify, Response, current_app
from werkzeug.exceptions import HTTPException


class APIError(Exception):
    """Base exception for all API-level errors.

    Subclass this for domain-specific errors that should be returned
    as structured JSON responses.
    """

    def __init__(
        self,
        message: str,
        status_code: int = HTTPStatus.INTERNAL_SERVER_ERROR,
        details: dict[str, Any] | None = None,
    ) -> None:
        super().__init__(message)
        self.message = message
        self.status_code = status_code
        self.details = details or {}


class NotFoundError(APIError):
    """Resource not found."""

    def __init__(self, resource: str, identifier: Any) -> None:
        super().__init__(
            message=f"{resource} with id '{identifier}' not found",
            status_code=HTTPStatus.NOT_FOUND,
        )


class ConflictError(APIError):
    """Resource already exists or state conflict."""

    def __init__(self, message: str = "Resource conflict") -> None:
        super().__init__(message=message, status_code=HTTPStatus.CONFLICT)


class ForbiddenError(APIError):
    """Insufficient permissions."""

    def __init__(self, message: str = "Forbidden") -> None:
        super().__init__(message=message, status_code=HTTPStatus.FORBIDDEN)


def register_error_handlers(app: Flask) -> None:
    """Register all error handlers on the Flask app."""

    @app.errorhandler(APIError)
    def handle_api_error(error: APIError) -> tuple[Response, int]:
        """Handle custom APIError exceptions."""
        response = {
            "error": error.message,
            "status": error.status_code,
        }
        if error.details:
            response["details"] = error.details
        return jsonify(response), error.status_code

    @app.errorhandler(HTTPException)
    def handle_http_exception(error: HTTPException) -> tuple[Response, int]:
        """Handle standard Werkzeug HTTP exceptions (404, 405, etc.)."""
        return jsonify({
            "error": error.description or error.name,
            "status": error.code,
        }), error.code or HTTPStatus.INTERNAL_SERVER_ERROR

    @app.errorhandler(422)
    def handle_unprocessable_entity(
        error: HTTPException,
    ) -> tuple[Response, int]:
        """Handle 422 Unprocessable Entity from validation failures."""
        return jsonify({
            "error": "Validation error",
            "status": HTTPStatus.UNPROCESSABLE_ENTITY,
            "details": getattr(error, "data", {}).get("messages", {}),
        }), HTTPStatus.UNPROCESSABLE_ENTITY

    @app.errorhandler(429)
    def handle_rate_limit(error: HTTPException) -> tuple[Response, int]:
        """Handle rate limit exceeded."""
        return jsonify({
            "error": "Rate limit exceeded. Try again later.",
            "status": HTTPStatus.TOO_MANY_REQUESTS,
        }), HTTPStatus.TOO_MANY_REQUESTS

    @app.errorhandler(Exception)
    def handle_unexpected_error(error: Exception) -> tuple[Response, int]:
        """Catch-all for unhandled exceptions. Log the full traceback."""
        current_app.logger.error(
            "Unhandled exception: %s\n%s",
            error,
            traceback.format_exc(),
        )
        return jsonify({
            "error": "An unexpected error occurred",
            "status": HTTPStatus.INTERNAL_SERVER_ERROR,
        }), HTTPStatus.INTERNAL_SERVER_ERROR
```

### Using abort() vs Raising Custom Exceptions

Flask's `abort()` raises a Werkzeug `HTTPException`. For simple cases it works, but custom exception classes are better for domain logic because they carry structured details:

```python
from flask import abort

# Simple abort — fine for generic cases
abort(404, description="User not found")

# Prefer custom exceptions for domain logic — they carry typed context
raise NotFoundError(resource="User", identifier=user_id)
raise ConflictError(message=f"Email '{email}' is already registered")
```

---

## CORS Configuration with flask-cors

Cross-Origin Resource Sharing must be configured explicitly for APIs consumed by browser-based frontends.

### Installation

```
pip install flask-cors
```

### Per-Environment CORS Setup

```python
# In create_app() or _register_extensions()
from flask_cors import CORS

def _register_extensions(app: Flask) -> None:
    CORS(
        app,
        origins=app.config["CORS_ORIGINS"],
        methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
        allow_headers=["Content-Type", "Authorization", "X-Request-ID"],
        expose_headers=["X-Request-ID", "X-RateLimit-Remaining"],
        supports_credentials=True,
        max_age=600,  # Preflight cache duration in seconds
    )
```

Configuration values per environment:

```python
class DevelopmentConfig(BaseConfig):
    CORS_ORIGINS: list[str] = ["http://localhost:3000", "http://localhost:5173"]

class ProductionConfig(BaseConfig):
    CORS_ORIGINS: list[str] = ["https://app.example.com", "https://admin.example.com"]
```

### Blueprint-Level CORS

You can also apply CORS to specific blueprints instead of globally:

```python
from flask_cors import CORS

# Only allow cross-origin to the public API blueprint
CORS(public_api_bp, origins=["https://partner.example.com"])
```

---

## File Upload Handling

Handling file uploads securely requires filename sanitization, MIME type validation, size limits, and writing to a safe destination.

### Secure Upload Implementation

```python
# src/app/domains/files/routes.py
from __future__ import annotations

import os
from http import HTTPStatus
from pathlib import Path
from typing import Final
from uuid import uuid4

from flask import (
    Blueprint,
    current_app,
    jsonify,
    request,
    Response,
)
from werkzeug.utils import secure_filename

from app.domains.files.validators import validate_upload
from app.errors import APIError

files_bp = Blueprint("files", __name__)

ALLOWED_EXTENSIONS: Final[frozenset[str]] = frozenset({
    "png", "jpg", "jpeg", "gif", "webp", "pdf", "csv", "xlsx",
})

ALLOWED_MIME_TYPES: Final[frozenset[str]] = frozenset({
    "image/png",
    "image/jpeg",
    "image/gif",
    "image/webp",
    "application/pdf",
    "text/csv",
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
})

MAX_FILE_SIZE: Final[int] = 10 * 1024 * 1024  # 10 MB


def _allowed_file(filename: str) -> bool:
    """Check if the file extension is in the allowed set."""
    return "." in filename and filename.rsplit(".", 1)[1].lower() in ALLOWED_EXTENSIONS


@files_bp.route("/upload", methods=["POST"])
def upload_file() -> tuple[Response, int]:
    """Handle single file upload with full validation.

    Validates:
    - File presence in request
    - Filename is not empty
    - Extension is in the allow-list
    - MIME type matches the allow-list
    - File size does not exceed the configured maximum
    """
    if "file" not in request.files:
        return jsonify({"error": "No file part in request"}), HTTPStatus.BAD_REQUEST

    file = request.files["file"]
    if file.filename is None or file.filename == "":
        return jsonify({"error": "No file selected"}), HTTPStatus.BAD_REQUEST

    # Extension check
    if not _allowed_file(file.filename):
        return jsonify({
            "error": f"File type not allowed. Allowed: {', '.join(sorted(ALLOWED_EXTENSIONS))}",
        }), HTTPStatus.BAD_REQUEST

    # MIME type check — do not trust Content-Type alone, but use it as a first filter
    if file.content_type not in ALLOWED_MIME_TYPES:
        return jsonify({
            "error": f"MIME type '{file.content_type}' is not allowed",
        }), HTTPStatus.BAD_REQUEST

    # Size check — read the stream to determine actual size
    file.seek(0, os.SEEK_END)
    file_size = file.tell()
    file.seek(0)  # Reset stream position

    if file_size > MAX_FILE_SIZE:
        max_mb = MAX_FILE_SIZE / (1024 * 1024)
        return jsonify({
            "error": f"File size exceeds maximum of {max_mb:.0f} MB",
        }), HTTPStatus.REQUEST_ENTITY_TOO_LARGE

    # Sanitize filename and generate a unique name to avoid collisions
    original_name = secure_filename(file.filename)
    ext = original_name.rsplit(".", 1)[1].lower()
    unique_name = f"{uuid4().hex}.{ext}"

    upload_dir = Path(current_app.config["UPLOAD_FOLDER"])
    upload_dir.mkdir(parents=True, exist_ok=True)
    dest = upload_dir / unique_name

    file.save(dest)

    current_app.logger.info(
        "File uploaded: original=%s saved=%s size=%d",
        original_name,
        unique_name,
        file_size,
    )

    return jsonify({
        "data": {
            "filename": unique_name,
            "original_name": original_name,
            "size": file_size,
            "mime_type": file.content_type,
        },
    }), HTTPStatus.CREATED


@files_bp.route("/upload/batch", methods=["POST"])
def upload_batch() -> tuple[Response, int]:
    """Handle multiple file uploads in a single request."""
    files = request.files.getlist("files")
    if not files:
        return jsonify({"error": "No files provided"}), HTTPStatus.BAD_REQUEST

    max_batch = current_app.config.get("MAX_BATCH_UPLOAD", 10)
    if len(files) > max_batch:
        return jsonify({
            "error": f"Maximum {max_batch} files per batch",
        }), HTTPStatus.BAD_REQUEST

    results: list[dict[str, str | int]] = []
    errors: list[dict[str, str]] = []

    for idx, file in enumerate(files):
        if file.filename is None or file.filename == "":
            errors.append({"index": str(idx), "error": "Empty filename"})
            continue

        if not _allowed_file(file.filename):
            errors.append({
                "index": str(idx),
                "error": f"File type not allowed: {file.filename}",
            })
            continue

        original_name = secure_filename(file.filename)
        ext = original_name.rsplit(".", 1)[1].lower()
        unique_name = f"{uuid4().hex}.{ext}"

        upload_dir = Path(current_app.config["UPLOAD_FOLDER"])
        upload_dir.mkdir(parents=True, exist_ok=True)
        file.save(upload_dir / unique_name)

        results.append({
            "filename": unique_name,
            "original_name": original_name,
        })

    status = HTTPStatus.CREATED if not errors else HTTPStatus.MULTI_STATUS
    return jsonify({"data": results, "errors": errors}), status
```

### Flask-Level Size Limit

In addition to the per-route validation, set `MAX_CONTENT_LENGTH` in the Flask configuration to have Werkzeug automatically reject oversized requests before your view function runs:

```python
class BaseConfig:
    MAX_CONTENT_LENGTH: int = 16 * 1024 * 1024  # 16 MB hard limit
```

---

## Streaming Responses for Large Files

For large files (database exports, log downloads, generated reports), stream the response to avoid loading the entire content into memory.

### Generator-Based Streaming

```python
# src/app/domains/files/routes.py (continued)
from flask import stream_with_context

@files_bp.route("/export/users.csv", methods=["GET"])
@auth_required
def export_users_csv() -> Response:
    """Stream a CSV export of all users.

    Uses a generator to avoid loading all rows into memory at once.
    """

    def generate_csv() -> Generator[str, None, None]:
        # Header row
        yield "id,email,name,created_at\n"

        # Stream rows in batches of 1000
        page = 1
        while True:
            users = UserService.get_users_batch(page=page, per_page=1000)
            if not users:
                break
            for user in users:
                yield (
                    f"{user.id},"
                    f"{user.email},"
                    f'"{user.full_name}",'
                    f"{user.created_at.isoformat()}\n"
                )
            page += 1

    response = Response(
        stream_with_context(generate_csv()),
        mimetype="text/csv",
        headers={
            "Content-Disposition": "attachment; filename=users_export.csv",
            "X-Accel-Buffering": "no",  # Disable nginx buffering
        },
    )
    return response
```

### Streaming a File from Disk

```python
@files_bp.route("/download/<filename>", methods=["GET"])
@auth_required
def download_file(filename: str) -> Response:
    """Stream a file from the upload directory.

    Uses send_from_directory which is safe against directory traversal.
    For very large files, use X-Sendfile with nginx/Apache instead.
    """
    from flask import send_from_directory

    upload_dir = current_app.config["UPLOAD_FOLDER"]
    return send_from_directory(
        upload_dir,
        filename,
        as_attachment=True,
    )
```

### Server-Sent Events (SSE) for Real-Time Streaming

```python
@files_bp.route("/stream/progress/<task_id>", methods=["GET"])
def stream_progress(task_id: str) -> Response:
    """Stream task progress updates via Server-Sent Events."""

    def event_stream() -> Generator[str, None, None]:
        while True:
            progress = TaskService.get_progress(task_id)
            if progress is None:
                yield "event: error\ndata: Task not found\n\n"
                break
            yield f"data: {json.dumps(progress)}\n\n"
            if progress.get("status") in ("completed", "failed"):
                break
            time.sleep(1)

    return Response(
        stream_with_context(event_stream()),
        mimetype="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )
```

---

## Flask Configuration Management

### Multi-Environment Configuration Classes

```python
# src/app/config.py
from __future__ import annotations

import os
from pathlib import Path
from typing import Final

BASE_DIR: Final[Path] = Path(__file__).resolve().parent.parent


class BaseConfig:
    """Shared configuration across all environments."""

    # Security
    SECRET_KEY: str = os.environ.get("SECRET_KEY", "change-me-in-production")
    JWT_SECRET_KEY: str = os.environ.get("JWT_SECRET_KEY", "change-me")
    JWT_ACCESS_TOKEN_EXPIRES: int = 900  # 15 minutes in seconds
    JWT_REFRESH_TOKEN_EXPIRES: int = 2_592_000  # 30 days

    # Database
    SQLALCHEMY_TRACK_MODIFICATIONS: bool = False
    SQLALCHEMY_ENGINE_OPTIONS: dict = {
        "pool_pre_ping": True,
        "pool_recycle": 300,
    }

    # File uploads
    UPLOAD_FOLDER: str = str(BASE_DIR / "uploads")
    MAX_CONTENT_LENGTH: int = 16 * 1024 * 1024  # 16 MB

    # Logging
    LOG_LEVEL: str = "INFO"

    # CORS
    CORS_ORIGINS: list[str] = []

    # Pagination defaults
    DEFAULT_PAGE_SIZE: int = 20
    MAX_PAGE_SIZE: int = 100


class DevelopmentConfig(BaseConfig):
    """Development environment configuration."""

    DEBUG: bool = True
    LOG_LEVEL: str = "DEBUG"
    SQLALCHEMY_DATABASE_URI: str = os.environ.get(
        "DATABASE_URL",
        "postgresql://dev:dev@localhost:5432/myapp_dev",
    )
    CORS_ORIGINS: list[str] = [
        "http://localhost:3000",
        "http://localhost:5173",
    ]


class TestingConfig(BaseConfig):
    """Testing environment configuration.

    Uses a separate database and disables CSRF for easier testing.
    """

    TESTING: bool = True
    DEBUG: bool = True
    LOG_LEVEL: str = "WARNING"
    SQLALCHEMY_DATABASE_URI: str = os.environ.get(
        "TEST_DATABASE_URL",
        "postgresql://dev:dev@localhost:5432/myapp_test",
    )
    # Faster password hashing in tests
    BCRYPT_LOG_ROUNDS: int = 4
    WTF_CSRF_ENABLED: bool = False
    CORS_ORIGINS: list[str] = ["*"]


class StagingConfig(BaseConfig):
    """Staging environment configuration — mirrors production."""

    DEBUG: bool = False
    SQLALCHEMY_DATABASE_URI: str = os.environ["DATABASE_URL"]
    CORS_ORIGINS: list[str] = os.environ.get(
        "CORS_ORIGINS", "https://staging.example.com"
    ).split(",")
    LOG_LEVEL: str = "INFO"


class ProductionConfig(BaseConfig):
    """Production environment configuration."""

    DEBUG: bool = False
    SQLALCHEMY_DATABASE_URI: str = os.environ["DATABASE_URL"]
    SQLALCHEMY_ENGINE_OPTIONS: dict = {
        "pool_pre_ping": True,
        "pool_recycle": 300,
        "pool_size": 20,
        "max_overflow": 30,
    }
    CORS_ORIGINS: list[str] = os.environ.get(
        "CORS_ORIGINS", "https://app.example.com"
    ).split(",")
    LOG_LEVEL: str = "WARNING"

    # Enforce a real secret key in production
    SECRET_KEY: str = os.environ["SECRET_KEY"]
    JWT_SECRET_KEY: str = os.environ["JWT_SECRET_KEY"]


config_by_name: dict[str, type[BaseConfig]] = {
    "development": DevelopmentConfig,
    "testing": TestingConfig,
    "staging": StagingConfig,
    "production": ProductionConfig,
}
```

### Entry Point

```python
# wsgi.py
import os
from app import create_app

config_name = os.environ.get("FLASK_ENV", "development")
app = create_app(config_name)

if __name__ == "__main__":
    app.run()
```

---

## SQLAlchemy Integration with Flask

Flask-SQLAlchemy provides a thin integration layer that manages sessions, engine creation, and scoped sessions tied to the Flask request lifecycle.

### Model Definitions with Mixins

```python
# src/app/domains/users/models.py
from __future__ import annotations

from datetime import datetime, timezone
from typing import Optional

from sqlalchemy import String, Text, Boolean, DateTime, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app import db


class TimestampMixin:
    """Mixin adding created_at and updated_at columns."""

    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=func.now(),
        nullable=False,
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=func.now(),
        onupdate=func.now(),
        nullable=False,
    )


class SoftDeleteMixin:
    """Mixin adding soft-delete support."""

    deleted_at: Mapped[Optional[datetime]] = mapped_column(
        DateTime(timezone=True),
        default=None,
        nullable=True,
    )

    @property
    def is_deleted(self) -> bool:
        return self.deleted_at is not None

    def soft_delete(self) -> None:
        self.deleted_at = datetime.now(timezone.utc)


class User(TimestampMixin, SoftDeleteMixin, db.Model):
    """Application user model."""

    __tablename__ = "users"

    id: Mapped[int] = mapped_column(primary_key=True)
    email: Mapped[str] = mapped_column(
        String(255), unique=True, nullable=False, index=True
    )
    full_name: Mapped[str] = mapped_column(String(255), nullable=False)
    password_hash: Mapped[str] = mapped_column(String(255), nullable=False)
    is_active: Mapped[bool] = mapped_column(
        Boolean, default=True, nullable=False
    )
    bio: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Relationships
    posts: Mapped[list["Post"]] = relationship(
        "Post", back_populates="author", lazy="dynamic"
    )

    def __repr__(self) -> str:
        return f"<User id={self.id} email={self.email!r}>"
```

### Service Layer Pattern

Keep database queries and business logic in a service class, not in the route handlers:

```python
# src/app/domains/users/services.py
from __future__ import annotations

from typing import Any

from flask import abort
from sqlalchemy import select

from app import db
from app.domains.users.models import User
from app.errors import NotFoundError, ConflictError


class UserService:
    """Business logic and data access for users."""

    @staticmethod
    def get_users_query():
        """Return a base query for active, non-deleted users."""
        return User.query.filter(
            User.is_active.is_(True),
            User.deleted_at.is_(None),
        ).order_by(User.created_at.desc())

    @staticmethod
    def get_users_batch(page: int, per_page: int) -> list[User]:
        """Fetch a batch of users for streaming exports."""
        return (
            User.query.filter(User.deleted_at.is_(None))
            .order_by(User.id)
            .offset((page - 1) * per_page)
            .limit(per_page)
            .all()
        )

    @staticmethod
    def get_user_or_404(user_id: int) -> User:
        """Fetch a user by ID or raise NotFoundError."""
        user = db.session.get(User, user_id)
        if user is None or user.is_deleted:
            raise NotFoundError(resource="User", identifier=user_id)
        return user

    @staticmethod
    def create_user(data: dict[str, Any]) -> User:
        """Create a new user. Raises ConflictError if email is taken."""
        existing = User.query.filter_by(email=data["email"]).first()
        if existing:
            raise ConflictError(
                message=f"Email '{data['email']}' is already registered"
            )

        user = User(
            email=data["email"],
            full_name=data["full_name"],
            password_hash=data["password_hash"],
        )
        db.session.add(user)
        db.session.commit()
        return user

    @staticmethod
    def update_user(user_id: int, data: dict[str, Any]) -> User:
        """Update an existing user's attributes."""
        user = UserService.get_user_or_404(user_id)
        for key, value in data.items():
            if hasattr(user, key):
                setattr(user, key, value)
        db.session.commit()
        return user

    @staticmethod
    def delete_user(user_id: int) -> None:
        """Soft-delete a user."""
        user = UserService.get_user_or_404(user_id)
        user.soft_delete()
        db.session.commit()
```

### Database Migrations with Flask-Migrate

Flask-Migrate wraps Alembic and integrates it with Flask CLI:

```bash
# Initialize migrations directory (once)
flask db init

# Generate a migration after model changes
flask db migrate -m "add users table"

# Apply migrations
flask db upgrade

# Rollback one migration
flask db downgrade
```

---

## Flask Extensions Ecosystem

### Flask-Login (Session-Based Auth)

Flask-Login manages user session lifecycle for traditional server-rendered applications:

```python
from flask_login import LoginManager, login_user, logout_user, current_user, login_required

login_manager = LoginManager()
login_manager.login_view = "auth.login"

@login_manager.user_loader
def load_user(user_id: str) -> User | None:
    return db.session.get(User, int(user_id))

# In create_app():
login_manager.init_app(app)
```

### Flask-JWT-Extended (Token-Based Auth for APIs)

Flask-JWT-Extended provides JWT access/refresh token management for stateless API authentication:

```python
# src/app/domains/auth/routes.py
from __future__ import annotations

from http import HTTPStatus

from flask import Blueprint, jsonify, request, Response
from flask_jwt_extended import (
    JWTManager,
    create_access_token,
    create_refresh_token,
    get_jwt_identity,
    jwt_required,
)

from app.domains.auth.services import AuthService

auth_bp = Blueprint("auth", __name__)

# Initialize JWTManager in create_app: jwt = JWTManager(app)


@auth_bp.route("/login", methods=["POST"])
def login() -> tuple[Response, int]:
    """Authenticate and return access + refresh tokens."""
    data = request.get_json(silent=True)
    if not data or "email" not in data or "password" not in data:
        return jsonify({"error": "Email and password required"}), HTTPStatus.BAD_REQUEST

    user = AuthService.authenticate(data["email"], data["password"])
    if user is None:
        return jsonify({"error": "Invalid credentials"}), HTTPStatus.UNAUTHORIZED

    access_token = create_access_token(
        identity=str(user.id),
        additional_claims={"email": user.email},
    )
    refresh_token = create_refresh_token(identity=str(user.id))

    return jsonify({
        "access_token": access_token,
        "refresh_token": refresh_token,
        "token_type": "Bearer",
    }), HTTPStatus.OK


@auth_bp.route("/refresh", methods=["POST"])
@jwt_required(refresh=True)
def refresh() -> tuple[Response, int]:
    """Issue a new access token using a valid refresh token."""
    current_user_id = get_jwt_identity()
    new_access_token = create_access_token(identity=current_user_id)
    return jsonify({"access_token": new_access_token}), HTTPStatus.OK


@auth_bp.route("/me", methods=["GET"])
@jwt_required()
def get_current_user() -> tuple[Response, int]:
    """Return the currently authenticated user's profile."""
    current_user_id = get_jwt_identity()
    user = AuthService.get_user_by_id(int(current_user_id))
    return jsonify({
        "data": {
            "id": user.id,
            "email": user.email,
            "full_name": user.full_name,
        },
    }), HTTPStatus.OK
```

### Flask-Migrate (Database Migrations)

Already covered in the SQLAlchemy section. The key integration point:

```python
from flask_migrate import Migrate

migrate = Migrate()

# In create_app:
migrate.init_app(app, db)
```

### Custom auth_required Decorator

A reusable decorator that combines JWT verification with permission checking:

```python
# src/app/shared/decorators.py
from __future__ import annotations

from functools import wraps
from typing import Any, Callable

from flask import g, jsonify
from flask_jwt_extended import get_jwt_identity, verify_jwt_in_request

from app.domains.users.services import UserService


def auth_required(fn: Callable[..., Any]) -> Callable[..., Any]:
    """Decorator requiring a valid JWT and an active user.

    Populates g.current_user for the view function.
    """

    @wraps(fn)
    def wrapper(*args: Any, **kwargs: Any) -> Any:
        verify_jwt_in_request()
        user_id = get_jwt_identity()
        user = UserService.get_user_or_404(int(user_id))
        if not user.is_active:
            return jsonify({"error": "Account is deactivated"}), 403
        g.current_user = user
        return fn(*args, **kwargs)

    return wrapper
```

---

## Performance: Connection Pooling and Response Compression

### Connection Pooling with SQLAlchemy

SQLAlchemy uses connection pooling by default. Fine-tune pool settings in the production configuration:

```python
class ProductionConfig(BaseConfig):
    SQLALCHEMY_ENGINE_OPTIONS: dict = {
        # Check connections before using them (avoids stale connection errors)
        "pool_pre_ping": True,
        # Recycle connections after 5 minutes to handle server-side timeouts
        "pool_recycle": 300,
        # Maintain 20 connections in the pool
        "pool_size": 20,
        # Allow up to 30 additional overflow connections under load
        "max_overflow": 30,
        # Timeout for getting a connection from the pool (seconds)
        "pool_timeout": 30,
    }
```

Key pool tuning guidance:

- `pool_size` should match your expected concurrent request count per worker.
- `max_overflow` provides headroom for burst traffic.
- `pool_pre_ping` adds a small overhead but prevents "server closed the connection unexpectedly" errors.
- `pool_recycle` should be less than the database server's `wait_timeout` (MySQL) or `idle_in_transaction_session_timeout` (PostgreSQL).
- With Gunicorn workers, each worker gets its own pool. Total connections = `workers * (pool_size + max_overflow)`.

### Response Compression with Flask-Compress

Flask-Compress applies gzip/brotli compression to responses automatically:

```
pip install flask-compress
```

```python
from flask_compress import Compress

compress = Compress()

# In create_app:
compress.init_app(app)
```

Configuration options:

```python
class BaseConfig:
    # Only compress responses larger than 500 bytes
    COMPRESS_MIN_SIZE: int = 500
    # Compression level (1-9, higher = more compression, more CPU)
    COMPRESS_LEVEL: int = 6
    # MIME types to compress
    COMPRESS_MIMETYPES: list[str] = [
        "text/html",
        "text/css",
        "text/xml",
        "text/plain",
        "application/json",
        "application/javascript",
    ]
    # Prefer brotli over gzip when supported
    COMPRESS_ALGORITHM: list[str] = ["br", "gzip"]
```

### Caching Headers

For endpoints that return rarely-changing data, add caching headers to reduce server load:

```python
from flask import make_response

@app.route("/api/config")
def get_app_config() -> Response:
    """Return application configuration that rarely changes."""
    response = make_response(jsonify({"version": "2.1.0", "features": [...]}))
    response.headers["Cache-Control"] = "public, max-age=3600"
    response.headers["ETag"] = compute_etag(response.data)
    return response
```

### Gunicorn Production Deployment

```python
# gunicorn.conf.py
import multiprocessing

# Workers
workers = multiprocessing.cpu_count() * 2 + 1
worker_class = "gthread"
threads = 4

# Networking
bind = "0.0.0.0:8000"
backlog = 2048
keepalive = 5

# Timeouts
timeout = 120
graceful_timeout = 30

# Logging
accesslog = "-"
errorlog = "-"
loglevel = "info"

# Security
limit_request_line = 8190
limit_request_fields = 100
```

---

## Best Practices

### Application Structure

- **Always use the application factory pattern.** It prevents circular imports, enables multiple configurations, and is required by Flask-Migrate and most extensions.
- **Organize by domain, not by layer.** Group routes, models, services, and schemas for a feature together instead of putting all routes in one folder and all models in another.
- **Keep route handlers thin.** Move business logic into service classes. Route handlers should parse input, call a service, and format the output.
- **Use `current_app` instead of importing the app directly.** This ensures your code works with any app instance, not just a specific global one.

### Configuration and Security

- **Never commit secrets to version control.** Load `SECRET_KEY`, `DATABASE_URL`, and API keys from environment variables. Use a `.env` file locally with `python-dotenv`.
- **Set `MAX_CONTENT_LENGTH` in the Flask config.** This rejects oversized requests at the WSGI level before your view function runs.
- **Use separate config classes for each environment.** This makes it easy to verify what differs between development, testing, staging, and production.
- **Always set `SQLALCHEMY_TRACK_MODIFICATIONS = False`.** The modification tracking system consumes memory and is not needed for most applications.

### Error Handling

- **Register a catch-all error handler for `Exception`.** This prevents Flask from returning HTML error pages for unhandled exceptions in an API.
- **Use custom exception classes** (e.g., `NotFoundError`, `ConflictError`) rather than generic `abort()` calls. They carry structured context and make debugging easier.
- **Log full tracebacks for 500 errors** in the catch-all handler. Without this, you lose debugging information.

### Database

- **Use Flask-Migrate for all schema changes.** Never modify the database schema manually in production.
- **Enable `pool_pre_ping`** to handle dropped connections gracefully.
- **Scope sessions to the request lifecycle.** Flask-SQLAlchemy does this automatically. Do not create manual sessions unless you have a specific reason.
- **Use the service layer for database operations.** Do not execute queries directly in route handlers.

### File Uploads

- **Always use `secure_filename()`.** Never use the original filename directly for filesystem operations.
- **Generate unique filenames** (UUID-based) to prevent overwrites and information leakage.
- **Validate both extension and MIME type.** Extension checks alone are insufficient because users can rename files.
- **Check actual file size** by seeking to the end of the stream, not by trusting `Content-Length`.

### Performance

- **Use streaming responses for large data exports.** Do not load the entire dataset into memory.
- **Enable response compression** with Flask-Compress for JSON APIs.
- **Deploy with Gunicorn using gthread workers** for a good balance of concurrency and simplicity.
- **Tune the SQLAlchemy connection pool** based on your worker count and expected concurrency.

---

## Anti-Patterns

### Running Flask's Development Server in Production

```python
# WRONG: The built-in server is single-threaded and not hardened
if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)

# CORRECT: Use Gunicorn or uWSGI
# gunicorn -w 4 -b 0.0.0.0:8000 wsgi:app
```

### Using a Global App Instance Instead of the Factory Pattern

```python
# WRONG: Module-level app causes circular imports and cannot be reconfigured
app = Flask(__name__)
db = SQLAlchemy(app)

# CORRECT: Use create_app() and init_app()
db = SQLAlchemy()

def create_app() -> Flask:
    app = Flask(__name__)
    db.init_app(app)
    return app
```

### Putting Business Logic in Route Handlers

```python
# WRONG: Route handler doing database queries and business logic
@users_bp.route("/<int:user_id>", methods=["DELETE"])
def delete_user(user_id: int):
    user = User.query.get(user_id)
    if not user:
        abort(404)
    user.deleted_at = datetime.now()
    db.session.commit()
    return jsonify({"message": "deleted"}), 200

# CORRECT: Delegate to a service
@users_bp.route("/<int:user_id>", methods=["DELETE"])
@auth_required
def delete_user(user_id: int):
    UserService.delete_user(user_id)
    return jsonify({"message": "User deleted"}), 200
```

### Trusting the Original Filename

```python
# WRONG: Directly using the user-supplied filename
file.save(os.path.join(upload_dir, file.filename))

# CORRECT: Sanitize and generate a unique name
safe_name = secure_filename(file.filename)
unique_name = f"{uuid4().hex}_{safe_name}"
file.save(os.path.join(upload_dir, unique_name))
```

### Catching Exceptions Too Broadly in Route Handlers

```python
# WRONG: Swallowing all exceptions silently
@app.route("/api/data")
def get_data():
    try:
        return jsonify(SomeService.get_data())
    except Exception:
        return jsonify({"error": "Something went wrong"}), 500

# CORRECT: Let the global error handler deal with unexpected exceptions.
# Only catch expected, domain-specific exceptions in views.
@app.route("/api/data")
def get_data():
    try:
        data = SomeService.get_data()
    except ValidationError as e:
        return jsonify({"error": str(e)}), 422
    return jsonify(data)
```

### Disabling CORS Entirely in Production

```python
# WRONG: Allowing all origins in production
CORS(app, origins="*", supports_credentials=True)

# CORRECT: Restrict to known frontend domains
CORS(app, origins=["https://app.example.com"], supports_credentials=True)
```

### Not Setting SQLAlchemy Pool Options

```python
# WRONG: Using defaults in production (pool_size=5, no pre_ping)
SQLALCHEMY_DATABASE_URI = os.environ["DATABASE_URL"]

# CORRECT: Tune the pool for your workload
SQLALCHEMY_ENGINE_OPTIONS = {
    "pool_pre_ping": True,
    "pool_recycle": 300,
    "pool_size": 20,
    "max_overflow": 30,
}
```

### Loading All Records for Export

```python
# WRONG: Loading all records into memory
@app.route("/export")
def export():
    users = User.query.all()  # OOM for large tables
    csv_data = generate_csv(users)
    return Response(csv_data, mimetype="text/csv")

# CORRECT: Stream records in batches
@app.route("/export")
def export():
    def generate():
        yield "id,email\n"
        page = 1
        while True:
            batch = User.query.order_by(User.id).offset((page - 1) * 1000).limit(1000).all()
            if not batch:
                break
            for user in batch:
                yield f"{user.id},{user.email}\n"
            page += 1
    return Response(stream_with_context(generate()), mimetype="text/csv")
```

---

## Sources & References

- [Flask Official Documentation](https://flask.palletsprojects.com/en/stable/) — Application factory, blueprints, request context, error handling, configuration, and the full API reference.
- [Flask-SQLAlchemy Documentation](https://flask-sqlalchemy.readthedocs.io/en/stable/) — Integration of SQLAlchemy with Flask, session management, model definition, and query patterns.
- [Flask-JWT-Extended Documentation](https://flask-jwt-extended.readthedocs.io/en/stable/) — JWT token creation, refresh tokens, custom claims, token blocklisting, and decorator-based route protection.
- [Flask-CORS Documentation](https://flask-cors.readthedocs.io/en/latest/) — Cross-Origin Resource Sharing configuration, per-blueprint CORS, and credential handling.
- [Flask-Migrate Documentation](https://flask-migrate.readthedocs.io/en/latest/) — Alembic integration for Flask, migration generation, upgrade/downgrade workflows.
- [Flask-Compress Documentation](https://github.com/colour-science/flask-compress) — Response compression middleware supporting gzip and brotli.
- [Gunicorn Documentation — Design and Configuration](https://docs.gunicorn.org/en/stable/design.html) — Worker types, process management, and production deployment tuning.
- [SQLAlchemy Engine Configuration](https://docs.sqlalchemy.org/en/20/core/engines.html) — Connection pool parameters, pool_pre_ping, pool_recycle, and engine options.
- [Werkzeug Utilities — secure_filename](https://werkzeug.palletsprojects.com/en/stable/utils/#werkzeug.utils.secure_filename) — Filename sanitization for safe file uploads.
