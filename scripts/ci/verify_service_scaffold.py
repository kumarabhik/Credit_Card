#!/usr/bin/env python3
from __future__ import annotations

import argparse
import sys
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]

SERVICE_CHECKS: dict[str, dict[str, list[tuple[str, str]]]] = {
    "edge-gateway": {
        "lint": [("file", "README.md"), ("dir", "extauthz")],
        "unit": [("file", "README.md")],
        "build": [("file", "Dockerfile")],
    },
    "fraud-service": {
        "lint": [("file", "README.md"), ("dir", "src/main/java"), ("dir", "src/main/resources")],
        "unit": [("dir", "src/test/java")],
        "build": [("file", "Dockerfile")],
    },
    "ledger-service": {
        "lint": [("file", "README.md"), ("dir", "cmd/server"), ("dir", "internal/store")],
        "unit": [("dir", "test/integration")],
        "build": [("file", "Dockerfile")],
    },
    "ml-scorer": {
        "lint": [("file", "README.md"), ("dir", "app"), ("dir", "model")],
        "unit": [("dir", "tests")],
        "build": [("file", "Dockerfile")],
    },
    "notification-service": {
        "lint": [("file", "README.md"), ("dir", "cmd/server"), ("dir", "internal/delivery")],
        "unit": [("dir", "test/integration")],
        "build": [("file", "Dockerfile")],
    },
    "settlement-service": {
        "lint": [("file", "README.md"), ("dir", "src/main/java"), ("dir", "src/main/resources")],
        "unit": [("dir", "src/test/java")],
        "build": [("file", "Dockerfile")],
    },
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Verify that a scaffolded service keeps its expected layout.")
    parser.add_argument("--service", required=True, choices=sorted(SERVICE_CHECKS))
    parser.add_argument("--phase", required=True, choices=("lint", "unit", "build"))
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    service_root = REPO_ROOT / "services" / args.service
    failures: list[str] = []

    for entry_type, relative_path in SERVICE_CHECKS[args.service][args.phase]:
        target = service_root / relative_path
        if entry_type == "file" and not target.is_file():
            failures.append(f"missing file: {target.relative_to(REPO_ROOT)}")
        if entry_type == "dir" and not target.is_dir():
            failures.append(f"missing directory: {target.relative_to(REPO_ROOT)}")

    if failures:
        print(f"{args.service} failed {args.phase} scaffold verification:", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1

    print(f"{args.service} passed {args.phase} scaffold verification.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
