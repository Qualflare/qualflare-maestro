# Configuration

Every option can be a flag or an environment variable. Precedence, highest first:
flag → environment variable → CI detection → git → default.

Reporter flags go **before** Maestro's arguments. The `--` form keeps the two apart:

```bash
qualflare-maestro -environment staging -- maestro test --env USER=ci .maestro/
```

| Flag | Environment variable | Default | Meaning |
|---|---|---|---|
| `-output-dir` | `QUALFLARE_OUTPUT_DIR` | `qualflare-results` | Where the report and its screenshots are written |
| `-environment` | `QUALFLARE_ENVIRONMENT` | `development` | Environment the run belongs to |
| `-language` | `QUALFLARE_LANGUAGE` | `en-US` | Report language |
| `-platform` | `QUALFLARE_PLATFORM` | detected | `ios`, `android` or `web`; detected from the device Maestro ran on |
| `-milestone` | `QUALFLARE_MILESTONE` | — | Milestone sequence number |
| `-branch` | `QUALFLARE_BRANCH` | CI, then git | Branch name |
| `-commit` | `QUALFLARE_COMMIT` | CI, then git | Commit SHA |
| `-run-id` | `QUALFLARE_RUN_ID` | CI run, else random | Groups one launch's report files; every shard must share it |
| `-shard-index` | `QUALFLARE_SHARD_INDEX` | — | Which shard produced these cases |
| `-enabled` | `QUALFLARE_ENABLED` | `true` | `false` runs Maestro exactly as given and writes no report |
| — | `QUALFLARE_MAESTRO_BIN` | `maestro` on `PATH` | The Maestro executable used by a bare `maestro` in the `--` form; a path ending in `maestro` (e.g. `/opt/maestro/bin/maestro`) in its place runs that binary instead |
| `-version` | — | — | Print the version and exit |

## Flags the reporter owns

`--format`, `--output`, `--debug-output` and `--flatten-debug-output` are set by the reporter,
because it reads Maestro's JUnit report and debug output from a place it controls. Passing any of
them exits with code `2`. Every other Maestro flag passes through unchanged.

## Exit codes

| Code | Meaning |
|---|---|
| Maestro's | Maestro ran; its own exit code is returned unchanged |
| `2` | A usage error, or one of the owned flags was passed |
| `127` | `maestro` could not be found |
| `1` | Maestro could not be started, or the report could not be written |

CI detection covers GitHub Actions, GitLab CI, CircleCI and Jenkins.
