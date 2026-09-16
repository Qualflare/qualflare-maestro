# qualflare-maestro — design

**Date:** 2026-09-16 · **Status:** approved design, revised after measuring Maestro 2.10.0 ·
**Reporter:** 11th in the family

## Why

Maestro results reach Qualflare today only as JUnit XML (`qf collect report.xml --format maestro`).
That file carries one line per flow: a status, a duration, a failure message, and tags flattened
into a comma-separated property the CLI never turns into tags. No steps, no screenshots, no way to
see *which* command failed. And when a run dies before reporting — invalid YAML, no device, a
session error — Maestro exits `1` and writes no report at all, so the upload is empty or absent and
the failure is invisible.

`qualflare-maestro` closes that gap: steps for every command, accurate durations, screenshots tied
to the step that produced them, real tags, Qualflare metadata from YAML, and infrastructure failures
reported as failures.

## Measured facts this design rests on

Established from the `cli-2.6.1` and `cli-2.10.0` sources and confirmed by live runs on an iOS 26.5
simulator. Captures: `test/captures/maestro-2.6.1-ios26.5/` and
`test/captures/maestro-2.10.0-ios26.5/` (each has a README).

### True of both measured versions

1. **No extension point exists.** The reporter is chosen from a closed `ReportFormat` enum
   (`JUNIT`, `HTML`, `HTML_DETAILED`, `NOOP`); there is no ServiceLoader, reporter-class flag or
   plugin path. `onFlowComplete` is a list of YAML commands that never sees the result, and
   `runScript`/`evalScript` have no host file access. **So this reporter wraps the `maestro`
   process**, as `qualflare-go` wraps `go test`.
2. **`--debug-output X --flatten-debug-output` puts everything under `X/`,** and a `testOutputDir`
   in `config.yaml` does not redirect it.
3. **JUnit** (`--format junit --output F`): one `<testsuite>` (`name` is `Test Suite` unless
   `--test-suite-name`; `device="iPhone 17 - iOS 26.5 - <UDID>"`), one `<testcase>` per flow with
   `id`, `name`, `classname` (the flow name unless `junitId`/`junitClassname` properties are set),
   `file` (relative to the working directory), `time` (seconds), `status` (`SUCCESS`/`ERROR`),
   `<properties>` (every flow property including dotted keys, plus `tags` joined with `", "`), and
   `<failure>` with the failure message. `file` exists from **2.6.0**; tags from 2.2.0.
4. **Commands entries** have the shape `{command: {<unionKey>: {...}}, metadata: {status,
   timestamp, duration, error, sequenceNumber, evaluatedCommand, ...}}`. `timestamp` is epoch ms,
   `duration` is ms. Statuses: `PENDING RUNNING COMPLETED FAILED WARNED SKIPPED`.
5. **Raw commands keep placeholders.** `assertVisible: ${LABEL}` is recorded as
   `"textRegex": "${LABEL}"` even when `--env LABEL=…` is passed.
6. **`defineVariablesCommand` leaks values.** Maestro inserts it first in every flow, and its *raw*
   form holds real values: `--env` variables, `MAESTRO_*` variables copied from the process
   environment (in CI these can be credentials), and on 2.10.0 also `MAESTRO_FILENAME` and
   `MAESTRO_DEVICE_UDID`.
7. **Exit code `1` covers everything** — failed assertion, invalid YAML, no device, driver error.
   Only the presence of the JUnit file distinguishes "flows ran" from "the run never got that far".

### Two debug-output layouts

| | **Flat layout** — measured on 2.6.1 | **Bundle layout** — measured on 2.10.0 |
|---|---|---|
| Commands | `X/commands-[shard-<n>-](<flow>).json` | `X/<flow>/commands.json` |
| Screenshots | `X/screenshot-[shard-<n>-]<emoji>-<epochMs>-(<flow>).png`, failed step only | `X/<flow>/screenshots/step-NNN-<cmd>-<arg>.png`, failed **and** warned steps |
| Step → screenshot link | none; matched by flow name in the filename | `metadata.artifacts: [{type: "SCREENSHOT", path}]` on the step |
| Entry order | `IdentityHashMap` — must sort by `sequenceNumber` | execution order |
| Nesting | not recorded | `metadata.depth` (0 = top level) |
| `retry`/`repeat` | each attempt overwrites the command's metadata | each attempt is its own entry |
| `WARNED` duration | `null` | real value |
| `metadata.error` | whole Throwable, including `hierarchyRoot` (the view tree) | `message` and `debugMessage` only |
| JUnit `time` | rounded to whole seconds | millisecond precision; `timestamp` populated |
| Extra files | — | `X/<flow>/manifest.json` (file index only, no results), `logs/`, `screen-hierarchy/` |

The source could not settle which of 2.7–2.9 use which layout, and **the design does not depend on
it**: the reporter decides by what is on disk (see *Reading the debug directory*).

## Decisions

| Decision | Choice |
|---|---|
| Shape | Go wrapper binary in a new repo `Qualflare/qualflare-maestro` |
| Supported Maestro | **2.6.0 and newer**; both layouts |
| Retries | Not in v0.1 — no case-level `attempts`. Retry attempts inside a flow appear as steps where Maestro records them. |
| Step names | Built from the **raw** command, never `evaluatedCommand` |
| Metadata | YAML `tags` → tags; a reserved `qualflare.*` property namespace; other properties pass through |
| `WARNED` steps | Status `skipped`, error `optional command did not succeed` |
| Owned Maestro flags | Refused if the user passes them (exit 2), never silently overridden |

## Non-goals (v0.1)

Wrapper-level retries · reading artifacts from a run the wrapper didn't launch · Maestro Cloud results
· `--continuous` mode · HTML reports · video recordings · `screen-hierarchy/`, device logs and
`manifest.json` contents (nothing in them is needed).

## Command-line surface

```
qualflare-maestro [reporter flags] -- maestro test <maestro args...>
qualflare-maestro [reporter flags] <maestro test args...>     # shorthand: prepends `maestro test`
```

**Owned Maestro flags** — added by the wrapper; if any appear in the user's arguments it exits `2`
with an explanation: `--format`, `--output`, `--debug-output`, `--flatten-debug-output`. Every
other flag passes through unchanged (`--env`, `--config`, `--include-tags`, `--exclude-tags`,
`--shard-split`, `--shard-all`, `--test-suite-name`, `--test-output-dir`, and any flag not listed as
owned). A subcommand other than `test` exits `2`.

**Reporter flags and environment** — the same names and precedence as `qualflare-go`
(`internal/config/config.go`: flag → `QUALFLARE_*` env → CI detection → git → default):
`-output-dir`/`QUALFLARE_OUTPUT_DIR` (default `qualflare-results`), `-environment`, `-language`,
`-platform`, `-milestone`, `-branch`, `-commit`, `-run-id`, `-shard-index`, `-enabled`, `-version`.
`qualflare-go`'s `-framework` and `-repeat` are not carried over. `-platform` defaults to *detect*
rather than a fixed value. `QUALFLARE_MAESTRO_BIN` overrides which `maestro` is run.

Reporter flags are recognised only **before** the Maestro arguments: the first token that is `--` or
not a known reporter flag starts them. The `--` form is the documented one, since it avoids any
confusion with a Maestro flag of the same name.

**`-enabled false`** (or `QUALFLARE_ENABLED=false`) runs Maestro exactly as given — no owned flags
added, no report written — and returns its exit code. `qualflare-go` returns before running anything
when disabled; a wrapper doing that would silently skip the tests, so this reporter does not.

## Run lifecycle

1. Parse reporter flags; validate the Maestro arguments (owned flags, subcommand).
2. Resolve `maestro` (`QUALFLARE_MAESTRO_BIN`, else `PATH`). A bare `maestro` in the arguments uses
   that resolution; only an argument containing a path separator names a different binary. Missing → clear message, exit `127`, no
   report.
3. Create a private work dir `W` = `<outputDir>/.work-<runID>-<pid>/`.
4. Launch `maestro test <user args> --format junit --output W/report.xml --debug-output W/debug
   --flatten-debug-output`. Stdin inherited; stdout and stderr tee'd to the terminal, last 64 KiB of
   each kept.
5. Forward SIGINT and SIGTERM to the child; wait for it.
6. Build the report from `W/report.xml` and `W/debug/`.
7. Copy referenced screenshots to `<outputDir>/attachments/`, write
   `<outputDir>/qualflare-maestro-<pid>-<runID>.json`, remove `W`.
8. Exit with **Maestro's exit code**, unchanged. If writing the report fails, exit `1`.

## Reading the debug directory

**Layout detection** — if any `W/debug/*/commands.json` exists, the run used the bundle layout;
else if any `W/debug/commands-*.json` exists, the flat layout; else there are no commands (every
case keeps its JUnit data, with a warning).

**Matching a commands file to its flow** — both layouts, by the flow name stored *inside* the file:
the `applyConfigurationCommand` entry's `config.name`. File and folder names are not parsed, so
Maestro's filename sanitising never matters. If that entry has no `name`, fall back to the
file/folder name. If two flows in one run share a name, skip step enrichment for every flow with that
name and warn, rather than attach one flow's steps to another. This also covers sharded runs: with
`--shard-all` the same flow runs on several devices, and v0.1 attaches steps only when a name is
unambiguous. The flat layout's `shard-<n>-` prefix is known from source; how the bundle layout names
sharded folders has not been observed, so v0.1 does not parse shard numbers from either.

**Ordering** — sort entries by `metadata.sequenceNumber` in both layouts. It is the same as file
order in the bundle layout and required in the flat one.

## Report mapping

Wire contract: the family's native Collect format. `qualflare-go/internal/wire/wire.go` is ported as
the Go types. Durations are integer **nanoseconds**. Statuses come from
`passed failed skipped error timeout aborted`.

**Collect**

| Field | Value |
|---|---|
| `framework` | `maestro` |
| `platform` | `-platform` if given; else from `device`: contains `iOS` → `ios`, `Android` or `emulator` → `android`, a browser name → `web`; else the fallback below |
| `os` | the `device` string minus its trailing ` - <UDID>`, e.g. `iPhone 17 - iOS 26.5` |
| `metadata` | `version` (binary), `timestamp`, `cliName: "qualflare-maestro"`, `runId` |
| branch / commit / CI fields | `qualflare-go`'s `internal/gitdetect` and `internal/cidetect` |

If the platform can't be detected and `-platform` wasn't given, write the documented fallback `ios`
**and** print a warning naming `-platform`. The field is required on the wire, so there must be a
value; the warning keeps the fallback from being silent.

**Suite** — one per JUnit `<testsuite>`, category `e2e`. Name is the JUnit suite name, suffixed with
` (<os>)` when there is more than one suite (`--shard-all`).

**Case** — one per `<testcase>`.

| Field | Source |
|---|---|
| `id` | `<flowPath>#<flow name>`; with more than one suite (`--shard-all`), also `@<os>` so each device keeps its own history, and `@<os>#<k>` (k = the suite's 1-based position) when two suites share the same device name, so identical simulators never produce duplicate ids |
| `name` | flow name (`name` attribute) |
| `className` | `<flowPath>`, e.g. `.maestro/settings-opens.yaml` |
| `status` | `SUCCESS` → `passed`; `ERROR` → `failed`; `CANCELED`/`STOPPED` → `aborted` |
| `duration` | from commands: latest `timestamp + duration` minus earliest `timestamp`, in ns; JUnit `time` when no commands file matched |
| `startedAt` | earliest command `timestamp` as RFC 3339, else JUnit `timestamp` when present |
| `error` | `<failure>` text |
| `tags` | `tags` property split on `", "` |
| `priority` | `qualflare.priority` ∈ `low medium high critical`; anything else ignored with a warning |
| `description` | `qualflare.description` |
| `links` | `qualflare.link.<type>` (`issue`, `tms`, `custom`) = URL; `qualflare.link.<type>.<name>` = URL also sets the link name |
| `labels` | `qualflare.label.<name>` = value |
| `properties` | every other property except `tags`, `junitId`, `junitClassname` |
| `steps` | below |
| `attachments` | below |
| `attempts` | omitted |

`<flowPath>` is JUnit's `file` made relative to the **git repository root**
(`git rev-parse --show-toplevel`). JUnit's value depends on the working directory, so using it raw
would give the same flow a new `id` — and split its history — whenever CI runs from another folder.
Outside a git repository, `file` is used as written, with a warning.

**Steps**

- **Dropped entirely:** `defineVariablesCommand` and `applyConfigurationCommand`. Anything nested
  under a dropped entry is dropped too.
- Status: `COMPLETED` → `passed`; `FAILED` → `failed`; `WARNED` → `skipped` with error
  `optional command did not succeed`; `SKIPPED`/`PENDING` → `skipped`; `RUNNING` → `aborted`.
- Duration: `metadata.duration` ms → ns; `null` → `0`.
- Error: `metadata.error.message` only. Never `hierarchyRoot`, `debugMessage` or `stackTrace`.
- Name: a Maestro-YAML-like rendering of the **raw** command: `launchApp: com.apple.Preferences`,
  `assertVisible: ${LABEL}`, `tapOn: Definitely Not A Real Row (optional)`,
  `repeat: 2 times`, `retry: up to 1 retry`, `runFlow`. Common commands get specific renderings;
  the rest fall back to the union key with its `Command` suffix removed. Renderings never read
  `evaluatedCommand`.
- Nesting: in the bundle layout, `parentIndex` is the index of the nearest earlier kept step with
  `depth` one less. The flat layout has no depth, so its steps are all top level.
- Capped at **300** per case (`MaxStepsPerTestAttempt`); the rest are dropped with one warning per
  case.

**Attachments** — screenshots copied to `attachments/<runID>-<pid>-<n>.png` (the pid keeps two
reporter runs that share a run id and output directory — normal in CI — from overwriting each
other's screenshots), referenced by
`localImagePath` relative to the report file, `mimeType: image/png`, at most 50 per case. The
attachment `name` is that copied file's name, never Maestro's own screenshot file name, which
embeds the command's argument and could carry a variable's value. Needs `qf`
CLI ≥ 0.1.24.

- Bundle layout: each `metadata.artifacts` entry of `type: "SCREENSHOT"` on a kept step, resolved
  against that flow's folder, with `stepIndex` set to that step. Screenshots in `screenshots/` not
  referenced by any step (e.g. `final.png`) are attached without a `stepIndex`.
- Flat layout: `screenshot-*-(<flow>).png` files whose flow name matches the case. A `❌` screenshot
  gets the `stepIndex` of the case's first `failed` step; any other is attached without one.

## Keeping variable values out of the report

Step names come from raw commands, and `defineVariablesCommand` is dropped. That is not the whole
job, because **Maestro's error messages are evaluated**: a flow failing on `assertVisible: ${SECRET}`
produces JUnit `<failure>` text reading `Assertion is false: "<the real value>" is visible`, and
`maestro.log` records evaluated commands too.

So the reporter also **redacts known values from free text**. It knows two sources of values:

- `--env KEY=VALUE` (and `--env=KEY=VALUE`, `-e KEY=VALUE`) in the Maestro arguments;
- `MAESTRO_*` variables in the reporter's own process environment, which Maestro copies into flows.

Every occurrence of such a value is replaced by `${KEY}` in: case `error`, `description`, property
and label values, step `name` and `error`, and the unattributed-failure text (which is taken from
`maestro.log` or stderr). Longer values are replaced first. Values shorter than 4 characters are not
redacted, because replacing `1` or `on` everywhere would mangle the report; they are documented as not
protected. IDs, flow names and file paths are never rewritten, so redaction cannot split a flow's
history.

Values set any other way (a flow's own `env:` block, `evalScript`, an `.env` file loaded by a script)
are invisible to the wrapper and are documented as not protected.

## Failure handling

| Situation | Report | Exit |
|---|---|---|
| Owned flag passed, or subcommand isn't `test` | none | `2` |
| `maestro` not found | none | `127` |
| JUnit file parses and has ≥1 case | normal report | Maestro's |
| Maestro exits non-zero; JUnit missing, unparseable or empty | suite `[unattributed]`, case `[unattributed failure]`, status `error`, error = last 50 lines of `W/debug/maestro.log`, else the stderr tail, else a sentence naming the exit code | Maestro's |
| Maestro exits `0` with no cases | report with no suites, plus a warning (e.g. tag filters matched nothing) | `0` |
| Interrupted (signal forwarded) and JUnit was written | normal report | Maestro's |
| Interrupted and JUnit was not written | suite `[unattributed]`, case `[interrupted run]`, status `aborted` | Maestro's |
| Report write fails | none | `1` |

A missing or malformed commands file never fails the run: the case keeps its JUnit data and loses
only steps and screenshots, with a warning.

## Repository layout

Follows `qualflare-go`:

```
cmd/qualflare-maestro/main.go        entry, flags, exit codes
internal/config/                     flags + env      (ported, trimmed)
internal/args/                       Maestro argument validation, flag injection, --env values
internal/redact/                     replaces known variable values with ${KEY}
internal/runner/                     child process, tee, signal forwarding
internal/junit/                      JUnit reader
internal/debugdir/                   layout detection, commands reading, flow matching
internal/steps/                      entry → step conversion, rendering, nesting
internal/build/                      assembles the Collect report
internal/wire/, internal/gitdetect/, internal/cidetect/,
internal/version/, internal/textutil/, internal/constants/   ported from qualflare-go
test/captures/                       real Maestro output per version + platform
test/integration/                    awkward flows, never uploaded, plus a verifier
e2e/                                 always-green dogfood flows, verifier, assert-launch-landed.py
docs/CONFIGURATION.md, docs/LIMITATIONS.md
.github/workflows/{ci,e2e,release}.yml, .goreleaser.yml
README.md, CHANGELOG.md, RELEASING.md, LICENSE (Apache-2.0)
```

No dependencies beyond the Go standard library, as with `qualflare-go`.

## Testing

- **Unit** — every internal package, driven by the committed captures rather than hand-written
  Maestro output. Explicit tests, run against **both** captures: `defineVariablesCommand` is absent
  from steps and none of its values (`General`, any `MAESTRO_` key's value) appear anywhere in the
  serialized report; `${LABEL}` survives in a step name; `WARNED` becomes `skipped`; nothing from
  `hierarchyRoot` is serialized; duration comes from timestamps; bundle-layout `repeat` children get
  `parentIndex`; a bundle screenshot's `stepIndex` points at the step that lists it; duplicate flow
  names skip enrichment; owned flags are refused.
- **Integration** (`test/integration/`, never uploaded) — flows that fail, use `optional`, nest via
  `runFlow`/`repeat`/`retry`, contain invalid YAML, and a run with no device, all against the iOS
  Settings app (`com.apple.Preferences`) so no app is built. A verifier asserts report shape, the
  unattributed failure, the secrets guarantee and exit-code passthrough.
- **Dogfood** (`e2e/`) — always-green flows against Settings, uploaded on every merge to `main` to a
  public `qualflare-maestro` project by the published `qf`, gated by a verifier and
  `assert-launch-landed.py`, as in `qualflare-go` and `qualflare-testng`.
- **CI** — unit tests on Linux. Integration and dogfood on a macOS runner with an iOS simulator, on a
  Maestro matrix of **2.6.1** (flat layout) and **the latest release** (bundle layout).

## Distribution

goreleaser: Linux/macOS/Windows × amd64/arm64 archives, checksums, SBOM, cosign signatures, GitHub
release, Homebrew cask `qualflare/tap/qualflare-maestro`, `go install`. Version from the git tag,
verified by the release workflow before publishing, as in `qualflare-go`.

## Risks

- **Android** — only iOS has been observed. The first Android capture goes under `test/captures/`
  before platform detection or screenshots are trusted for it.
- **Layout drift** — Maestro releases every few weeks and changed its layout once already. Layout
  detection plus the latest-release CI job is what catches the next change; a run whose debug
  directory matches neither layout still produces a correct case-level report, with a warning.
- **Secrets** — the guarantee is "no `--env` or `MAESTRO_*` value of 4+ characters appears anywhere
  in the report", enforced by redaction (above). The integration suite fails a flow on a secret
  selector on purpose and checks the written report end to end, so a leak path redaction misses is
  caught rather than assumed away.
- **`junitId`** — the case `id` stays `<flowPath>#<name>`. A user's `junitId` is a JUnit concern, so
  changing it never splits a flow's history.
- **macOS CI cost** — iOS simulator jobs are slow; the dogfood suite stays small for that reason.

## Follow-ups outside this repo

- `landing-fe/src/pages/maestro-test-reporting.astro` — lead with the reporter; correct the claim
  that a Maestro-looking filename "may suffice" for detection (content detection classifies the file
  as generic JUnit first).
- `qualflare-cli/internal/core/services/report_service_parse_test.go:598` — stale "sibling
  commands.json" comment.
