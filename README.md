# qualflare-maestro

[![CI](https://github.com/Qualflare/qualflare-maestro/actions/workflows/ci.yml/badge.svg)](https://github.com/Qualflare/qualflare-maestro/actions/workflows/ci.yml)
[![Qualflare](https://api.qualflare.com/p/qualflare-maestro/badge.svg)](https://reports.qualflare.com/p/qualflare-maestro/launches)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

A native [Maestro](https://maestro.dev) reporter for [Qualflare](https://qualflare.com). It runs
`maestro test` and turns what Maestro leaves behind into a report with a step for every command,
screenshots tied to the step that took them, real tags, and Qualflare metadata written in the flow's
own YAML.

Without it, Maestro results reach Qualflare as JUnit XML: one line per flow, with a status, a
duration and a failure message. No steps and no screenshots. And when a run dies before reporting —
invalid YAML, no device — there is no file at all, so the failure never shows up.

The reporter makes **no network calls**. It writes a report directory, and
[`qualflare-cli`](https://github.com/Qualflare/qualflare-cli) uploads it.

## Install

```bash
brew install qualflare/tap/qualflare-maestro
# or
go install github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro@latest
```

Binaries for macOS, Linux and Windows are on the
[releases page](https://github.com/Qualflare/qualflare-maestro/releases).

Requires **Maestro 2.6.0 or newer**. Screenshots need `qf` **0.1.24 or newer** to upload.

## Quickstart

```bash
qualflare-maestro -- maestro test .maestro/
qf my-project collect ./qualflare-results
```

`qualflare-maestro .maestro/` is shorthand for the first line. Maestro's exit code is returned
unchanged, so the command drops into CI in place of `maestro test`:

```yaml
- name: Run Maestro flows
  run: qualflare-maestro -- maestro test .maestro/

- name: Upload results to Qualflare
  if: always()
  run: qf my-project collect ./qualflare-results
```

## Why it wraps Maestro instead of plugging in

Maestro has no reporter or listener API: the report formats are a fixed list, and nothing lets you
load code into a run. So the reporter runs `maestro test` itself, adds `--format junit`, `--output`,
`--debug-output` and `--flatten-debug-output` so Maestro's output lands somewhere it controls, and
reads it when Maestro exits. Passing one of those four flags yourself is an error; everything else
goes straight to Maestro.

## What ends up in the report

- **A case per flow**, identified by its path in your repository and its name.
- **A step per command**, named from the flow's YAML (`tapOn: Login`, `assertVisible: ${USER}`).
  An optional command that did not succeed is a skipped step. On Maestro 2.10+, commands inside
  `runFlow`, `repeat` and `retry` are nested under them.
- **Screenshots** Maestro takes of failed and warned steps on 2.10+, or failed steps on 2.6.x,
  attached to the step that took them.
- **Durations** measured from the commands themselves, not rounded to whole seconds.
- **Tags** from the flow's `tags`.
- **Infrastructure failures.** If Maestro exits without writing results — invalid YAML, no device —
  the report holds an error `[unattributed failure]` case with the end of Maestro's log, rather than
  nothing.

## Metadata from YAML

Flows cannot call an API, so Qualflare metadata goes in the flow's `properties`:

```yaml
appId: com.example.app
name: Checkout with a saved card
tags:
  - checkout
  - smoke
properties:
  qualflare.priority: high
  qualflare.description: Pays with the first saved card and checks the receipt.
  qualflare.link.issue: https://example.atlassian.net/browse/PAY-142
  qualflare.link.custom.runbook: https://wiki.example.com/payments
  qualflare.label.team: payments
  owner: checkout-squad
---
- launchApp
```

| Property | Becomes |
|---|---|
| `qualflare.priority` | priority: `low`, `medium`, `high` or `critical` |
| `qualflare.description` | description |
| `qualflare.link.<issue\|tms\|custom>[.<name>]` | a link, optionally named |
| `qualflare.label.<name>` | a label |
| anything else | a case property |

## Keeping secrets out

Step names come from the YAML as written, so `inputText: ${PASSWORD}` stays a placeholder. Maestro's
error messages do contain real values, so values passed with `--env` and `MAESTRO_*` environment
variables are also replaced by `${NAME}` wherever they appear in the report. See
[docs/LIMITATIONS.md](docs/LIMITATIONS.md) for what that does not cover.

## Configuration

Output directory, environment, platform and more: [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Test reports

This reporter is tested with itself. `e2e/` holds Maestro flows against the iOS Settings app — one
carrying Qualflare metadata in its YAML, one nesting commands under `repeat`, `retry` and `runFlow`,
and one with an optional command that misses — run through this reporter on a simulator and
uploaded to Qualflare on every merge to `main` by the **published** `qualflare-cli`. The results
below are that suite's, reported through the code this README documents:

[![Qualflare](https://api.qualflare.com/p/qualflare-maestro/banner.svg)](https://reports.qualflare.com/p/qualflare-maestro/launches)

Every flow there is meant to pass, so a red run is a real regression rather than a fixture failing
on purpose. Failing flows, invalid YAML and a missing device are covered in `test/integration`,
which is never uploaded.

## Known limitations

[docs/LIMITATIONS.md](docs/LIMITATIONS.md)

## Development

```bash
go test ./...
```

Tests run against real Maestro output committed under `test/captures/`. The tests that run Maestro
itself need macOS, Maestro and an iOS simulator:

```bash
go test -tags integration ./test/integration/ -run 'TestInvalidYAML|TestNoDevice' -v   # no simulator booted
go test -tags integration ./test/integration/ -run TestFlows -v                       # simulator booted
```

## License

Apache-2.0
