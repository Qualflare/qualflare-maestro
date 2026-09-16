#!/usr/bin/env python3
"""Print the UDID of an available iPhone simulator on the newest iOS runtime."""
import json
import subprocess
import sys

devices = json.loads(subprocess.check_output(
    ["xcrun", "simctl", "list", "devices", "available", "-j"]))["devices"]
for runtime in sorted(devices, reverse=True):
    if "iOS" not in runtime:
        continue
    for device in devices[runtime]:
        if device["name"].startswith("iPhone"):
            print(device["udid"])
            sys.exit(0)
sys.exit("no available iPhone simulator")
