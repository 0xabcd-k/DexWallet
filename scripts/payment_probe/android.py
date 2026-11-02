#!/usr/bin/env python3
"""Small ADB helper for the isolated payment probe device; no wallet credentials."""

import argparse
from pathlib import Path
import re
import subprocess
import xml.etree.ElementTree as ET


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--adb", default="/home/kelizhang/Android/Sdk/platform-tools/adb")
    parser.add_argument("--serial", default="emulator-5580")
    parser.add_argument("--output", required=True)
    parser.add_argument("action", choices=["inspect", "tap", "screenshot", "shell"])
    parser.add_argument("arguments", nargs="*")
    args = parser.parse_args()
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    adb = [args.adb, "-s", args.serial]

    def run(*parts):
        result = subprocess.run(adb + list(parts), capture_output=True, timeout=30)
        if result.returncode:
            raise SystemExit(f"ADB exited {result.returncode}: {result.stderr.decode(errors='replace')}")
        return result.stdout

    if args.action == "shell":
        print(run("shell", *args.arguments).decode())
        return
    if args.action == "screenshot":
        name = args.arguments[0] if args.arguments else "screen.png"
        (output / name).write_bytes(run("exec-out", "screencap", "-p"))
        print(output / name)
        return

    remote = "/sdcard/Download/dexwallet-payment-probe.xml"
    run("shell", "uiautomator", "dump", remote)
    data = run("exec-out", "cat", remote)
    (output / "current-ui.xml").write_bytes(data)
    nodes = list(ET.fromstring(data).iter("node"))
    if args.action == "inspect":
        for node in nodes:
            attrs = node.attrib
            if attrs.get("text") or attrs.get("content-desc"):
                print({key: attrs.get(key) for key in ["text", "content-desc", "resource-id", "class", "clickable", "bounds"]})
        return

    text = " ".join(args.arguments)
    matches = [node for node in nodes if text in (node.attrib.get("text"), node.attrib.get("content-desc"))]
    clickable = [node for node in matches if node.attrib.get("clickable") == "true"]
    target = clickable or matches
    if len(target) != 1:
        raise SystemExit(f"Expected one target for {text!r}; found {len(target)}")
    x1, y1, x2, y2 = map(int, re.findall(r"\d+", target[0].attrib["bounds"]))
    run("shell", "input", "tap", str((x1 + x2) // 2), str((y1 + y2) // 2))
    print("Tapped", text)


if __name__ == "__main__":
    main()
