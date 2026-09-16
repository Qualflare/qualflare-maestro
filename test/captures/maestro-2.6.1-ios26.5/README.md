# Real Maestro output, captured 2026-09-16

Produced by Maestro CLI **2.6.1** against the iOS Settings app on an **iPhone 17 simulator,
iOS 26.5**, from the flows in `flows/`, with exactly the flags the wrapper owns:

```bash
maestro test --format junit --output out/report.xml \
  --debug-output out/debug --flatten-debug-output \
  --env LABEL=General \
  flows/
```

Exit code was `1` (one flow fails on purpose). Everything below landed in `out/debug/` even
though `flows/config.yaml` sets `testOutputDir` — flattening overrides it.

Kept: `report.xml`, both `commands-(<flow>).json` files, the failure screenshot, the flows.
Not kept: `maestro.log` and `xctest_runner_*.log`, which contain absolute home-directory paths.

Worth knowing before using these as fixtures:

- `commands-(Settings opens).json` entry `#0 defineVariablesCommand` holds **real variable
  values** in its raw form — `LABEL: General` plus `MAESTRO_CLI_*` variables Maestro copied
  from the process environment. This is why the reporter drops that command entirely.
- `#3 assertConditionCommand` keeps `"textRegex": "${LABEL}"` in its raw form, which is what
  makes naming steps from the raw command safe.
- `#4 tapOnElement` is `WARNED` with `"duration": null` (an optional command that failed).
- The failed flow's error object carries a full `hierarchyRoot`, which is why that file is
  ~146 KB; only `message` is meant to reach a report.
- JUnit `time` values are rounded to whole seconds (`23.0`, `13.0`).
