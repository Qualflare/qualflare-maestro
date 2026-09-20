# Real Maestro output, captured 2026-09-20

Maestro CLI **2.10.0** on an **Android 14 (API 34) x86_64 emulator**, driving the Settings app on a
GitHub `ubuntu-latest` runner, with the flags the wrapper owns:

```bash
maestro test --format junit --output out/report.xml \
  --debug-output out/debug --flatten-debug-output \
  --env LABEL=Settings \
  flows/
```

Exit code `1` (one flow fails on purpose). Produced by `.github/workflows/android-probe.yml`, which
is `workflow_dispatch`-only; re-run it to refresh this capture.

This is the first Android measurement. `docs/LIMITATIONS.md` had said Android output "is expected to
match" iOS, and most of it does — the bundle layout is identical, steps and screenshots work, and a
`WARNED` optional command gets a screenshot as on iOS 2.10. **The platform did not.**

## Why the AVD name matters

`report.xml` says:

```xml
<testsuite name="Test Suite" device="qualflare_probe_api34" tests="3" failures="1" …>
```

`qualflare_probe_api34` is the **AVD name**, and that is all the device attribute carries. Tracing
Maestro: `DeviceService.listAndroidDevices` sets a connected device's `description` to
`avdName ?: connection.serial`, `TestSuiteInteractor` copies that to `deviceName`, and
`JUnitTestSuiteReporter` writes it as `device`. So an Android device arrives as an arbitrary,
user-chosen string — an AVD name, or an adb serial like `R5CT30ABCDE` for a real phone — with nothing
in it that says "android".

iOS is different only by accident: a simulator's description is
`"${device.name} - $runtimeName - ${device.udid}"`, which is where the `iOS 26.5` in the other
captures comes from. A real iPhone joins a bare version number instead, so it has no "iOS" either.

**No string heuristic can be sound here**, which is why platform detection now reads the manifest.

## The manifest is the platform signal

`debug/<flow>/manifest.json` lists the run's artifacts, and its `DEVICE_LOG` entries differ by
platform in a way naming cannot break:

| Platform | DEVICE_LOG entries |
|---|---|
| Android (this capture) | `logs/device-logcat.txt`, `metadata.source: emulator` |
| iOS (`../maestro-2.10.0-ios26.5/`) | `logs/device-simulator.log` (`source: simulator`) and `logs/device-xctest.log` (`source: xctest`) |

logcat exists only on Android; xctest only on Apple platforms. The 2.6.x flat layout has no manifest
at all, so an Android run there still needs `-platform android`.

## What is kept, and what is not

Kept: `report.xml`, each flow's `commands.json` and `manifest.json`, and the two screenshots
(~30 KB each) Maestro took of the failed and the warned step.

Not kept, deliberately:

- `logs/device-logcat.txt` — **935 KB** for these three tiny flows. The reporter never reads `logs/`,
  and this is the clearest argument for that rule.
- `logs/maestro.log` and `debug/maestro.log` — absolute runner paths.
- `screen-hierarchy/` — full view trees, and on Android they carry every node of the Settings app.

## Other things this run settles

- The bundle layout is byte-for-byte the same shape as iOS 2.10: per-flow directory, `commands.json`
  in execution order with `depth`, `metadata.artifacts` linking screenshots per step.
- Screenshot filenames follow the 2.10 convention (`step-NNN-<command>-<arg>.png`) with no emoji —
  the ❌ in the 2.6.1 flat layout is specific to that older naming.
- `launchApp` with no `assertVisible` is enough for a passing flow, which is what makes these probe
  flows independent of Android's shifting Settings labels.
