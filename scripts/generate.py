#!/usr/bin/env python3
"""Generate in isolation, then update only owned files. --check never writes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
API_OUT = Path("backend/tests/codegen/example")
PLATFORM_OUT = Path("backend/api")
MODEL_OUT = Path("backend/model/example")
USER_MODEL_OUT = Path("backend/model/user")
TS_OUT = Path("frontend/packages/api-contract/src/example.ts")
REGISTRY = Path("scripts/generated-files.json")


def digest(data):
    return hashlib.sha256(data).hexdigest()


def run(args, cwd, env):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(result.stdout + result.stderr)


def render(root=ROOT):
    versions = json.loads((root / "scripts/tools.json").read_text())
    tool = root / ".tools/bin/goctl"
    if not tool.is_file():
        raise RuntimeError("Run python3 scripts/prepare_tools.py first")
    metadata = subprocess.check_output(["go", "version", "-m", str(tool)], text=True)
    if not re.search(r"\bgithub.com/zeromicro/go-zero/tools/goctl\s+" + re.escape(versions["goctl"]) + r"\s", metadata):
        raise RuntimeError("goctl version differs from scripts/tools.json")
    if not (root / "backend/goctl.yaml").is_file():
        raise RuntimeError("backend/goctl.yaml is required; refusing default SQL type mappings")
    # Direct TS generation does not represent arbitrary JSON null / interface{}.
    # Reject unsupported declarations instead of silently weakening the contract.
    for source in (root / "backend/manifest/api").rglob("*.api"):
        content = re.sub(r"//[^\n]*", "", source.read_text())
        if re.search(r"\binterface\s*\{|\*", content):
            raise RuntimeError(f"Unsupported nullable/untyped API declaration: {source.name}")
    with tempfile.TemporaryDirectory(prefix="dexwallet-codegen-") as folder:
        work = Path(folder)
        backend = work / "backend"
        backend.mkdir()
        for name in ("go.mod", "goctl.yaml"):
            shutil.copyfile(root / "backend" / name, backend / name)
        shutil.copytree(root / "backend/manifest", backend / "manifest")
        template = work / "templates"
        template.mkdir()
        env = {**os.environ, "GOTOOLCHAIN": "go" + versions["go"],
               "GOCTL_EXPERIMENTAL": "on", "GOCTL_HOME": str(template),
               "GOCTL_CACHE": str(work / "cache"), "TMPDIR": str(work)}
        env.pop("GOWORK", None)
        api = backend / "manifest/api/examples/contract.api"
        run([str(tool), "api", "go", "--api", str(api), "--dir", str(work / API_OUT),
             "--style", "go_zero", "--home", str(template)], backend, env)
        run([str(tool), "api", "ts", "--api", str(api), "--dir", str(work / "ts")], backend, env)
        run([str(tool), "model", "mysql", "ddl", "--src",
             str(backend / "manifest/sql/ddl/examples/contract_sample.sql"),
             "--dir", str(work / MODEL_OUT), "--style", "go_zero", "--home", str(template)], backend, env)
        run([str(tool), "model", "mysql", "ddl", "--src",
             str(backend / "manifest/sql/ddl/user/tables.sql"),
             "--dir", str(work / USER_MODEL_OUT), "--style", "go_zero", "--home", str(template)], backend, env)
        generated, scaffold = {}, {}
        platform = backend / "manifest/api/platform/platform.api"
        # Only production API scaffolds use project adapters; the isolated example
        # remains a stock goctl contract test. All other templates stay upstream.
        for name in ("handler.tpl", "context.tpl", "main.tpl"):
            source = root / "backend/template/api" / name
            target = template / "api" / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(source.read_text().replace("__API_DIR__", PLATFORM_OUT.relative_to("backend").as_posix()))
        run([str(tool), "api", "go", "--api", str(platform), "--dir", str(work / PLATFORM_OUT),
             "--style", "go_zero", "--home", str(template)], backend, env)
        run([str(tool), "api", "ts", "--api", str(platform), "--dir", str(work / "platform-ts")], backend, env)
        for file in (work / PLATFORM_OUT).rglob("*"):
            if file.is_file():
                # Project main/context use the shared config directly. Discard
                # goctl's unused per-API config instead of recreating an alias.
                if file == work / PLATFORM_OUT / "internal/config/config.go":
                    continue
                target = generated if file.name in ("types.go", "routes.go") else scaffold
                target[str(file.relative_to(work))] = file.read_bytes()
        generated["frontend/packages/api-contract/src/platform.ts"] = (work / "platform-ts/platformComponents.ts").read_bytes().rstrip(b"\n") + b"\n"
        # Error code values are sourced from the API contract, never maintained twice.
        codes = re.search(r'Code\s+string\s+`json:"code,options=([^\"]+)"`', platform.read_text()).group(1).split("|")
        constants = "\n".join(f'    {c.title().replace("_", "")} Code = "{c}"' for c in codes)
        codefile = work / "codes_gen.go"
        codefile.write_text('// Code generated by scripts/generate.py. DO NOT EDIT.\npackage apperror\n\ntype Code string\nconst (\n' + constants + '\n)\n')
        run(["gofmt", "-w", str(codefile)], backend, env)
        generated["backend/apperror/codes_gen.go"] = codefile.read_bytes()
        for file in (work / API_OUT).rglob("*"):
            if file.is_file():
                rel = file.relative_to(work)
                target = generated if file.name in ("types.go", "routes.go") else scaffold
                target[str(rel)] = file.read_bytes()
        for folder in (MODEL_OUT, USER_MODEL_OUT):
            for file in (work / folder).glob("*.go"):
                target = generated if file.name.endswith("_gen.go") else scaffold
                target[str(file.relative_to(work))] = file.read_bytes()
        model = b"\n".join(v for k, v in generated.items() if k.endswith("_model_gen.go"))
        if b"decimal.Decimal" not in model or b"decimal.NullDecimal" not in model or b"float64" in model:
            raise RuntimeError("DECIMAL mapping failed: check backend/goctl.yaml; no files changed")
        # Normalize the generator's extra EOF blank line, without changing types.
        generated[str(TS_OUT)] = (work / "ts/contractComponents.ts").read_bytes().rstrip(b"\n") + b"\n"
        if any(re.search(rb"\bany\b", v) for k, v in generated.items() if k.endswith(".ts")):
            raise RuntimeError("TS contract contains unsupported any")
        return generated, scaffold


def synchronize(root, generated, scaffold, check=False):
    registry = root / REGISTRY
    previous = json.loads(registry.read_text()) if registry.exists() else {"generated": {}, "scaffold": []}
    problems = []
    for name in previous["scaffold"]:
        if name not in scaffold and (root / name).exists():
            problems.append(f"Retired handwritten scaffold needs review: {name}")
    # Detect unregistered generator outputs, including untracked files.
    known = set(previous["generated"]) | set(previous["scaffold"]) | set(generated) | set(scaffold)
    for folder in (API_OUT, PLATFORM_OUT, MODEL_OUT, USER_MODEL_OUT, TS_OUT.parent, Path("backend/apperror")):
        for path in (root / folder).rglob("*"):
            if path.is_file() and str(path.relative_to(root)) not in known:
                if b"Code generated by" in path.read_bytes()[:200]:
                    problems.append(f"Unregistered generated file: {path.relative_to(root)}")
    for name in set(previous["generated"]) | set(generated):
        path = root / name
        current = path.read_bytes() if path.exists() else None
        expected = generated.get(name)
        if check:
            if current != expected:
                problems.append(f"Generated output differs: {name}")
        elif current is not None and current != expected:
            if digest(current) != previous["generated"].get(name):
                problems.append(f"Locally modified/unowned generated file: {name}")
    for name in scaffold:
        if check and not (root / name).is_file():
            problems.append(f"Missing scaffold: {name}")
    next_registry = {"generated": {k: digest(v) for k, v in sorted(generated.items())},
                     "scaffold": sorted(scaffold)}
    registry_bytes = (json.dumps(next_registry, indent=2, ensure_ascii=False) + "\n").encode()
    if check and (not registry.exists() or registry.read_bytes() != registry_bytes):
        problems.append("Generated file registry differs")
    if problems:
        raise RuntimeError("\n".join(problems))
    if check:
        return
    for name in set(previous["generated"]) - set(generated):
        (root / name).unlink(missing_ok=True)
    for name, content in {**scaffold, **generated}.items():
        path = root / name
        if name in scaffold and path.exists():
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
    registry.write_bytes(registry_bytes)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    try:
        outputs, skeletons = render()
        synchronize(ROOT, outputs, skeletons, args.check)
        print(f"PASS: {'checked' if args.check else 'generated'} {len(outputs)} owned files; {len(skeletons)} protected scaffolds")
    except (RuntimeError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"{error}\n")
