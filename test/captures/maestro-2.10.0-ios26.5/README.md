# Real Maestro output, captured 2026-09-16

Maestro CLI **2.10.0** (official `cli-2.10.0` release zip) against the iOS Settings app on an
**iPhone 17 simulator, iOS 26.5**, with the flags the wrapper owns:

```bash
maestro test --format junit --output out/report.xml \
  --debug-output out/debug --flatten-debug-output \
  --env LABEL=General \
  flows/
```

Exit code `1` (one flow fails on purpose). Same flows as the 2.6.1 capture, plus
`settings-nested.yaml` (`repeat`, `retry`, `runFlow`).

This is the **per-flow bundle layout** — the one to compare against
`../maestro-2.6.1-ios26.5/`, which is the older flat-file layout:

```
debug/<flow name>/commands.json
debug/<flow name>/manifest.json          index of files only, no results ($schema artifact-manifest/v1)
debug/<flow name>/screenshots/step-NNN-<command>-<arg>.png
debug/<flow name>/logs/…                 not kept (device logs are MBs; maestro.log has home paths)
debug/<flow name>/screen-hierarchy/…     not kept (full view trees)
debug/maestro.log                        not kept
```

What differs from 2.6.1, all visible in these files:

- `commands.json` is in **execution order** and every metadata entry has **`depth`**: the
  `repeat` in `Settings nested commands` produces two `depth: 1` entries under one `depth: 0`
  `repeatCommand`. A step's parent is the nearest earlier entry one level shallower.
- A step's screenshots are listed on the step itself in **`metadata.artifacts`**
  (`{"type": "SCREENSHOT", "path": "screenshots/step-005-…png"}`) — no filename matching needed.
  An optional command that failed (`WARNED`) now gets a screenshot too.
- `WARNED` has a real `duration` (2.6.1 wrote `null`).
- `metadata.error` carries only `message` and `debugMessage`; no `hierarchyRoot`.
- JUnit `time` has millisecond precision and `timestamp` is populated.

What did **not** change, and still matters:

- `defineVariablesCommand` (entry 0 of every `commands.json`) holds real values in its raw form:
  `LABEL`, `MAESTRO_*` from the process environment, and now `MAESTRO_FILENAME` and
  `MAESTRO_DEVICE_UDID`. The reporter drops it.
- Raw commands keep `"textRegex": "${LABEL}"`.

`General` also appears in the view hierarchy files — it is visible text on the Settings screen,
not a leak — which is one more reason the reporter never reads `screen-hierarchy/`.
