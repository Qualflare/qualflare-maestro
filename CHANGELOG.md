# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
