#!/usr/bin/env bash
# Run by .github/workflows/android-probe.yml inside the emulator action.
#
# A script file, not inline YAML: the action feeds `script:` to `sh -c` ONE LINE
# AT A TIME, so a backslash continuation reaches Maestro as a literal `\` and a
# `cd` does not survive to the next line. The first attempt failed with
# "Flow path does not exist: .../\" for exactly that reason.
set -uo pipefail
cd "$(dirname "$0")"
mkdir -p out

echo "=== the device, as adb sees it"
adb devices -l
adb -s emulator-5554 emu avd name || true
adb shell getprop ro.product.model
adb shell getprop ro.build.version.release

echo "=== 1. maestro alone, with the flags the reporter owns"
maestro test --format junit --output out/report.xml --debug-output out/debug --flatten-debug-output --env LABEL=Settings flows/
echo "maestro exit=$? (one flow fails on purpose)"

echo "=== the device attribute Maestro actually wrote"
grep -o 'device="[^"]*"' out/report.xml || echo "(no device attribute found)"

echo "=== the debug layout it produced"
find out/debug -maxdepth 2 | head -30

echo "=== 2. the reporter over the same flows"
"$RUNNER_TEMP/qualflare-maestro" -output-dir out/qualflare-results -- maestro test --env LABEL=Settings flows/
echo "reporter exit=$? (same failing flow)"

echo "=== what the reporter decided"
python3 - <<'PY'
import glob, json
for f in sorted(glob.glob("out/qualflare-results/*.json")):
    d = json.load(open(f))
    print(f)
    print("  platform:", d.get("platform"), "| framework:", d.get("framework"), "| os:", d.get("os"))
    for s in d.get("suites", []):
        print("  suite:", s.get("name"), "| cases:", len(s.get("cases", [])))
        for c in s.get("cases", [])[:5]:
            print("    case:", c.get("name"), "| status:", c.get("status"), "| steps:", len(c.get("steps") or []),
                  "| attachments:", len(c.get("attachments") or []))
    for w in d.get("warnings") or []:
        print("  warning:", w)
PY
