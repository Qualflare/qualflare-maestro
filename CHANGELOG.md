# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Android runs reported `platform: ios`. Maestro names an Android device after its AVD or its adb
  serial, so the device string says nothing about the platform; the reporter now reads the platform
  from the per-flow `manifest.json`, where a logcat device log means Android and an xctest one means
  iOS. Measured on a real API 34 emulator, captured under
  `test/captures/maestro-2.10.0-android14/`.

### Added

- `.github/workflows/android-probe.yml`, a dispatch-only job that captures Maestro's Android output.

## [0.1.0] - 2026-09-19

Initial release.

### Added

- First release. Wraps `maestro test` and writes a native Qualflare report: a case per flow, a step
  per command named from the flow's YAML, nesting and screenshots linked to steps on Maestro 2.10+,
  metadata from `qualflare.*` properties, and an `[unattributed failure]` case when Maestro exits
  without results.
- Supports both Maestro debug-output layouts (2.6.x flat files and the 2.10+ per-flow bundle),
  chosen by what is on disk.
- Replaces `--env` and `MAESTRO_*` values with `${NAME}` in report text.
- Excludes Maestro's own settings and boolean values from redaction.
- Qualifies case ids only when the same flow runs in more than one suite, keeping `--shard-split`
  ids stable.
- Prunes `test/captures/` from the Go module with a nested `go.mod`: Maestro's failure screenshots
  are named with an emoji, which the module zip format rejects, breaking `go install`.
