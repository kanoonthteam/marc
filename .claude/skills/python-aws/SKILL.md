---
name: python-aws
description: Production-grade Python AWS patterns -- boto3 S3 operations, presigned URLs, IAM credentials, bucket management, multipart uploads, and cost optimization
---

# Python AWS (boto3) -- Staff Engineer Patterns

Production-ready patterns for boto3 S3 operations, presigned URL generation, IAM credential management, bucket policies, multipart uploads, event notifications, and cost optimization using Python 3.11+.

## Table of Contents
1. [boto3 Client vs Resource API](#boto3-client-vs-resource-api)
2. [Session Management & Credential Chain](#session-management--credential-chain)
3. [IAM Credential Management](#iam-credential-management)
4. [S3 Core Operations](#s3-core-operations)
5. [Presigned URL Generation](#presigned-url-generation)
6. [Multipart Upload for Large Files](#multipart-upload-for-large-files)
7. [S3 Bucket Policies & CORS Configuration](#s3-bucket-policies--cors-configuration)
8. [Error Handling Patterns](#error-handling-patterns)
9. [S3 Event Notifications](#s3-event-notifications)
10. [Transfer Acceleration & Performance Optimization](#transfer-acceleration--performance-optimization)
11. [S3 Select for Server-Side Querying](#s3-select-for-server-side-querying)
12. [Cost Optimization](#cost-optimization)
13. [Testing with moto & localstack](#testing-with-moto--localstack)
14. [Best Practices](#best-practices)
15. [Anti-Patterns](#anti-patterns)
16. [Sources & References](#sources--references)

---

## boto3 Client vs Resource API

### Client API (Low-Level)

The client API maps 1:1 to AWS service API operations. It returns raw dictionaries and gives full control over request/response handling. Use the client API when you need access to every parameter an API exposes, or when the resource API does not cover a specific operation.

Key characteristics:
- Returns plain `dict` responses
- Provides access to every AWS API action
- Requires manual pagination via `get_paginator()`
- Better for automation scripts and Lambda functions where startup time matters
- Explicitly typed with `mypy-boto3-s3` stubs

### Resource API (High-Level)

The resource API provides an object-oriented abstraction over the client. It exposes AWS resources as Python objects with attributes and actions. Note that AWS has announced the resource API will not receive new features -- new services only get client API support.

Key characteristics:
- Returns rich Python objects (`s3.Object`, `s3.Bucket`)
- Built-in lazy loading of attributes
- Collection iterators with automatic pagination
- Simpler code for common CRUD operations
- Limited to a subset of services (S3, EC2, IAM, DynamoDB, SQS, SNS, CloudFormation, CloudWatch, Glacier)

### When to Use Which

| Criteria | Client | Resource |
|----------|--------|----------|
| New projects | Preferred | Acceptable for S3/EC2 |
| Full API coverage | Yes | No |
| Pagination | Manual (paginator) | Automatic (collections) |
| Return type | `dict` | Python objects |
| Type hint support | Excellent (`mypy-boto3`) | Limited |
| Lambda cold start | Faster | Slower (loads metadata) |
| AWS recommendation | Long-term supported | Maintenance mode |

**Recommendation**: Default to the client API for new code. Use the resource API only when its object-oriented convenience significantly reduces complexity (e.g., iterating all objects in a bucket).

---

## Session Management & Credential Chain

### The Credential Resolution Order

boto3 resolves credentials in a specific order. Understanding this chain is critical for debugging authentication failures:

1. **Explicit parameters** passed to `boto3.client()` or `boto3.Session()`
2. **Environment variables**: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`
3. **Shared credential file**: `~/.aws/credentials`
4. **AWS config file**: `~/.aws/config`
5. **Assume role provider** (from config profiles with `role_arn`)
6. **boto3 config file**: `/etc/boto.cfg` and `~/.boto`
7. **Container credential provider** (ECS task role via `AWS_CONTAINER_CREDENTIALS_RELATIVE_URI`)
8. **Instance metadata service** (EC2 instance role / IMDS v2)

### Session Creation Patterns

```python
from __future__ import annotations

import boto3
from botocore.config import Config
from mypy_boto3_s3 import S3Client
from mypy_boto3_s3.type_defs import PutObjectOutputTypeDef


def create_s3_client_default() -> S3Client:
    """Use the default credential chain. Suitable for Lambda, ECS, EC2."""
    return boto3.client(
        "s3",
        config=Config(
            retries={"max_attempts": 3, "mode": "adaptive"},
            max_pool_connections=25,
            connect_timeout=5,
            read_timeout=10,
        ),
    )


def create_s3_client_with_profile(profile_name: str, region: str = "us-east-1") -> S3Client:
    """Use a named profile from ~/.aws/credentials. Suitable for local development."""
    session = boto3.Session(profile_name=profile_name, region_name=region)
    return session.client(
        "s3",
        config=Config(
            retries={"max_attempts": 3, "mode": "adaptive"},
        ),
    )


def create_s3_client_with_assume_role(
    role_arn: str,
    session_name: str = "app-session",
    region: str = "us-east-1",
) -> S3Client:
    """Assume an IAM role and return an S3 client with temporary credentials."""
    sts_client = boto3.client("sts", region_name=region)
    response = sts_client.assume_role(
        RoleArn=role_arn,
        RoleSessionName=session_name,
        DurationSeconds=3600,
    )
    credentials = response["Credentials"]

    return boto3.client(
        "s3",
        region_name=region,
        aws_access_key_id=credentials["AccessKeyId"],
        aws_secret_access_key=credentials["SecretAccessKey"],
        aws_session_token=credentials["SessionToken"],
        config=Config(retries={"max_attempts": 3, "mode": "adaptive"}),
    )
```

### Thread Safety

boto3 sessions are **not** thread-safe. Each thread should create its own session and client. Clients created from the same session are also not thread-safe. The recommended pattern for multi-threaded applications:

```python
from __future__ import annotations

import concurrent.futures
from typing import Any

import boto3
from botocore.config import Config
from mypy_boto3_s3 import S3Client


def _get_thread_local_client(region: str = "us-east-1") -> S3Client:
    """Create a per-thread S3 client. Call this inside each thread."""
    session = boto3.Session()
    return session.client(
        "s3",
        region_name=region,
        config=Config(
            retries={"max_attempts": 3, "mode": "adaptive"},
            max_pool_connections=10,
        ),
    )


def download_objects_parallel(
    bucket: str,
    keys: list[str],
    dest_dir: str,
    max_workers: int = 10,
) -> list[str]:
    """Download multiple S3 objects in parallel using thread-safe clients."""
    downloaded: list[str] = []

    def _download(key: str) -> str:
        client = _get_thread_local_client()
        local_path = f"{dest_dir}/{key.split('/')[-1]}"
        client.download_file(bucket, key, local_path)
        return local_path

    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {executor.submit(_download, key): key for key in keys}
        for future in concurrent.futures.as_completed(futures):
            downloaded.append(future.result())

    return downloaded
```

---

## IAM Credential Management

### Environment Variables

The simplest approach for CI/CD pipelines and containers:

```bash
export AWS_ACCESS_KEY_ID="AKIAIOSFODNN7EXAMPLE"
export AWS_SECRET_ACCESS_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
export AWS_DEFAULT_REGION="us-east-1"
# Optional: for temporary credentials (STS)
export AWS_SESSION_TOKEN="FwoGZXIvYXdzE..."
```

### Named Profiles

For local development with multiple AWS accounts:

```ini
# ~/.aws/credentials
[default]
aws_access_key_id = AKIAIOSFODNN7EXAMPLE
aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

[staging]
aws_access_key_id = AKIAI44QH8DHBEXAMPLE
aws_secret_access_key = je7MtGbClwBF/2Zp9Utk/h3yCo8nvbEXAMPLEKEY

# ~/.aws/config
[profile staging]
region = us-west-2
output = json

[profile production]
role_arn = arn:aws:iam::123456789012:role/ProductionAccess
source_profile = default
region = us-east-1
mfa_serial = arn:aws:iam::123456789012:mfa/developer
```

### Instance Roles (EC2 / ECS / Lambda)

The preferred approach for production workloads. No credentials are stored in code or environment variables. The AWS SDK automatically retrieves temporary credentials from the Instance Metadata Service (IMDS v2) or the ECS task credential endpoint.

**EC2 Instance Profile**: Attach an IAM role to the EC2 instance. boto3 auto-discovers credentials via IMDS v2.

**ECS Task Role**: Define `taskRoleArn` in the ECS task definition. The ECS agent injects credentials via the container credential provider.

**Lambda Execution Role**: Defined in the Lambda function configuration. Credentials are injected via environment variables automatically.

### Credential Refresh for Long-Running Processes

For applications that run longer than the STS token lifetime (default 1 hour), use `RefreshableCredentials`:

```python
from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

import boto3
from botocore.credentials import RefreshableCredentials
from botocore.session import get_session


def create_refreshable_session(
    role_arn: str,
    session_name: str = "refreshable-session",
    region: str = "us-east-1",
) -> boto3.Session:
    """Create a boto3 session with auto-refreshing credentials via STS AssumeRole."""

    def _refresh() -> dict[str, Any]:
        sts_client = boto3.client("sts", region_name=region)
        response = sts_client.assume_role(
            RoleArn=role_arn,
            RoleSessionName=session_name,
            DurationSeconds=3600,
        )
        credentials = response["Credentials"]
        return {
            "access_key": credentials["AccessKeyId"],
            "secret_key": credentials["SecretAccessKey"],
            "token": credentials["SessionToken"],
            "expiry_time": credentials["Expiration"].isoformat(),
        }

    refreshable_credentials = RefreshableCredentials.create_from_metadata(
        metadata=_refresh(),
        refresh_using=_refresh,
        method="sts-assume-role",
    )

    botocore_session = get_session()
    botocore_session._credentials = refreshable_credentials
    botocore_session.set_config_variable("region", region)

    return boto3.Session(botocore_session=botocore_session)
```

---

## S3 Core Operations

### upload_file vs put_object

`upload_file` is a managed transfer that automatically handles multipart uploads for large files. `put_object` sends data in a single PUT request and is better for small objects or when you already have the content in memory.

| Method | Input | Multipart | Progress Callback | Best For |
|--------|-------|-----------|-------------------|----------|
| `upload_file` | File path | Automatic | Yes | Files on disk |
| `upload_fileobj` | File-like object | Automatic | Yes | In-memory / streams |
| `put_object` | `bytes` or file-like | No | No | Small objects (<= 5 MB) |

### download_file vs get_object

`download_file` is a managed transfer that handles multipart downloads and retries. `get_object` returns a streaming response body.

| Method | Output | Multipart | Range Requests | Best For |
|--------|--------|-----------|----------------|----------|
| `download_file` | Writes to disk | Automatic | No | Downloading to file |
| `download_fileobj` | Writes to file-like | Automatic | No | Downloading to memory |
| `get_object` | `StreamingBody` | No | Yes (via `Range`) | Partial reads, streaming |

### Production S3 Operations Module

```python
from __future__ import annotations

import hashlib
import logging
import mimetypes
from pathlib import Path
from typing import Any, BinaryIO

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError
from mypy_boto3_s3 import S3Client
from mypy_boto3_s3.type_defs import (
    GetObjectOutputTypeDef,
    HeadObjectOutputTypeDef,
    PutObjectOutputTypeDef,
)

logger = logging.getLogger(__name__)


class S3Service:
    """Production S3 service with typed methods and structured error handling."""

    def __init__(
        self,
        bucket_name: str,
        region: str = "us-east-1",
        client: S3Client | None = None,
    ) -> None:
        self._bucket = bucket_name
        self._client: S3Client = client or boto3.client(
            "s3",
            region_name=region,
            config=Config(
                retries={"max_attempts": 3, "mode": "adaptive"},
                max_pool_connections=25,
            ),
        )

    def upload_file(
        self,
        local_path: str | Path,
        key: str,
        content_type: str | None = None,
        metadata: dict[str, str] | None = None,
        storage_class: str = "STANDARD",
    ) -> str:
        """Upload a file from disk to S3 with automatic multipart handling.

        Returns the S3 URI of the uploaded object.
        """
        local_path = Path(local_path)
        if not local_path.is_file():
            raise FileNotFoundError(f"Local file not found: {local_path}")

        if content_type is None:
            content_type, _ = mimetypes.guess_type(str(local_path))
            content_type = content_type or "application/octet-stream"

        extra_args: dict[str, Any] = {
            "ContentType": content_type,
            "StorageClass": storage_class,
        }
        if metadata:
            extra_args["Metadata"] = metadata

        try:
            self._client.upload_file(
                Filename=str(local_path),
                Bucket=self._bucket,
                Key=key,
                ExtraArgs=extra_args,
            )
            s3_uri = f"s3://{self._bucket}/{key}"
            logger.info("Uploaded %s to %s", local_path, s3_uri)
            return s3_uri
        except ClientError as exc:
            logger.error("Failed to upload %s: %s", key, exc.response["Error"]["Message"])
            raise

    def upload_bytes(
        self,
        data: bytes,
        key: str,
        content_type: str = "application/octet-stream",
        metadata: dict[str, str] | None = None,
    ) -> PutObjectOutputTypeDef:
        """Upload in-memory bytes directly via put_object."""
        try:
            response = self._client.put_object(
                Bucket=self._bucket,
                Key=key,
                Body=data,
                ContentType=content_type,
                ContentMD5=hashlib.md5(data).hexdigest(),  # noqa: S324 -- AWS requires MD5
                Metadata=metadata or {},
            )
            logger.info("Put object %s (%d bytes)", key, len(data))
            return response
        except ClientError as exc:
            logger.error("Failed to put %s: %s", key, exc.response["Error"]["Message"])
            raise

    def download_file(self, key: str, local_path: str | Path) -> Path:
        """Download an S3 object to disk with automatic multipart handling."""
        local_path = Path(local_path)
        local_path.parent.mkdir(parents=True, exist_ok=True)

        try:
            self._client.download_file(
                Bucket=self._bucket,
                Key=key,
                Filename=str(local_path),
            )
            logger.info("Downloaded %s to %s", key, local_path)
            return local_path
        except ClientError as exc:
            error_code = exc.response["Error"]["Code"]
            if error_code == "404":
                raise FileNotFoundError(f"S3 object not found: s3://{self._bucket}/{key}") from exc
            raise

    def get_object_stream(self, key: str) -> GetObjectOutputTypeDef:
        """Get an S3 object as a streaming response. Caller must read and close the body."""
        try:
            return self._client.get_object(Bucket=self._bucket, Key=key)
        except ClientError as exc:
            error_code = exc.response["Error"]["Code"]
            if error_code == "NoSuchKey":
                raise FileNotFoundError(f"S3 object not found: s3://{self._bucket}/{key}") from exc
            raise

    def head_object(self, key: str) -> HeadObjectOutputTypeDef | None:
        """Check if an object exists and return its metadata. Returns None if not found."""
        try:
            return self._client.head_object(Bucket=self._bucket, Key=key)
        except ClientError as exc:
            if exc.response["Error"]["Code"] == "404":
                return None
            raise

    def delete_object(self, key: str) -> None:
        """Delete a single object. S3 returns success even if the key does not exist."""
        self._client.delete_object(Bucket=self._bucket, Key=key)
        logger.info("Deleted %s", key)

    def delete_objects_bulk(self, keys: list[str]) -> int:
        """Delete up to 1000 objects in a single request. Returns count of deleted objects."""
        if not keys:
            return 0
        if len(keys) > 1000:
            raise ValueError("delete_objects supports a maximum of 1000 keys per call")

        response = self._client.delete_objects(
            Bucket=self._bucket,
            Delete={"Objects": [{"Key": k} for k in keys], "Quiet": True},
        )
        errors = response.get("Errors", [])
        if errors:
            logger.error("Bulk delete errors: %s", errors)
        return len(keys) - len(errors)

    def list_objects(
        self,
        prefix: str = "",
        max_keys: int | None = None,
    ) -> list[dict[str, Any]]:
        """List objects under a prefix using automatic pagination."""
        paginator = self._client.get_paginator("list_objects_v2")
        page_config: dict[str, Any] = {}
        if max_keys:
            page_config["MaxItems"] = max_keys

        objects: list[dict[str, Any]] = []
        for page in paginator.paginate(
            Bucket=self._bucket,
            Prefix=prefix,
            PaginationConfig=page_config,
        ):
            for obj in page.get("Contents", []):
                objects.append(
                    {
                        "key": obj["Key"],
                        "size": obj["Size"],
                        "last_modified": obj["LastModified"],
                        "etag": obj["ETag"],
                    }
                )
        return objects

    def copy_object(
        self,
        source_key: str,
        dest_key: str,
        dest_bucket: str | None = None,
    ) -> None:
        """Copy an object within the same bucket or to another bucket."""
        dest_bucket = dest_bucket or self._bucket
        copy_source = {"Bucket": self._bucket, "Key": source_key}
        self._client.copy_object(
            CopySource=copy_source,
            Bucket=dest_bucket,
            Key=dest_key,
        )
        logger.info("Copied %s -> %s/%s", source_key, dest_bucket, dest_key)
```

---

## Presigned URL Generation

Presigned URLs allow clients to upload or download S3 objects without AWS credentials. The URL embeds a signature that expires after a configurable duration.

### Security Considerations

- Presigned URLs inherit the permissions of the IAM principal that created them
- If the IAM principal's permissions are revoked, existing presigned URLs stop working immediately
- Always set the shortest practical expiration time
- For uploads, constrain `Content-Type` and `Content-Length` using conditions
- Use HTTPS only -- presigned URLs over HTTP expose the signature in transit

### Presigned URL Service

```python
from __future__ import annotations

import logging
from typing import Any

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError
from mypy_boto3_s3 import S3Client

logger = logging.getLogger(__name__)

# IMPORTANT: For presigned URLs, use path-style or ensure the client region
# matches the bucket region. Virtual-hosted style with wrong region causes
# SignatureDoesNotMatch errors.
_PRESIGN_CONFIG = Config(
    signature_version="s3v4",
    retries={"max_attempts": 3, "mode": "adaptive"},
)


class PresignedUrlService:
    """Generate presigned URLs for secure direct-to-S3 uploads and downloads."""

    def __init__(
        self,
        bucket_name: str,
        region: str = "us-east-1",
        client: S3Client | None = None,
    ) -> None:
        self._bucket = bucket_name
        self._region = region
        self._client: S3Client = client or boto3.client(
            "s3",
            region_name=region,
            config=_PRESIGN_CONFIG,
        )

    def generate_download_url(
        self,
        key: str,
        expires_in: int = 3600,
        response_content_type: str | None = None,
    ) -> str:
        """Generate a presigned GET URL for downloading an object.

        Args:
            key: S3 object key.
            expires_in: URL lifetime in seconds (max 604800 = 7 days for IAM users,
                        max 36 hours for STS temporary credentials).
            response_content_type: Override Content-Type in the response headers.

        Returns:
            The presigned URL string.
        """
        params: dict[str, Any] = {"Bucket": self._bucket, "Key": key}
        if response_content_type:
            params["ResponseContentType"] = response_content_type

        try:
            url: str = self._client.generate_presigned_url(
                ClientMethod="get_object",
                Params=params,
                ExpiresIn=expires_in,
            )
            logger.info("Generated download URL for %s (expires in %ds)", key, expires_in)
            return url
        except ClientError as exc:
            logger.error("Failed to generate download URL for %s: %s", key, exc)
            raise

    def generate_upload_url(
        self,
        key: str,
        content_type: str = "application/octet-stream",
        expires_in: int = 3600,
        max_content_length: int | None = None,
    ) -> dict[str, Any]:
        """Generate a presigned PUT URL for uploading an object.

        Args:
            key: Destination S3 key.
            content_type: Required Content-Type for the upload.
            expires_in: URL lifetime in seconds.
            max_content_length: If set, the upload will fail if Content-Length exceeds this.

        Returns:
            Dict with 'url', 'method', 'headers' that the client must use.
        """
        params: dict[str, Any] = {
            "Bucket": self._bucket,
            "Key": key,
            "ContentType": content_type,
        }

        try:
            url: str = self._client.generate_presigned_url(
                ClientMethod="put_object",
                Params=params,
                ExpiresIn=expires_in,
            )
            headers = {"Content-Type": content_type}
            result: dict[str, Any] = {
                "url": url,
                "method": "PUT",
                "headers": headers,
                "key": key,
                "expires_in": expires_in,
            }
            logger.info("Generated upload URL for %s (expires in %ds)", key, expires_in)
            return result
        except ClientError as exc:
            logger.error("Failed to generate upload URL for %s: %s", key, exc)
            raise

    def generate_presigned_post(
        self,
        key_prefix: str,
        content_type: str = "application/octet-stream",
        max_content_length: int = 10 * 1024 * 1024,  # 10 MB
        expires_in: int = 3600,
    ) -> dict[str, Any]:
        """Generate a presigned POST for browser-based uploads with size constraints.

        This is preferred over PUT presigned URLs for browser uploads because it
        allows enforcing content-length limits and other conditions server-side.

        Returns:
            Dict with 'url' and 'fields' for constructing a multipart/form-data POST.
        """
        conditions: list[Any] = [
            ["content-length-range", 1, max_content_length],
            ["starts-with", "$Content-Type", content_type.split("/")[0]],
            ["starts-with", "$key", key_prefix],
        ]
        fields: dict[str, str] = {"Content-Type": content_type}

        try:
            response = self._client.generate_presigned_post(
                Bucket=self._bucket,
                Key=f"{key_prefix}/${{filename}}",
                Fields=fields,
                Conditions=conditions,
                ExpiresIn=expires_in,
            )
            logger.info(
                "Generated presigned POST for prefix %s (max %d bytes, expires in %ds)",
                key_prefix,
                max_content_length,
                expires_in,
            )
            return response
        except ClientError as exc:
            logger.error("Failed to generate presigned POST for %s: %s", key_prefix, exc)
            raise
```

---

## Multipart Upload for Large Files

For objects larger than ~100 MB, use multipart upload to improve throughput, allow resumable uploads, and reduce the impact of network failures. boto3's managed transfers (`upload_file`, `upload_fileobj`) handle multipart automatically, but you may need manual control for custom progress tracking or resume-from-failure scenarios.

### Automatic Multipart via TransferConfig

```python
from __future__ import annotations

import logging
from pathlib import Path
from typing import Any

import boto3
from boto3.s3.transfer import TransferConfig
from botocore.config import Config
from mypy_boto3_s3 import S3Client

logger = logging.getLogger(__name__)

# Tune these based on your network and object sizes
TRANSFER_CONFIG = TransferConfig(
    multipart_threshold=8 * 1024 * 1024,    # 8 MB -- switch to multipart above this
    max_concurrency=10,                       # parallel upload threads
    multipart_chunksize=8 * 1024 * 1024,     # 8 MB per part
    num_download_attempts=3,
    use_threads=True,
)


class ProgressCallback:
    """Track upload/download progress with percentage logging."""

    def __init__(self, filename: str, total_size: int) -> None:
        self._filename = filename
        self._total = total_size
        self._transferred = 0

    def __call__(self, bytes_transferred: int) -> None:
        self._transferred += bytes_transferred
        pct = (self._transferred / self._total) * 100 if self._total > 0 else 0
        logger.info("Transfer %s: %.1f%% (%d / %d bytes)", self._filename, pct, self._transferred, self._total)


def upload_large_file(
    client: S3Client,
    bucket: str,
    key: str,
    local_path: str | Path,
    content_type: str = "application/octet-stream",
) -> None:
    """Upload a large file with automatic multipart and progress tracking."""
    local_path = Path(local_path)
    file_size = local_path.stat().st_size
    callback = ProgressCallback(local_path.name, file_size)

    client.upload_file(
        Filename=str(local_path),
        Bucket=bucket,
        Key=key,
        ExtraArgs={"ContentType": content_type},
        Config=TRANSFER_CONFIG,
        Callback=callback,
    )
    logger.info("Completed upload of %s (%d bytes)", key, file_size)
```

### Manual Multipart Upload with Resume Support

For scenarios requiring resume-on-failure (e.g., uploading a 50 GB file over an unreliable connection), use the low-level multipart API:

```python
from __future__ import annotations

import hashlib
import json
import logging
from pathlib import Path
from typing import Any

import boto3
from botocore.exceptions import ClientError
from mypy_boto3_s3 import S3Client

logger = logging.getLogger(__name__)

PART_SIZE = 100 * 1024 * 1024  # 100 MB per part


class ResumableMultipartUpload:
    """Multipart upload with checkpoint file for resume-on-failure."""

    def __init__(self, client: S3Client, bucket: str, key: str) -> None:
        self._client = client
        self._bucket = bucket
        self._key = key
        self._checkpoint_path = Path(f"/tmp/.multipart-{hashlib.md5(f'{bucket}/{key}'.encode()).hexdigest()}.json")  # noqa: S324

    def upload(self, local_path: str | Path) -> str:
        """Upload a large file with resume capability. Returns the ETag."""
        local_path = Path(local_path)
        file_size = local_path.stat().st_size

        checkpoint = self._load_checkpoint()
        upload_id = checkpoint.get("upload_id")
        completed_parts: list[dict[str, Any]] = checkpoint.get("parts", [])
        start_part = len(completed_parts) + 1

        if not upload_id:
            response = self._client.create_multipart_upload(
                Bucket=self._bucket,
                Key=self._key,
            )
            upload_id = response["UploadId"]
            logger.info("Created multipart upload %s for %s", upload_id, self._key)

        try:
            with open(local_path, "rb") as fh:
                part_number = 1
                while True:
                    data = fh.read(PART_SIZE)
                    if not data:
                        break

                    if part_number < start_part:
                        part_number += 1
                        continue

                    response = self._client.upload_part(
                        Bucket=self._bucket,
                        Key=self._key,
                        PartNumber=part_number,
                        UploadId=upload_id,
                        Body=data,
                    )
                    completed_parts.append(
                        {"ETag": response["ETag"], "PartNumber": part_number}
                    )
                    self._save_checkpoint(upload_id, completed_parts)
                    logger.info("Uploaded part %d/%d", part_number, -(-file_size // PART_SIZE))
                    part_number += 1

            result = self._client.complete_multipart_upload(
                Bucket=self._bucket,
                Key=self._key,
                UploadId=upload_id,
                MultipartUpload={"Parts": completed_parts},
            )
            self._checkpoint_path.unlink(missing_ok=True)
            logger.info("Completed multipart upload for %s", self._key)
            return result["ETag"]

        except (ClientError, OSError) as exc:
            logger.error("Multipart upload failed at part %d: %s", part_number, exc)
            logger.info("Resume by calling upload() again -- checkpoint saved")
            raise

    def abort(self) -> None:
        """Abort an in-progress multipart upload and clean up."""
        checkpoint = self._load_checkpoint()
        upload_id = checkpoint.get("upload_id")
        if upload_id:
            self._client.abort_multipart_upload(
                Bucket=self._bucket,
                Key=self._key,
                UploadId=upload_id,
            )
            self._checkpoint_path.unlink(missing_ok=True)
            logger.info("Aborted multipart upload %s", upload_id)

    def _load_checkpoint(self) -> dict[str, Any]:
        if self._checkpoint_path.exists():
            return json.loads(self._checkpoint_path.read_text())
        return {}

    def _save_checkpoint(self, upload_id: str, parts: list[dict[str, Any]]) -> None:
        self._checkpoint_path.write_text(json.dumps({"upload_id": upload_id, "parts": parts}))
```

### Cleaning Up Incomplete Multipart Uploads

Incomplete multipart uploads consume storage and incur costs. Always configure a lifecycle rule to auto-abort them:

```python
def configure_abort_incomplete_multipart(client: S3Client, bucket: str, days: int = 7) -> None:
    """Add a lifecycle rule to abort incomplete multipart uploads after N days."""
    client.put_bucket_lifecycle_configuration(
        Bucket=bucket,
        LifecycleConfiguration={
            "Rules": [
                {
                    "ID": "AbortIncompleteMultipartUploads",
                    "Status": "Enabled",
                    "Filter": {"Prefix": ""},
                    "AbortIncompleteMultipartUpload": {"DaysAfterInitiation": days},
                }
            ]
        },
    )
```

---

## S3 Bucket Policies & CORS Configuration

### Bucket Policy Patterns

```python
from __future__ import annotations

import json
from typing import Any

from mypy_boto3_s3 import S3Client


def set_bucket_policy_read_only(
    client: S3Client,
    bucket: str,
    allowed_account_ids: list[str],
) -> None:
    """Apply a bucket policy allowing read-only access from specific AWS accounts."""
    policy: dict[str, Any] = {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "AllowCrossAccountRead",
                "Effect": "Allow",
                "Principal": {
                    "AWS": [f"arn:aws:iam::{acct}:root" for acct in allowed_account_ids]
                },
                "Action": ["s3:GetObject", "s3:ListBucket"],
                "Resource": [
                    f"arn:aws:s3:::{bucket}",
                    f"arn:aws:s3:::{bucket}/*",
                ],
            }
        ],
    }
    client.put_bucket_policy(Bucket=bucket, Policy=json.dumps(policy))


def enforce_ssl_only(client: S3Client, bucket: str) -> None:
    """Deny all requests that are not over HTTPS."""
    policy: dict[str, Any] = {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "DenyInsecureTransport",
                "Effect": "Deny",
                "Principal": "*",
                "Action": "s3:*",
                "Resource": [
                    f"arn:aws:s3:::{bucket}",
                    f"arn:aws:s3:::{bucket}/*",
                ],
                "Condition": {"Bool": {"aws:SecureTransport": "false"}},
            }
        ],
    }
    client.put_bucket_policy(Bucket=bucket, Policy=json.dumps(policy))


def enforce_encryption_at_rest(client: S3Client, bucket: str) -> None:
    """Deny PutObject requests that do not use server-side encryption."""
    policy: dict[str, Any] = {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "DenyUnencryptedUploads",
                "Effect": "Deny",
                "Principal": "*",
                "Action": "s3:PutObject",
                "Resource": f"arn:aws:s3:::{bucket}/*",
                "Condition": {
                    "StringNotEquals": {
                        "s3:x-amz-server-side-encryption": ["aws:kms", "AES256"]
                    }
                },
            }
        ],
    }
    client.put_bucket_policy(Bucket=bucket, Policy=json.dumps(policy))
```

### CORS Configuration

Required for browser-based direct-to-S3 uploads using presigned URLs or presigned POST:

```python
def configure_cors_for_uploads(
    client: S3Client,
    bucket: str,
    allowed_origins: list[str],
) -> None:
    """Configure CORS rules to allow browser-based uploads."""
    cors_config: dict[str, Any] = {
        "CORSRules": [
            {
                "AllowedHeaders": ["*"],
                "AllowedMethods": ["GET", "PUT", "POST", "HEAD"],
                "AllowedOrigins": allowed_origins,
                "ExposeHeaders": ["ETag", "x-amz-request-id", "x-amz-id-2"],
                "MaxAgeSeconds": 3600,
            }
        ]
    }
    client.put_bucket_cors(Bucket=bucket, CORSConfiguration=cors_config)
```

### Block Public Access

Always enable Block Public Access unless you explicitly need public objects (e.g., a static website bucket):

```python
def block_all_public_access(client: S3Client, bucket: str) -> None:
    """Enable all four Block Public Access settings."""
    client.put_public_access_block(
        Bucket=bucket,
        PublicAccessBlockConfiguration={
            "BlockPublicAcls": True,
            "IgnorePublicAcls": True,
            "BlockPublicPolicy": True,
            "RestrictPublicBuckets": True,
        },
    )
```

---

## Error Handling Patterns

### Structured Error Handling with ClientError

All AWS API errors surface as `botocore.exceptions.ClientError`. The response dict contains error code, message, and HTTP status.

```python
from __future__ import annotations

import logging
from typing import Any

from botocore.exceptions import (
    BotoCoreError,
    ClientError,
    ConnectionClosedError,
    EndpointConnectionError,
    NoCredentialsError,
    ParamValidationError,
)
from mypy_boto3_s3 import S3Client

logger = logging.getLogger(__name__)


class S3Error(Exception):
    """Base exception for S3 operations."""

    def __init__(self, message: str, key: str, error_code: str | None = None) -> None:
        super().__init__(message)
        self.key = key
        self.error_code = error_code


class S3NotFoundError(S3Error):
    """Object does not exist."""


class S3AccessDeniedError(S3Error):
    """Insufficient permissions."""


class S3BucketNotFoundError(S3Error):
    """Bucket does not exist."""


def safe_get_object(client: S3Client, bucket: str, key: str) -> dict[str, Any]:
    """Demonstrate structured error handling for S3 get_object."""
    try:
        return client.get_object(Bucket=bucket, Key=key)

    except ClientError as exc:
        error_code = exc.response["Error"]["Code"]
        error_message = exc.response["Error"]["Message"]
        http_status = exc.response["ResponseMetadata"]["HTTPStatusCode"]

        if error_code in ("NoSuchKey", "404"):
            raise S3NotFoundError(
                f"Object not found: s3://{bucket}/{key}",
                key=key,
                error_code=error_code,
            ) from exc
        elif error_code in ("AccessDenied", "403"):
            raise S3AccessDeniedError(
                f"Access denied for s3://{bucket}/{key}. Check IAM policy.",
                key=key,
                error_code=error_code,
            ) from exc
        elif error_code == "NoSuchBucket":
            raise S3BucketNotFoundError(
                f"Bucket does not exist: {bucket}",
                key=key,
                error_code=error_code,
            ) from exc
        elif error_code == "SlowDown":
            logger.warning("S3 rate limit hit for %s -- implement exponential backoff", key)
            raise
        else:
            logger.error(
                "Unexpected S3 error: code=%s status=%d message=%s",
                error_code,
                http_status,
                error_message,
            )
            raise

    except NoCredentialsError as exc:
        logger.critical("No AWS credentials found. Check environment or instance role.")
        raise

    except EndpointConnectionError as exc:
        logger.error("Cannot reach S3 endpoint. Check VPC endpoints or network config.")
        raise

    except ConnectionClosedError as exc:
        logger.warning("Connection closed by S3 -- retry with backoff")
        raise

    except ParamValidationError as exc:
        logger.error("Invalid parameters: %s", exc)
        raise

    except BotoCoreError as exc:
        logger.error("Unexpected botocore error: %s", exc)
        raise
```

### Common S3 Error Codes

| Error Code | HTTP Status | Meaning | Action |
|------------|-------------|---------|--------|
| `NoSuchKey` | 404 | Object does not exist | Check key spelling, prefix |
| `NoSuchBucket` | 404 | Bucket does not exist | Check bucket name, region |
| `AccessDenied` | 403 | IAM policy denies action | Audit IAM policies |
| `SlowDown` | 503 | Request rate too high | Exponential backoff |
| `EntityTooLarge` | 400 | PUT exceeds 5 GB | Use multipart upload |
| `InvalidBucketName` | 400 | Bucket name violates rules | Fix naming (lowercase, 3-63 chars) |
| `BucketAlreadyOwnedByYou` | 409 | Bucket exists in your account | Safe to ignore on create |
| `BucketAlreadyExists` | 409 | Globally unique name taken | Choose different name |
| `InvalidRange` | 416 | Range header exceeds object size | Check Content-Length first |
| `PreconditionFailed` | 412 | ETag / If-Match condition failed | Re-fetch ETag and retry |

### Retry Configuration

boto3 supports three retry modes:

- **legacy** (default): Limited retry logic, only retries throttling errors
- **standard**: Retries throttling + transient errors (timeouts, 5xx)
- **adaptive**: Standard + client-side rate limiting to avoid `SlowDown` errors

Always use `adaptive` mode in production:

```python
from botocore.config import Config

config = Config(
    retries={
        "max_attempts": 5,
        "mode": "adaptive",
    }
)
client = boto3.client("s3", config=config)
```

---

## S3 Event Notifications

### Lambda Trigger Configuration

S3 can invoke Lambda functions when objects are created, deleted, or restored. This is the foundation for event-driven architectures with S3.

```python
from __future__ import annotations

import json
from typing import Any

from mypy_boto3_s3 import S3Client


def configure_lambda_notification(
    client: S3Client,
    bucket: str,
    lambda_arn: str,
    events: list[str] | None = None,
    prefix: str = "",
    suffix: str = "",
) -> None:
    """Configure S3 event notification to trigger a Lambda function.

    Note: The Lambda function must have a resource-based policy allowing
    s3.amazonaws.com to invoke it.

    Args:
        client: S3 client.
        bucket: Bucket name.
        lambda_arn: ARN of the Lambda function.
        events: List of S3 event types. Defaults to object creation events.
        prefix: Filter by key prefix.
        suffix: Filter by key suffix (e.g., '.csv').
    """
    if events is None:
        events = ["s3:ObjectCreated:*"]

    filter_rules: list[dict[str, str]] = []
    if prefix:
        filter_rules.append({"Name": "prefix", "Value": prefix})
    if suffix:
        filter_rules.append({"Name": "suffix", "Value": suffix})

    notification_config: dict[str, Any] = {
        "LambdaFunctionConfigurations": [
            {
                "LambdaFunctionArn": lambda_arn,
                "Events": events,
                "Filter": {"Key": {"FilterRules": filter_rules}} if filter_rules else {},
            }
        ]
    }

    client.put_bucket_notification_configuration(
        Bucket=bucket,
        NotificationConfiguration=notification_config,
    )
```

### Lambda Handler for S3 Events

```python
from __future__ import annotations

import json
import logging
import urllib.parse
from typing import Any

import boto3

logger = logging.getLogger()
logger.setLevel(logging.INFO)

s3_client = boto3.client("s3")


def lambda_handler(event: dict[str, Any], context: Any) -> dict[str, Any]:
    """Process S3 event notifications in Lambda.

    The event contains a list of records, each representing a single S3 event.
    """
    processed: list[str] = []

    for record in event.get("Records", []):
        event_name: str = record["eventName"]
        bucket_name: str = record["s3"]["bucket"]["name"]
        # S3 event keys are URL-encoded
        object_key: str = urllib.parse.unquote_plus(record["s3"]["object"]["key"])
        object_size: int = record["s3"]["object"].get("size", 0)

        logger.info(
            "Processing event=%s bucket=%s key=%s size=%d",
            event_name,
            bucket_name,
            object_key,
            object_size,
        )

        if event_name.startswith("ObjectCreated"):
            response = s3_client.get_object(Bucket=bucket_name, Key=object_key)
            # Process the object content
            content_type = response["ContentType"]
            logger.info("Object content type: %s", content_type)
            # Always read and close the body to avoid connection leaks
            body = response["Body"].read()
            response["Body"].close()
            processed.append(object_key)

        elif event_name.startswith("ObjectRemoved"):
            logger.info("Object deleted: %s", object_key)

    return {"statusCode": 200, "processed": processed}
```

### SQS and SNS Destinations

For decoupled architectures or fan-out, route S3 events to SQS or SNS instead of directly to Lambda:

- **SQS**: Use when you need buffering, retry with dead-letter queues, or batch processing
- **SNS**: Use when you need to fan out a single S3 event to multiple subscribers
- **EventBridge**: Use when you need advanced filtering, routing to 20+ target services, or event replay

---

## Transfer Acceleration & Performance Optimization

### Transfer Acceleration

S3 Transfer Acceleration routes uploads through CloudFront edge locations, providing faster uploads for geographically distributed clients. It adds ~$0.04/GB on top of standard transfer costs.

```python
from __future__ import annotations

import boto3
from botocore.config import Config
from mypy_boto3_s3 import S3Client


def enable_transfer_acceleration(client: S3Client, bucket: str) -> None:
    """Enable Transfer Acceleration on a bucket."""
    client.put_bucket_accelerate_configuration(
        Bucket=bucket,
        AccelerateConfiguration={"Status": "Enabled"},
    )


def create_accelerated_client(region: str = "us-east-1") -> S3Client:
    """Create an S3 client that uses the Transfer Acceleration endpoint."""
    return boto3.client(
        "s3",
        region_name=region,
        config=Config(
            s3={"use_accelerate_endpoint": True},
            retries={"max_attempts": 3, "mode": "adaptive"},
        ),
    )
```

### Request Rate Optimization

S3 supports at least 3,500 PUT/COPY/POST/DELETE and 5,500 GET/HEAD requests per second per prefix. Strategies for high-throughput workloads:

1. **Use random key prefixes**: Distribute objects across partitions by adding a hash prefix (e.g., `abc123/data/file.csv` instead of `data/file.csv`)
2. **Use multiple prefixes**: Spread reads/writes across different prefixes to multiply throughput
3. **Use byte-range fetches**: Download large objects in parallel chunks using `Range` headers
4. **Use S3 Express One Zone**: For single-digit millisecond latency with directory bucket types

### Parallel Byte-Range Downloads

```python
from __future__ import annotations

import concurrent.futures
import logging
from pathlib import Path

import boto3
from botocore.config import Config
from mypy_boto3_s3 import S3Client

logger = logging.getLogger(__name__)

CHUNK_SIZE = 64 * 1024 * 1024  # 64 MB chunks


def download_with_byte_ranges(
    client: S3Client,
    bucket: str,
    key: str,
    local_path: str | Path,
    max_workers: int = 8,
) -> Path:
    """Download a large S3 object in parallel using byte-range requests."""
    local_path = Path(local_path)
    local_path.parent.mkdir(parents=True, exist_ok=True)

    head = client.head_object(Bucket=bucket, Key=key)
    total_size: int = head["ContentLength"]

    ranges: list[tuple[int, int]] = []
    start = 0
    while start < total_size:
        end = min(start + CHUNK_SIZE - 1, total_size - 1)
        ranges.append((start, end))
        start = end + 1

    # Pre-allocate the file
    with open(local_path, "wb") as fh:
        fh.truncate(total_size)

    def _download_range(byte_range: tuple[int, int]) -> None:
        range_start, range_end = byte_range
        thread_client = boto3.Session().client(
            "s3",
            config=Config(retries={"max_attempts": 3, "mode": "adaptive"}),
        )
        response = thread_client.get_object(
            Bucket=bucket,
            Key=key,
            Range=f"bytes={range_start}-{range_end}",
        )
        data = response["Body"].read()
        response["Body"].close()
        with open(local_path, "r+b") as fh:
            fh.seek(range_start)
            fh.write(data)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as pool:
        futures = [pool.submit(_download_range, r) for r in ranges]
        for future in concurrent.futures.as_completed(futures):
            future.result()  # Raise any exceptions

    logger.info("Downloaded %s (%d bytes) in %d chunks", key, total_size, len(ranges))
    return local_path
```

---

## S3 Select for Server-Side Querying

S3 Select lets you run SQL expressions against CSV, JSON, or Parquet objects directly on S3, returning only the matching data. This reduces data transfer costs and speeds up analytics for simple queries.

### Querying CSV Files

```python
from __future__ import annotations

import csv
import io
import logging
from typing import Any

from mypy_boto3_s3 import S3Client

logger = logging.getLogger(__name__)


def query_csv_with_s3_select(
    client: S3Client,
    bucket: str,
    key: str,
    sql_expression: str,
    has_header: bool = True,
    compression: str = "NONE",
) -> list[list[str]]:
    """Run a SQL query against a CSV file stored in S3.

    Args:
        client: S3 client.
        bucket: Bucket name.
        key: Object key (must be a CSV file).
        sql_expression: SQL expression (e.g., "SELECT s._1, s._2 FROM s3object s WHERE s._3 > '100'").
        has_header: Whether the CSV has a header row.
        compression: NONE, GZIP, or BZIP2.

    Returns:
        List of rows, each row being a list of string values.
    """
    input_serialization: dict[str, Any] = {
        "CSV": {
            "FileHeaderInfo": "USE" if has_header else "NONE",
            "RecordDelimiter": "\n",
            "FieldDelimiter": ",",
        },
        "CompressionType": compression,
    }

    response = client.select_object_content(
        Bucket=bucket,
        Key=key,
        ExpressionType="SQL",
        Expression=sql_expression,
        InputSerialization=input_serialization,
        OutputSerialization={"CSV": {}},
    )

    rows: list[list[str]] = []
    for event in response["Payload"]:
        if "Records" in event:
            payload = event["Records"]["Payload"].decode("utf-8")
            reader = csv.reader(io.StringIO(payload))
            rows.extend(list(reader))

    # Remove empty trailing rows
    rows = [row for row in rows if any(cell.strip() for cell in row)]
    logger.info("S3 Select returned %d rows from %s", len(rows), key)
    return rows


def query_json_with_s3_select(
    client: S3Client,
    bucket: str,
    key: str,
    sql_expression: str,
    json_type: str = "LINES",
) -> list[str]:
    """Run a SQL query against a JSON/JSONL file in S3.

    Args:
        client: S3 client.
        bucket: Bucket name.
        key: Object key.
        sql_expression: SQL expression (e.g., "SELECT s.name, s.age FROM s3object s WHERE s.age > 30").
        json_type: DOCUMENT (single JSON object) or LINES (newline-delimited JSON).

    Returns:
        List of JSON string results.
    """
    response = client.select_object_content(
        Bucket=bucket,
        Key=key,
        ExpressionType="SQL",
        Expression=sql_expression,
        InputSerialization={"JSON": {"Type": json_type}},
        OutputSerialization={"JSON": {"RecordDelimiter": "\n"}},
    )

    results: list[str] = []
    for event in response["Payload"]:
        if "Records" in event:
            payload = event["Records"]["Payload"].decode("utf-8")
            for line in payload.strip().split("\n"):
                if line.strip():
                    results.append(line.strip())

    logger.info("S3 Select returned %d records from %s", len(results), key)
    return results
```

### S3 Select Limitations

- Maximum input object size: 256 GB (uncompressed), 128 MB for Parquet
- No `JOIN`, `GROUP BY`, subqueries, or window functions
- No `ORDER BY` -- results are returned in storage order
- Only CSV, JSON, and Parquet input formats
- Consider Athena or Redshift Spectrum for complex queries

---

## Cost Optimization

### Storage Classes

| Class | Use Case | Retrieval Cost | Availability |
|-------|----------|---------------|--------------|
| STANDARD | Frequently accessed data | None | 99.99% |
| INTELLIGENT_TIERING | Unknown/changing access patterns | None (monitoring fee) | 99.9% |
| STANDARD_IA | Infrequent access, rapid retrieval | Per-GB retrieval fee | 99.9% |
| ONEZONE_IA | Reproducible infrequent data | Per-GB, single AZ risk | 99.5% |
| GLACIER_IR | Archive with millisecond access | Per-GB retrieval | 99.9% |
| GLACIER_FLEXIBLE_RETRIEVAL | Archive, minutes-to-hours retrieval | Per-GB + per-request | 99.99% |
| DEEP_ARCHIVE | Long-term archive, 12+ hour retrieval | Per-GB + per-request | 99.99% |

### Lifecycle Rules

```python
from __future__ import annotations

from typing import Any

from mypy_boto3_s3 import S3Client


def configure_lifecycle_rules(client: S3Client, bucket: str) -> None:
    """Configure production lifecycle rules for cost optimization."""
    rules: list[dict[str, Any]] = [
        {
            "ID": "TransitionToIA",
            "Status": "Enabled",
            "Filter": {"Prefix": "data/"},
            "Transitions": [
                {"Days": 30, "StorageClass": "STANDARD_IA"},
                {"Days": 90, "StorageClass": "GLACIER_IR"},
                {"Days": 365, "StorageClass": "DEEP_ARCHIVE"},
            ],
        },
        {
            "ID": "ExpireTempFiles",
            "Status": "Enabled",
            "Filter": {"Prefix": "tmp/"},
            "Expiration": {"Days": 7},
        },
        {
            "ID": "CleanupOldVersions",
            "Status": "Enabled",
            "Filter": {"Prefix": ""},
            "NoncurrentVersionTransitions": [
                {"NoncurrentDays": 30, "StorageClass": "STANDARD_IA"},
                {"NoncurrentDays": 90, "StorageClass": "GLACIER_IR"},
            ],
            "NoncurrentVersionExpiration": {"NoncurrentDays": 365},
        },
        {
            "ID": "AbortIncompleteMultipart",
            "Status": "Enabled",
            "Filter": {"Prefix": ""},
            "AbortIncompleteMultipartUpload": {"DaysAfterInitiation": 3},
        },
        {
            "ID": "DeleteExpiredMarkers",
            "Status": "Enabled",
            "Filter": {"Prefix": ""},
            "Expiration": {"ExpiredObjectDeleteMarker": True},
        },
    ]

    client.put_bucket_lifecycle_configuration(
        Bucket=bucket,
        LifecycleConfiguration={"Rules": rules},
    )
```

### Cost Optimization Checklist

- **Enable Intelligent-Tiering** for objects with unpredictable access patterns -- it has zero retrieval fees and automatically moves objects between tiers
- **Set lifecycle rules** to transition cold data to IA/Glacier classes
- **Abort incomplete multipart uploads** via lifecycle rules (they cost the same as stored objects)
- **Delete expired object versions** in versioned buckets
- **Use S3 Storage Lens** to analyze access patterns and identify cost savings
- **Use S3 Select** instead of downloading entire objects when you need a subset of data
- **Enable S3 Inventory** for auditing storage class distribution
- **Prefer regional endpoints** over transfer acceleration for intra-region traffic
- **Compress objects** before upload (gzip, zstd) to reduce storage and transfer costs
- **Set appropriate `Content-Encoding`** headers so clients can decompress automatically
- **Use S3 Batch Operations** for bulk transitions or deletions instead of per-object API calls

---

## Testing with moto & localstack

### Unit Testing with moto

moto is a library that mocks AWS services in-process. It is fast, requires no external dependencies, and supports most S3 operations. Use moto for unit tests where you need to verify your code's logic in isolation.

```python
from __future__ import annotations

import json
from typing import Any, Generator

import boto3
import pytest
from moto import mock_aws
from mypy_boto3_s3 import S3Client

BUCKET_NAME = "test-bucket"
REGION = "us-east-1"


@pytest.fixture
def s3_client() -> Generator[S3Client, None, None]:
    """Provide a mocked S3 client with a pre-created bucket."""
    with mock_aws():
        client: S3Client = boto3.client("s3", region_name=REGION)
        client.create_bucket(Bucket=BUCKET_NAME)
        yield client


class TestS3Service:
    """Tests for the S3Service class using moto mocks."""

    def test_upload_and_download(self, s3_client: S3Client, tmp_path: Any) -> None:
        """Verify round-trip upload and download."""
        # Arrange
        local_file = tmp_path / "test.txt"
        local_file.write_text("hello world")

        # Act -- upload
        s3_client.upload_file(str(local_file), BUCKET_NAME, "data/test.txt")

        # Act -- download
        download_path = tmp_path / "downloaded.txt"
        s3_client.download_file(BUCKET_NAME, "data/test.txt", str(download_path))

        # Assert
        assert download_path.read_text() == "hello world"

    def test_put_object_and_get_object(self, s3_client: S3Client) -> None:
        """Verify put_object and get_object for in-memory data."""
        content = b'{"event": "test", "value": 42}'
        s3_client.put_object(
            Bucket=BUCKET_NAME,
            Key="events/event.json",
            Body=content,
            ContentType="application/json",
        )

        response = s3_client.get_object(Bucket=BUCKET_NAME, Key="events/event.json")
        body = response["Body"].read()
        response["Body"].close()

        assert json.loads(body) == {"event": "test", "value": 42}
        assert response["ContentType"] == "application/json"

    def test_head_object_not_found(self, s3_client: S3Client) -> None:
        """Verify 404 behavior for non-existent objects."""
        from botocore.exceptions import ClientError

        with pytest.raises(ClientError) as exc_info:
            s3_client.head_object(Bucket=BUCKET_NAME, Key="does-not-exist.txt")

        assert exc_info.value.response["Error"]["Code"] == "404"

    def test_list_objects_with_prefix(self, s3_client: S3Client) -> None:
        """Verify listing objects under a specific prefix."""
        for i in range(5):
            s3_client.put_object(Bucket=BUCKET_NAME, Key=f"logs/2024/file-{i}.log", Body=b"data")
        s3_client.put_object(Bucket=BUCKET_NAME, Key="other/file.txt", Body=b"data")

        response = s3_client.list_objects_v2(Bucket=BUCKET_NAME, Prefix="logs/2024/")
        keys = [obj["Key"] for obj in response.get("Contents", [])]

        assert len(keys) == 5
        assert all(k.startswith("logs/2024/") for k in keys)

    def test_delete_objects_bulk(self, s3_client: S3Client) -> None:
        """Verify bulk deletion of objects."""
        keys = [f"bulk/file-{i}.txt" for i in range(10)]
        for key in keys:
            s3_client.put_object(Bucket=BUCKET_NAME, Key=key, Body=b"data")

        response = s3_client.delete_objects(
            Bucket=BUCKET_NAME,
            Delete={"Objects": [{"Key": k} for k in keys], "Quiet": True},
        )
        assert "Errors" not in response or len(response["Errors"]) == 0

        list_response = s3_client.list_objects_v2(Bucket=BUCKET_NAME, Prefix="bulk/")
        assert list_response.get("KeyCount", 0) == 0

    def test_presigned_url_generation(self, s3_client: S3Client) -> None:
        """Verify presigned URL is generated with correct structure."""
        s3_client.put_object(Bucket=BUCKET_NAME, Key="docs/readme.pdf", Body=b"pdf content")

        url = s3_client.generate_presigned_url(
            ClientMethod="get_object",
            Params={"Bucket": BUCKET_NAME, "Key": "docs/readme.pdf"},
            ExpiresIn=3600,
        )

        assert "docs/readme.pdf" in url
        assert "X-Amz-Expires=3600" in url or "Expires" in url

    def test_copy_object(self, s3_client: S3Client) -> None:
        """Verify copying objects within the same bucket."""
        s3_client.put_object(Bucket=BUCKET_NAME, Key="source/file.txt", Body=b"original")

        s3_client.copy_object(
            CopySource={"Bucket": BUCKET_NAME, "Key": "source/file.txt"},
            Bucket=BUCKET_NAME,
            Key="dest/file.txt",
        )

        response = s3_client.get_object(Bucket=BUCKET_NAME, Key="dest/file.txt")
        assert response["Body"].read() == b"original"
        response["Body"].close()


class TestS3BucketConfiguration:
    """Tests for bucket-level configuration."""

    def test_bucket_versioning(self, s3_client: S3Client) -> None:
        """Verify enabling bucket versioning."""
        s3_client.put_bucket_versioning(
            Bucket=BUCKET_NAME,
            VersioningConfiguration={"Status": "Enabled"},
        )

        response = s3_client.get_bucket_versioning(Bucket=BUCKET_NAME)
        assert response["Status"] == "Enabled"

    def test_bucket_encryption(self, s3_client: S3Client) -> None:
        """Verify default encryption configuration."""
        s3_client.put_bucket_encryption(
            Bucket=BUCKET_NAME,
            ServerSideEncryptionConfiguration={
                "Rules": [
                    {
                        "ApplyServerSideEncryptionByDefault": {
                            "SSEAlgorithm": "aws:kms",
                        },
                        "BucketKeyEnabled": True,
                    }
                ]
            },
        )

        response = s3_client.get_bucket_encryption(Bucket=BUCKET_NAME)
        rules = response["ServerSideEncryptionConfiguration"]["Rules"]
        assert rules[0]["ApplyServerSideEncryptionByDefault"]["SSEAlgorithm"] == "aws:kms"
```

### Integration Testing with LocalStack

LocalStack provides a fully functional local AWS cloud stack. Use it for integration tests that need to verify interactions across multiple AWS services (e.g., S3 events triggering Lambda functions).

```python
from __future__ import annotations

import os
from typing import Generator

import boto3
import pytest
from mypy_boto3_s3 import S3Client

LOCALSTACK_ENDPOINT = os.getenv("LOCALSTACK_ENDPOINT", "http://localhost:4566")
BUCKET_NAME = "integration-test-bucket"


@pytest.fixture(scope="session")
def localstack_s3_client() -> Generator[S3Client, None, None]:
    """Provide an S3 client connected to LocalStack."""
    client: S3Client = boto3.client(
        "s3",
        endpoint_url=LOCALSTACK_ENDPOINT,
        aws_access_key_id="test",
        aws_secret_access_key="test",
        region_name="us-east-1",
    )
    # Create bucket for the test session
    try:
        client.create_bucket(Bucket=BUCKET_NAME)
    except client.exceptions.BucketAlreadyOwnedByYou:
        pass

    yield client

    # Cleanup: delete all objects and the bucket
    paginator = client.get_paginator("list_objects_v2")
    for page in paginator.paginate(Bucket=BUCKET_NAME):
        objects = [{"Key": obj["Key"]} for obj in page.get("Contents", [])]
        if objects:
            client.delete_objects(Bucket=BUCKET_NAME, Delete={"Objects": objects})
    client.delete_bucket(Bucket=BUCKET_NAME)
```

### docker-compose for LocalStack

```yaml
# docker-compose.yml
services:
  localstack:
    image: localstack/localstack:latest
    ports:
      - "4566:4566"
    environment:
      - SERVICES=s3,lambda,sqs,sns
      - DEBUG=0
      - DEFAULT_REGION=us-east-1
    volumes:
      - localstack_data:/var/lib/localstack
      - /var/run/docker.sock:/var/run/docker.sock

volumes:
  localstack_data:
```

### Testing Tips

- **Use `mock_aws()` from moto v5+** instead of the deprecated individual decorators like `@mock_s3`
- **Scope fixtures appropriately**: Use `function` scope for isolation, `session` scope for expensive setup
- **Test error paths**: Verify your code handles `ClientError`, missing objects, and permission errors
- **Use `tmp_path` pytest fixture** for file-based tests -- pytest automatically cleans up temp directories
- **Mock time for presigned URL tests** when verifying expiration behavior
- **Test pagination** by inserting more objects than the default page size (1000)

---

## Best Practices

### Security
- **Never hardcode credentials** in source code. Use the credential chain (environment, profiles, instance roles).
- **Use IAM roles for EC2/ECS/Lambda** instead of long-lived access keys.
- **Enable S3 Block Public Access** on all buckets unless public access is an explicit requirement.
- **Enforce SSL-only** access via bucket policy.
- **Enable default encryption** (SSE-S3 or SSE-KMS) on all buckets.
- **Enable bucket versioning** for critical data to protect against accidental deletion.
- **Use VPC endpoints** for S3 access from within a VPC to avoid data traversing the public internet.
- **Set minimum TLS 1.2** in bucket policies for compliance.

### Performance
- **Reuse clients** across requests in the same thread. Creating a new client per request wastes connection pool resources.
- **Use adaptive retry mode** with 3-5 max attempts.
- **Set appropriate timeouts** -- 5s connect, 10-30s read for most operations.
- **Use `max_pool_connections`** to match your concurrency level (default is 10).
- **Use multipart uploads** for objects larger than 100 MB.
- **Use byte-range fetches** for parallel downloads of large objects.
- **Distribute key prefixes** for high-throughput workloads (3,500+ writes/s or 5,500+ reads/s per prefix).

### Reliability
- **Always handle `ClientError`** and check `response["Error"]["Code"]` for specific error handling.
- **Implement idempotent operations** -- S3 PUT is naturally idempotent, but design your application logic accordingly.
- **Use ETags for optimistic concurrency** with `If-Match` / `If-None-Match` headers.
- **Configure lifecycle rules** to abort incomplete multipart uploads.
- **Enable S3 Inventory** for large buckets to avoid expensive LIST operations.
- **Log the `x-amz-request-id`** from responses for AWS support troubleshooting.

### Code Quality
- **Use `mypy-boto3-s3`** type stubs for full type checking with mypy.
- **Inject the S3 client** as a dependency to enable testing with moto.
- **Wrap S3 operations** in a service class with structured error handling and logging.
- **Use `pathlib.Path`** for local file paths instead of raw strings.
- **Prefer `upload_file` / `download_file`** over raw `put_object` / `get_object` for file-based operations.

---

## Anti-Patterns

### 1. Creating a New Client Per Request

```python
# BAD -- creates a new HTTPS connection pool every call
def get_object_bad(bucket: str, key: str) -> bytes:
    client = boto3.client("s3")
    return client.get_object(Bucket=bucket, Key=key)["Body"].read()

# GOOD -- reuse the client
class S3Reader:
    def __init__(self) -> None:
        self._client = boto3.client("s3")

    def get_object(self, bucket: str, key: str) -> bytes:
        return self._client.get_object(Bucket=bucket, Key=key)["Body"].read()
```

### 2. Ignoring Pagination

```python
# BAD -- only returns first 1000 objects
def list_all_bad(client: S3Client, bucket: str) -> list[str]:
    response = client.list_objects_v2(Bucket=bucket)
    return [obj["Key"] for obj in response.get("Contents", [])]

# GOOD -- uses paginator for all objects
def list_all_good(client: S3Client, bucket: str) -> list[str]:
    paginator = client.get_paginator("list_objects_v2")
    keys: list[str] = []
    for page in paginator.paginate(Bucket=bucket):
        keys.extend(obj["Key"] for obj in page.get("Contents", []))
    return keys
```

### 3. Not Closing StreamingBody

```python
# BAD -- leaks the HTTP connection
def read_bad(client: S3Client, bucket: str, key: str) -> bytes:
    response = client.get_object(Bucket=bucket, Key=key)
    return response["Body"].read()
    # Body is never closed -- connection stays open

# GOOD -- explicitly close the body
def read_good(client: S3Client, bucket: str, key: str) -> bytes:
    response = client.get_object(Bucket=bucket, Key=key)
    try:
        return response["Body"].read()
    finally:
        response["Body"].close()
```

### 4. Using put_object for Large Files

```python
# BAD -- single PUT, fails over 5 GB, no retry per part
def upload_large_bad(client: S3Client, bucket: str, key: str, path: str) -> None:
    with open(path, "rb") as f:
        client.put_object(Bucket=bucket, Key=key, Body=f.read())

# GOOD -- managed transfer with automatic multipart
def upload_large_good(client: S3Client, bucket: str, key: str, path: str) -> None:
    client.upload_file(path, bucket, key)
```

### 5. Catching Bare Exceptions

```python
# BAD -- swallows all errors including KeyboardInterrupt
def exists_bad(client: S3Client, bucket: str, key: str) -> bool:
    try:
        client.head_object(Bucket=bucket, Key=key)
        return True
    except Exception:
        return False  # Could be a permissions error, not a 404

# GOOD -- check specific error code
def exists_good(client: S3Client, bucket: str, key: str) -> bool:
    try:
        client.head_object(Bucket=bucket, Key=key)
        return True
    except ClientError as exc:
        if exc.response["Error"]["Code"] == "404":
            return False
        raise  # Re-raise permission errors, throttling, etc.
```

### 6. Hardcoding Credentials

```python
# BAD -- credentials in source code
client = boto3.client(
    "s3",
    aws_access_key_id="AKIAIOSFODNN7EXAMPLE",
    aws_secret_access_key="wJalrXUtnFEMI/K7MDENG",
)

# GOOD -- rely on the credential chain
client = boto3.client("s3")
```

### 7. Not Using Server-Side Encryption

```python
# BAD -- object stored unencrypted
client.put_object(Bucket=bucket, Key=key, Body=data)

# GOOD -- enforce SSE-KMS encryption
client.put_object(
    Bucket=bucket,
    Key=key,
    Body=data,
    ServerSideEncryption="aws:kms",
    SSEKMSKeyId="alias/my-key",
)
```

### 8. Sharing boto3 Sessions Across Threads

```python
# BAD -- session is not thread-safe
session = boto3.Session()
client = session.client("s3")

def worker(key: str) -> None:
    client.get_object(Bucket="bucket", Key=key)  # Race condition

# GOOD -- each thread gets its own session and client
def worker(key: str) -> None:
    thread_session = boto3.Session()
    thread_client = thread_session.client("s3")
    thread_client.get_object(Bucket="bucket", Key=key)
```

---

## Sources & References

- [boto3 S3 Client API Reference](https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/s3.html) -- Complete API documentation for all S3 client operations, parameters, and response types.
- [AWS S3 Developer Guide -- Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance.html) -- Official AWS performance optimization guidance including request rate partitioning and Transfer Acceleration.
- [boto3 Credentials Configuration](https://boto3.amazonaws.com/v1/documentation/api/latest/guide/credentials.html) -- Detailed documentation of the credential resolution chain, session management, and AssumeRole patterns.
- [AWS S3 Pricing](https://aws.amazon.com/s3/pricing/) -- Current pricing for storage classes, request costs, data transfer, and optional features like Transfer Acceleration and S3 Select.
- [moto -- Mock AWS Services](https://docs.getmoto.org/en/latest/) -- Documentation for the moto library covering supported services, decorator usage, and testing patterns.
- [AWS S3 Security Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/security-best-practices.html) -- Official security guidance including Block Public Access, encryption, bucket policies, and VPC endpoints.
- [botocore Retries and Configuration](https://boto3.amazonaws.com/v1/documentation/api/latest/guide/retries.html) -- Documentation for retry modes (legacy, standard, adaptive) and client configuration options.
- [AWS S3 Event Notifications](https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventNotifications.html) -- Guide to configuring S3 event notifications with Lambda, SQS, SNS, and EventBridge destinations.
