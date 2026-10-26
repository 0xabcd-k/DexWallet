#!/usr/bin/env python3
"""检查当前项目文档、场景覆盖、奖励参数及只读历史归档。"""

import hashlib
import json
import re
from decimal import Decimal
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
SNAPSHOT = DOCS / "archive" / "v1"
LINK = re.compile(r"\[[^\]\n]*\]\(([^)\n]+)\)")
errors = []


def require(condition, message):
    if not condition:
        errors.append(message)


def current_documents():
    return [ROOT / "AGENTS.md"] + sorted(
        p for p in DOCS.rglob("*.md") if SNAPSHOT not in p.parents
    )


def check_documents(documents):
    local_links = 0
    for path in documents:
        relative = path.relative_to(ROOT)
        content = path.read_text(encoding="utf-8")
        lines = content.splitlines()
        require(bool(lines) and lines[0].startswith("# "), f"{relative}: 缺少文档标题")
        require(content.endswith("\n"), f"{relative}: 文件末尾缺换行")
        require("\ufffd" not in content, f"{relative}: 含无效编码替换字符")
        fence = None
        for number, line in enumerate(lines, 1):
            require(line.rstrip() == line, f"{relative}:{number}: 行尾空白")
            marker = re.match(r"^\s*(`{3,}|~{3,})(.*)$", line)
            if marker:
                token, suffix = marker.groups()
                if fence is None:
                    fence = token
                elif token[0] == fence[0] and len(token) >= len(fence) and not suffix.strip():
                    fence = None
        require(fence is None, f"{relative}: 代码块未闭合")
        for raw in LINK.findall(content):
            target = raw.strip().strip("<>")
            parts = urlsplit(target)
            if parts.scheme or parts.netloc or not parts.path:
                continue
            resolved = (path.parent / unquote(parts.path)).resolve()
            local_links += 1
            require(resolved.is_relative_to(ROOT), f"{relative}: 本地链接越出仓库 {target}")
            require(resolved.exists(), f"{relative}: 链接不存在 {target}")
    print(f"当前文档 {len(documents)} 份，本地链接 {local_links} 个")


def check_ids(path, prefix, count):
    content = path.read_text(encoding="utf-8")
    found = re.findall(rf"^\| ({prefix}\d{{2}})(?=[ |])", content, re.MULTILINE)
    expected = {f"{prefix}{number:02d}" for number in range(1, count + 1)}
    require(set(found) == expected, f"{path.relative_to(ROOT)}: {prefix} 编号集合不完整")
    require(len(found) == len(set(found)), f"{path.relative_to(ROOT)}: {prefix} 编号重复")


def check_coverage():
    acceptance = DOCS / "testing" / "验收与场景追踪.md"
    for prefix, count in (("S", 33), ("E", 28), ("R", 12)):
        check_ids(acceptance, prefix, count)
    check_ids(DOCS / "product" / "02业务规则.md", "R", 12)
    check_ids(DOCS / "design" / "02用户端界面.md", "U", 17)
    admin = DOCS / "design" / "03商户与平台界面.md"
    check_ids(admin, "M", 7)
    check_ids(admin, "P", 9)
    print("场景范围：S01–S33、E01–E28、R01–R12；界面 U01–U17、M01–M07、P01–P09")


def check_reward_table():
    content = (DOCS / "product" / "04任务与奖励.md").read_text(encoding="utf-8")
    rows = []
    for line in content.splitlines():
        fields = [field.strip() for field in line.strip("|").split("|")]
        if len(fields) == 5 and fields[0] in {
            "普通一档", "普通二档", "普通三档", "小额加奖", "双倍奖", "大奖", "最高奖"
        }:
            amount, weight, probability = Decimal(fields[1]), int(fields[2]), Decimal(fields[3].rstrip("%"))
            require(amount > 0 and weight > 0, f"奖励表 {fields[0]}: 金额或权重无效")
            require(Decimal(weight) / 100 == probability, f"奖励表 {fields[0]}: 权重与概率不一致")
            rows.append((amount, weight))
    require(len(rows) == 7, "奖励表不是七档")
    require(sum(weight for _, weight in rows) == 10000, "奖励权重总和不是 10000")
    expectation = sum((amount * weight for amount, weight in rows), Decimal(0)) / 10000
    require(expectation == Decimal("0.095"), f"奖励期望错误：{expectation}")
    require(expectation / Decimal("0.10") == Decimal("0.95"), "奖励 RTP 错误")
    print(f"奖励候选表：期望 ${expectation}，RTP {expectation / Decimal('0.10')}")


def check_archive():
    manifest = json.loads((SNAPSHOT / "manifest.json").read_text(encoding="utf-8"))
    require(bool(re.fullmatch(r"[0-9a-f]{40}", manifest["source_commit"])), "归档缺少来源提交")
    paths = set()
    for item in manifest["files"]:
        name = item["path"]
        path = (SNAPSHOT / name).resolve()
        require(name not in paths, f"归档清单重复：{name}")
        paths.add(name)
        if not path.is_relative_to(SNAPSHOT) or not path.is_file():
            errors.append(f"归档文件无效或缺失：{name}")
            continue
        require(hashlib.sha256(path.read_bytes()).hexdigest() == item["sha256"], f"归档内容改变：{name}")
    actual = {path.relative_to(SNAPSHOT).as_posix() for path in SNAPSHOT.rglob("*") if path.is_file()}
    require(actual == paths | {"manifest.json"}, "归档文件集合与清单不一致")
    print(f"历史归档 {len(paths)} 个文件，逐文件核对 SHA-256")


def main():
    check_documents(current_documents())
    check_coverage()
    check_reward_table()
    check_archive()
    if errors:
        for error in errors:
            print(f"ERROR: {error}")
        raise SystemExit(1)
    print("PASS：文档结构、链接、场景、奖励参数与归档完整性检查通过")


if __name__ == "__main__":
    main()
