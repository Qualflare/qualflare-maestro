# qualflare-maestro — design

**Date:** 2026-09-16 · **Status:** approved design, pre-plan · **Reporter:** 11th in the family

## Why

Maestro results reach Qualflare today only as JUnit XML (`qf collect report.xml --format maestro`).
That file carries one line per flow: a status, a duration rounded to whole seconds, a failure
message, and tags flattened into a comma-separated property the CLI never turns into tags. No
steps, no screenshots, no way to see *which* command failed. And when a run dies before reporting —
invalid YAML, no device, a session error — Maestro exits `1` and writes no report at all, so the
upload is empty or absent and the failure is invisible.

`qualflare-maestro` closes that gap: steps for every command, accurate durations, failure
screenshots, real tags, Qualflare metadata from YAML, and infrastructure failures reported as
failures.

## Measured facts this design rests on

Established by reading the `cli-2.6.1` source (commit `00f5167`) and confirmed by a live run on an
iOS 26.5 simulator. Captures are in `test/captures/maestro-2.6.1-ios26.5/`.

1. **No extension point exists.** `TestSuiteReporter` is internal and chosen by a closed
   `ReportFormat` enum (`JUNIT`, `HTML`, `HTML_DETAILED`, `NOOP`). No ServiceLoader, no reporter-class
   flag, no plugin path. `onFlowComplete` is a list of YAML commands and never sees the result;
   `runScript`/`evalScript` run in a Graal context with no host file access. **So this reporter must
   wrap the `maestro` process**, as `qualflare-go` wraps `go test`.
2. **One flag combination makes artifact locations deterministic.** With
   `--debug-output X --flatten-debug-output`, every `commands-(<flow>).json`, every
   `screenshot-<emoji>-<epochMs>-(<flow>).png` and `maestro.log` land directly in `X/`, and a
   `testOutputDir` in `config.yaml` is ignored for them. (Confirmed live.) Shards prefix names with
   `shard-<n>-`. Slashes in flow names become `_`.
3. **JUnit** (`--format junit --output F`) has one `<testsuite>` (name `Test Suite` unless
   `--test-suite-name`; attribute `device="iPhone 17 - iOS 26.5 - <UDID>"`) and one `<testcase>`
   per flow with `id`, `name`, `classname` (all the flow name unless `junitId`/`junitClassname`
   properties are set), `file` (path relative to the working directory), `time` (seconds, **rounded
   to whole seconds**), `status` (`SUCCESS`/`ERROR`), `<properties>` (every flow property, including
   dotted keys, plus `tags` joined with `", "`), and `<failure>` holding the failure message.
4. **`commands-(<flow>).json`** is an array of `{command: {<unionKey>: {...}}, metadata: {status,
   timestamp, duration, error, sequenceNumber, evaluatedCommand}}`. Order entries by
   `sequenceNumber`, not array position. `timestamp` is epoch ms, `duration` is ms and **may be
   null** (seen on `WARNED`). Command statuses: `PENDING RUNNING COMPLETED FAILED WARNED SKIPPED`.
   `error` is a serialized Throwable (`message`, `debugMessage`, `localizedMessage`, `stackTrace`,
   `hierarchyRoot` — the last is the full view tree).
5. **Raw commands keep placeholders.** The raw `command` of `assertVisible: ${LABEL}` records
   `"textRegex": "${LABEL}"` even when `--env LABEL=…` is passed.
6. **`defineVariablesCommand` leaks values.** Maestro inserts it into every flow; its *raw* form
   holds real values of `--env` variables **and `MAESTRO_*` variables copied from the process
   environment** (seen: `MAESTRO_CLI_NO_ANALYTICS`). In CI that can include credentials.
7. **No retry history exists locally.** There is no flow-level retry for local runs; the YAML
   `retry` command overwrites a command's metadata on each attempt.
8. **Exit code `1` covers everything** — failed assertion, invalid YAML, no device, driver error.
   Only the presence of the JUnit file distinguishes "flows ran" from "run never got that far".

## Decisions

| Decision | Choice |
|---|---|
| Shape | Go wrapper binary in a new repo `Qualflare/qualflare-maestro` |
| Retries | Not in v0.1 — report what Maestro ran, once. `--retries` is a 0.2 candidate. |
| Step names | Built from the **raw** command, never `evaluatedCommand` |
| Metadata | YAML `tags` → tags; a reserved `qualflare.*` property namespace; other properties pass through |
| `WARNED` steps | Status `skipped`, error `optional command did not succeed` |
| Owned Maestro flags | Refused if the user passes them (exit 2), never silently overridden |

## Non-goals (v0.1)

Wrapper-level retries · Android-specific enrichment beyond platform detection · reading artifacts
from a run the wrapper didn't launch · Maestro Cloud results · `--continuous` mode (Maestro writes no
per-flow commands file there) · HTML report generation · video recordings.

## Command-line surface

```
qualflare-maestro [reporter flags] -- maestro test <maestro args...>
qualflare-maestro [reporter flags] <maestro test args...>     # shorthand: prepends `maestro test`
```

**Owned Maestro flags** — the wrapper adds these itself and exits `2` with an explanation if any
appear in the user's arguments: `--format`, `--output`, `--debug-output`,
`--flatten-debug-output`. Every other Maestro flag passes through unchanged (`--env`, `--config`,
`--include-tags`, `--exclude-tags`, `--shard-split`, `--shard-all`, `--test-suite-name`,
`--test-output-dir`, and any flag not listed above as owned). A subcommand other than `test` exits `2`.

**Reporter flags and environment** — mirror `qualflare-go`'s names so the family stays consistent:
output directory (default `qualflare-results`, `QUALFLARE_OUTPUT_DIR`), environment name, run id,
shard index, enable/disable switch, plus `--platform ios|android|web` to override detection.
Exact names are fixed in the plan by reading `qualflare-go/internal/config` and
`docs/CONFIGURATION.md`.

## Run lifecycle

1. Parse reporter flags; validate the Maestro argument list (owned flags, subcommand).
2. Resolve `maestro` on `PATH` (or `QUALFLARE_MAESTRO_BIN`). Missing → clear message, exit `127`,
   no report.
3. Create a private work dir `W` inside the output directory (`.work-<runID>/`).
4. Launch `maestro test <user args> --format junit --output W/report.xml --debug-output W/debug
   --flatten-debug-output`. Stdin inherited; stdout and stderr tee'd to the terminal with the last
   64 KiB of each kept as evidence.
5. Forward SIGINT and SIGTERM to the child; wait for it.
6. Build the report from `W/report.xml`, `W/debug/commands-*.json`, `W/debug/screenshot-*.png`,
   `W/debug/maestro.log`.
7. Copy referenced screenshots into `<outputDir>/attachments/`, write
   `<outputDir>/qualflare-maestro-<pid>-<runID>.json`, remove `W`.
8. Exit with **Maestro's exit code**, unchanged. If writing the report fails, exit `1`.

## Report mapping

Wire contract: the family's native Collect format (`frameworks/qualflare-playwright/src/shared/types.ts`
is the reference). Durations are integer **nanoseconds**. Statuses are drawn from
`passed failed skipped error timeout aborted`.

**Collect**

| Field | Value |
|---|---|
| `framework` | `maestro` |
| `platform` | `--platform` if given; else from `device`: contains `iOS` → `ios`, `Android` or `emulator` → `android`, a browser name → `web`; else the fallback below |
| `os` | the `device` string minus the trailing UDID, e.g. `iPhone 17 - iOS 26.5` |
| `metadata` | `version` (from the binary), `timestamp`, `cliName: "qualflare-maestro"`, `runId` |
| branch / commit / CI fields | same detection as `qualflare-go` (`internal/gitdetect`, `internal/cidetect`) |

If the platform can't be detected and `--platform` wasn't given, write the documented fallback `ios`
**and** print a warning naming `--platform`. The field is required on the wire, so there must be a
value; the warning is what keeps the fallback from being silent.

**Suite** — one per JUnit `<testsuite>`. Name is the JUnit suite name, suffixed with the device when
there is more than one suite (`--shard-all`).

**Case** — one per `<testcase>`.

| Field | Source |
|---|---|
| `id` | `<flowPath>#<flow name>`, plus `@shard-<n>` when sharded — stable across runs, unique across files |
| `name` | flow name (`name` attribute) |
| `className` | `<flowPath>`, e.g. `.maestro/settings-opens.yaml` |

`<flowPath>` is the JUnit `file` attribute made relative to the **git repository root** (found with
the same detection `qualflare-go` uses), not to the working directory Maestro happened to run in.
JUnit's own value depends on the working directory, so using it raw would give the same flow a
different `id` — and split its history — whenever CI runs from another folder. Outside a git
repository, the attribute is used as written, with a warning.
| `status` | `SUCCESS` → `passed`; `ERROR` → `failed`; `CANCELED`/`STOPPED` → `aborted` |
| `duration` | from commands: `max(timestamp + duration)` − `min(timestamp)`, in ns; JUnit `time` only when no commands file matched |
| `error` | `<failure>` text |
| `tags` | `tags` property split on `", "` |
| `priority` | `qualflare.priority` ∈ `low medium high critical`; anything else ignored with a warning |
| `description` | `qualflare.description` |
| `links` | `qualflare.link.<type>` (`issue`, `tms`, `custom`) = URL; `qualflare.link.<type>.<name>` = URL sets the link name |
| `labels` | `qualflare.label.<name>` = value |
| `properties` | every other property except `tags`, `junitId`, `junitClassname` |
| `steps` | see below |
| `attachments` | see below |
| `attempts` | omitted |

**Joining JUnit to commands.** Key: the flow name with `/` replaced by `_`, plus the shard prefix. If
two flows in one run share a name, Maestro wrote both to one file — skip step enrichment for every
flow with that name and warn, rather than attach one flow's steps to another.

**Steps**

- Entries ordered by `metadata.sequenceNumber`.
- **Dropped entirely:** `defineVariablesCommand`, `applyConfigurationCommand`.
- Status: `COMPLETED` → `passed`; `FAILED` → `failed`; `WARNED` → `skipped` with error
  `optional command did not succeed`; `SKIPPED`/`PENDING` → `skipped`; `RUNNING` → `aborted`.
- Duration: `metadata.duration` ms → ns; `null` → `0`.
- Error: `metadata.error.message` only. Never `hierarchyRoot`, `debugMessage` or `stackTrace`.
- Name: a Maestro-YAML-like rendering of the **raw** command — `launchApp: com.apple.Preferences`,
  `assertVisible: ${LABEL}`, `tapOn: "Definitely Not A Real Row" (optional)`. The plan defines
  renderings for the common commands and a generic fallback (the union key with `Command` stripped)
  for the rest. Renderings never read `evaluatedCommand`.
- Nesting (`runFlow`, `repeat`, `retry`): use `parentIndex` if parentage can be derived reliably from
  the file; otherwise flat in sequence order. Resolved by a live capture in the plan, not guessed.
- Capped at **300** per case, matching the family; overflow dropped with one warning per case.

**Attachments** — screenshots whose `(<flow>)` suffix matches the case: copied to
`attachments/<runID>-<n>.png`, referenced by `localImagePath` (report-relative), `mimeType
image/png`. For a `❌` screenshot, `stepIndex` points at the case's `failed` step. Any other
screenshot (`✅`, `⚠️`, or one taken by the flow's own `takeScreenshot`) is attached without a
`stepIndex`. The live 2.6.1 run produced only a `❌` screenshot — the suite runner takes none for an
optional command that fails — so warning screenshots are handled if they appear, not relied on.
At most 50 per case. Requires `qf` CLI ≥ 0.1.24.

## Failure handling

| Situation | Report | Exit |
|---|---|---|
| Owned flag passed, or subcommand isn't `test` | none | `2` |
| `maestro` not found | none | `127` |
| Maestro exits `0` or `1` with a parseable JUnit file with ≥1 case | normal report | Maestro's |
| Maestro exits non-zero, JUnit missing/unparseable/empty | suite `[unattributed]`, case `[unattributed failure]`, status `error`, error = last 50 lines of `maestro.log`, else stderr tail, else a sentence naming the exit code | Maestro's |
| Maestro exits `0`, no cases | empty report plus a warning (e.g. tag filters matched nothing) | `0` |
| Interrupted (SIGINT/SIGTERM forwarded) and JUnit was written | normal report | Maestro's |
| Interrupted and JUnit was not written | suite `[unattributed]`, case `[interrupted run]`, status `aborted` | Maestro's |
| Report write fails | none | `1` |

Malformed or missing `commands-*.json` never fails the run: the case keeps its JUnit data and loses
only steps, with a warning.

## Repository layout

Follows `qualflare-go`:

```
cmd/qualflare-maestro/main.go        entry, arg handling, exit codes
internal/config/                     flags + env
internal/args/                       Maestro argument validation and flag injection
internal/runner/                     child process, tee, signal forwarding
internal/junit/                      JUnit reader
internal/commands/                   commands-*.json reader + step rendering
internal/screenshots/                discovery and matching
internal/build/                      assembles the Collect report
internal/wire/                       wire types
internal/gitdetect/, internal/cidetect/, internal/version/   ported from qualflare-go
test/captures/                       real Maestro output, per Maestro version + platform
test/integration/fixtures/           awkward flows, never uploaded
e2e/                                 always-green dogfood flows + verifier + assert-launch-landed.py
docs/CONFIGURATION.md, docs/LIMITATIONS.md
.github/workflows/{ci,e2e,release}.yml, .goreleaser.yml
README.md, CHANGELOG.md, RELEASING.md, LICENSE (Apache-2.0)
```

No runtime dependencies beyond the Go standard library, as with `qualflare-go`.

## Testing

- **Unit** — every internal package, driven by the committed captures, never hand-written Maestro
  output. Explicit tests for: `defineVariablesCommand` absent from steps and its values absent from
  the entire report (search the serialized JSON for `General` and every `MAESTRO_` key); `${LABEL}`
  preserved in a step name; `WARNED` with `null` duration; `hierarchyRoot` never serialized; duration
  from timestamps not JUnit; duplicate flow names skip enrichment; owned-flag refusal.
- **Integration fixture** (`test/integration/fixtures/`, never uploaded) — flows that fail, use
  `optional`, nest via `runFlow`/`repeat`/`retry`, contain invalid YAML, and a run with no device.
  Driven against the iOS Settings app (`com.apple.Preferences`) so no test app is built. A verifier
  asserts the report shape, the unattributed failure, the secrets guarantee and exit-code passthrough.
- **Dogfood** (`e2e/`) — always-green flows against Settings, uploaded on every merge to `main` to a
  public `qualflare-maestro` project by the published `qf`, gated by a verifier and
  `assert-launch-landed.py` exactly as `qualflare-go` and `qualflare-testng` do.
- **CI** — unit tests on Linux; integration and dogfood on a macOS runner with an iOS simulator.
  Maestro version matrix: the floor and the current release.

## Maestro version floor

Every owned flag, the `commands-(<flow>).json` naming and the `defineVariablesCommand` behaviour must
hold. The plan measures an older release to set the floor rather than assuming 2.6.1 behaviour holds
backwards, and records a capture for it under `test/captures/`.

## Distribution

goreleaser: Linux/macOS/Windows × amd64/arm64 archives, checksums, SBOM, cosign signatures, GitHub
release, Homebrew cask `qualflare/tap/qualflare-maestro`, `go install`. Version from the git tag,
verified by the release workflow before publishing — same as `qualflare-go`.

## Risks and open items for the plan

- **Nesting** — whether parentage of `runFlow`/`repeat`/`retry` children is recoverable from
  `commands-*.json`. Capture first; flat steps are the fallback.
- **Android parity** — only iOS has been observed. The first Android capture may change platform
  detection and screenshot behaviour; record it under `test/captures/` before relying on it.
- **Raw-command secrets beyond `defineVariables`** — the guarantee is "no `--env` or `MAESTRO_*`
  value appears anywhere in the report". The integration verifier enforces it end to end, so a
  command type that inlines evaluated values is caught rather than assumed away.
- **Case identity when `junitId` is set** — the `id` stays `<flowPath>#<name>`; a user's `junitId` is a
  JUnit concern and is not used, so renaming a flow's `junitId` never splits its history.
- **macOS CI cost** — iOS simulator jobs are slow; the dogfood suite stays small (a handful of
  flows) for that reason.

## Follow-ups outside this repo

- `landing-fe/src/pages/maestro-test-reporting.astro` — lead with the reporter; correct the existing
  claim that a Maestro-looking filename "may suffice" for detection (content detection classifies it
  as generic JUnit first).
- `qualflare-cli/internal/core/services/report_service_parse_test.go:598` — stale "sibling
  commands.json" comment.
