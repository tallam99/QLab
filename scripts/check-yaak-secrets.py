#!/usr/bin/env python3
"""Fail if a committed Yaak workspace contains secret values.

The committed Yaak workspace (yaak/*.yaak.json) is shared and versioned, so it
must never carry real credentials. This guard scans it two ways:

  1. Secret-named variables/headers (auth_token, *secret*, *api_key*, …) must be
     empty or a template reference (${[ ... ]}) — never a literal value.
  2. Any string value anywhere must not look like a real secret (JWT, Bearer
     <literal>, provider key prefixes, PEM private key).

Template references like "Bearer ${[ auth_token ]}" are allowed — the secret
lives in each developer's local Yaak environment, not in the committed file.

Usage:
    check-yaak-secrets.py [FILE ...]        # defaults to yaak/qlab.yaak.json
Exit code 0 = clean, 1 = secrets found / file error.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

# Variable/header names that must never hold a literal value.
SECRET_NAME = re.compile(
    r"(?i)(token|secret|passwd|password|api[_-]?key|apikey|authorization"
    r"|bearer|credential|private[_-]?key|client[_-]?secret|access[_-]?key)"
)

# A value that is only a template reference, e.g. "${[ auth_token ]}".
TEMPLATE_ONLY = re.compile(r"^\s*\$\{\[[^\]]+\]\}\s*$")

# A value that contains a template reference anywhere, e.g. "Bearer ${[ auth_token ]}".
# Such values delegate the secret to a (separately-checked) variable, so they're
# allowed in secret-named fields.
CONTAINS_TEMPLATE = re.compile(r"\$\{\[[^\]]+\]\}")

# Patterns that indicate a literal secret embedded in any string value.
SECRET_VALUE_PATTERNS = [
    ("JWT", re.compile(r"eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+")),
    # "Bearer <literal>" but allow "Bearer ${[ ... ]}".
    ("bearer token", re.compile(r"(?i)bearer\s+(?!\$\{\[)\S{8,}")),
    ("Google/Firebase API key", re.compile(r"AIza[0-9A-Za-z_-]{30,}")),
    ("OpenAI-style key", re.compile(r"\bsk-[A-Za-z0-9]{16,}")),
    ("GitHub token", re.compile(r"\bgh[pousr]_[A-Za-z0-9]{20,}")),
    ("AWS access key id", re.compile(r"\bAKIA[0-9A-Z]{16}\b")),
    ("Slack token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{10,}")),
    ("PEM private key", re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----")),
]


def is_secret_name(name: str) -> bool:
    return bool(name) and SECRET_NAME.search(name) is not None


def looks_like_secret(value: str) -> str | None:
    """Return the name of the matched secret pattern, or None."""
    if TEMPLATE_ONLY.match(value):
        return None
    for label, pattern in SECRET_VALUE_PATTERNS:
        if pattern.search(value):
            return label
    return None


def walk_named_values(node, path: str):
    """Yield (path, name, value) for {"name":..., "value":...} pairs (env vars, headers)."""
    if isinstance(node, dict):
        if "name" in node and "value" in node and isinstance(node.get("value"), str):
            yield (path, node.get("name", ""), node["value"])
        for key, child in node.items():
            yield from walk_named_values(child, f"{path}.{key}")
    elif isinstance(node, list):
        for i, child in enumerate(node):
            yield from walk_named_values(child, f"{path}[{i}]")


def walk_strings(node, path: str):
    """Yield (path, value) for every string in the document."""
    if isinstance(node, dict):
        for key, child in node.items():
            yield from walk_strings(child, f"{path}.{key}")
    elif isinstance(node, list):
        for i, child in enumerate(node):
            yield from walk_strings(child, f"{path}[{i}]")
    elif isinstance(node, str):
        yield (path, node)


def check_file(path: Path) -> list[str]:
    try:
        data = json.loads(path.read_text())
    except FileNotFoundError:
        return [f"{path}: file not found"]
    except json.JSONDecodeError as exc:
        return [f"{path}: invalid JSON: {exc}"]

    problems: list[str] = []

    # Rule 1: secret-named fields must be empty or delegate to a template variable.
    for loc, name, value in walk_named_values(data, path.name):
        if is_secret_name(name) and value.strip() and not CONTAINS_TEMPLATE.search(value):
            problems.append(f"{loc}: secret-named field {name!r} has a non-empty literal value")

    # Rule 2: no string anywhere may look like a literal secret.
    for loc, value in walk_strings(data, path.name):
        label = looks_like_secret(value)
        if label:
            problems.append(f"{loc}: value looks like a {label}")

    return problems


def main(argv: list[str]) -> int:
    files = [Path(a) for a in argv[1:]]
    if not files:
        files = [Path(__file__).resolve().parent.parent / "yaak" / "qlab.yaak.json"]

    all_problems: list[str] = []
    for f in files:
        if f.suffix == ".json" or f.name.endswith(".yaak.json"):
            all_problems.extend(check_file(f))

    if all_problems:
        print("Secret check FAILED — do not commit secrets in the Yaak workspace:", file=sys.stderr)
        for p in all_problems:
            print(f"  - {p}", file=sys.stderr)
        print(
            "\nKeep credentials in your LOCAL Yaak environment only; reference them as "
            "${[ auth_token ]} in the committed file.",
            file=sys.stderr,
        )
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
