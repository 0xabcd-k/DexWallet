#!/usr/bin/env python3
"""Install pinned build tools locally; never change the user's global goctl."""
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]

if __name__ == "__main__":
    versions = json.loads((ROOT / "scripts/tools.json").read_text())
    target = ROOT / ".tools/bin"
    target.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ["go", "install", "github.com/zeromicro/go-zero/tools/goctl@" + versions["goctl"]],
        cwd=ROOT,
        env={**os.environ, "GOBIN": str(target), "GOTOOLCHAIN": "go" + versions["go"]},
        check=True,
    )
    subprocess.run([str(target / "goctl"), "--version"], check=True)
