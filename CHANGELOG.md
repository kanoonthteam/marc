# Changelog

## Unreleased

- Added GoReleaser config and GitHub Actions release workflow (T022). CGO arm64 cross-compile setup wired in for `marc-server` linux/arm64 builds.
- Added `make check-size` target and CI workflow that fails on PR if `marc` exceeds 15 MB or `marc-server` exceeds 35 MB. Guards against accidental client→server import leaks.
