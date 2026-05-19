from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
GO_IMAGE = "golang:1.26.3"


def run(command: list[str]) -> None:
    print("+", " ".join(command))
    subprocess.run(command, cwd=ROOT, check=True)


def docker_image(*args: str) -> list[str]:
    return [
        "docker",
        "run",
        "--rm",
        "-v",
        f"{ROOT}:/workspace",
        "-w",
        "/workspace",
        *args,
    ]


def find_files(patterns: tuple[str, ...]) -> list[str]:
    files: list[str] = []
    for pattern in patterns:
        for path in ROOT.rglob(pattern):
            if ".git" in path.parts or ".sixth" in path.parts or "gen" in path.parts:
                continue
            if path.is_file():
                files.append(path.relative_to(ROOT).as_posix())
    return sorted(set(files))


def find_go_modules() -> list[str]:
    modules: list[str] = []
    for path in ROOT.rglob("go.mod"):
        if ".git" in path.parts or ".sixth" in path.parts:
            continue
        modules.append(path.parent.relative_to(ROOT).as_posix())
    return sorted(modules)


def run_gofmt(files: list[str]) -> None:
    if not files:
        print("Skipping gofmt: no Go files found.")
        return
    run(docker_image(GO_IMAGE, "gofmt", "-w", *files))


def run_goimports(files: list[str]) -> None:
    if not files:
        print("Skipping goimports: no Go files found.")
        return
    run(
        docker_image(
            GO_IMAGE,
            "sh",
            "-lc",
            "export PATH=\"$PATH:/usr/local/go/bin\"; go install golang.org/x/tools/cmd/goimports@latest >/dev/null 2>&1 && /go/bin/goimports -w \"$@\"",
            "sh",
            *files,
        )
    )


def run_golangci() -> None:
    modules = find_go_modules()
    if not modules:
        print("Skipping golangci-lint: no Go modules found.")
        return
    for module in modules:
        run(
            docker_image(
                GO_IMAGE,
                "sh",
                "-lc",
                "export PATH=\"$PATH:/usr/local/go/bin:/go/bin\"; "
                "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest >/dev/null 2>&1 && "
                f"cd {module} && golangci-lint run ./...",
            )
        )


def run_spotless() -> None:
    gradle_files = find_files(("build.gradle.kts",))
    if not gradle_files:
        print("Skipping Spotless: no Gradle build files found yet.")
        return
    gradle_wrappers = find_files(("gradlew",))
    if not gradle_wrappers:
        print("Skipping Spotless: no Gradle wrapper found in the repo.")
        return
    if shutil.which("bash") is None:
        print("Skipping Spotless: bash is required to run Gradle wrappers.")
        return
    run(["bash", "-lc", "./gradlew spotlessCheck"])


def run_ruff(files: list[str]) -> None:
    if not files:
        print("Skipping ruff: no Python files found.")
        return
    run(docker_image("ghcr.io/astral-sh/ruff:0.6.9", "check", "--fix", *files))


def run_hadolint(files: list[str]) -> None:
    if not files:
        print("Skipping hadolint: no Dockerfiles found.")
        return
    run(docker_image("hadolint/hadolint:2.12.0-debian", "hadolint", *files))


def run_gitleaks() -> None:
    run(
        docker_image(
            "ghcr.io/gitleaks/gitleaks:v8.24.2",
            "detect",
            "--source",
            ".",
            "--no-banner",
            "--redact",
            "--no-git",
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Run repo-managed pre-commit checks.")
    parser.add_argument(
        "tool",
        choices=(
            "all",
            "gofmt",
            "goimports",
            "golangci-lint",
            "spotless",
            "ruff",
            "hadolint",
            "gitleaks",
        ),
    )
    parser.add_argument("files", nargs="*")
    args = parser.parse_args()

    go_files = args.files or find_files(("*.go",))
    py_files = args.files or find_files(("*.py",))
    docker_files = args.files or [
        path
        for path in find_files(("Dockerfile", "*.Dockerfile"))
        if path.endswith("Dockerfile") or path.endswith(".Dockerfile")
    ]

    if args.tool == "all":
        run_gitleaks()
        run_gofmt(find_files(("*.go",)))
        run_goimports(find_files(("*.go",)))
        run_golangci()
        run_spotless()
        run_ruff(find_files(("*.py",)))
        run_hadolint(docker_files)
        return 0
    if args.tool == "gofmt":
        run_gofmt(go_files)
    elif args.tool == "goimports":
        run_goimports(go_files)
    elif args.tool == "golangci-lint":
        run_golangci()
    elif args.tool == "spotless":
        run_spotless()
    elif args.tool == "ruff":
        run_ruff(py_files)
    elif args.tool == "hadolint":
        run_hadolint(docker_files)
    elif args.tool == "gitleaks":
        run_gitleaks()
    return 0


if __name__ == "__main__":
    sys.exit(main())
