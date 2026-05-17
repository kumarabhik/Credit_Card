from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BUF_IMAGE = "bufbuild/buf:1.57.2"
SYFT_IMAGE = "anchore/syft:v1.24.0"
K6_IMAGE = "grafana/k6:0.52.0"
STACK_SERVICES = [
    "postgres",
    "redis",
    "localstack",
    "jaeger",
    "loki",
    "otel-collector",
    "prometheus",
    "grafana",
    "smoke-auth",
    "edge-gateway",
]


def run(command: list[str], *, cwd: Path = ROOT) -> None:
    print("+", " ".join(command))
    subprocess.run(command, cwd=cwd, check=True)


def compose(*args: str) -> list[str]:
    return ["docker", "compose", *args]


def docker_run(image: str, *args: str) -> list[str]:
    return [
        "docker",
        "run",
        "--rm",
        "-v",
        f"{ROOT}:/workspace",
        "-w",
        "/workspace",
        image,
        *args,
    ]


def up() -> None:
    run(compose("up", "-d", "--wait", *STACK_SERVICES))


def down() -> None:
    run(compose("down", "--volumes", "--remove-orphans"))


def lint() -> None:
    run([sys.executable, "scripts/pre_commit_runner.py", "all"])
    run(compose("config", "-q"))
    run(docker_run(BUF_IMAGE, "lint"))


def proto() -> None:
    run(docker_run(BUF_IMAGE, "lint"))
    run(docker_run(BUF_IMAGE, "generate", "--template", "buf.gen.yaml"))


def smoke(*, ensure_stack: bool = True) -> None:
    if ensure_stack:
        try:
            wait_for_ready("http://127.0.0.1:8080/readyz", timeout_seconds=5)
        except RuntimeError:
            up()
    started = time.monotonic()
    wait_for_ready("http://127.0.0.1:8080/readyz", timeout_seconds=60)
    request_body = {
        "card_token": "tok_demo_card",
        "amount": {"currency": "USD", "minor_units": 2599},
        "merchant_id": "mch_demo_grocery",
        "geo": {"lat": 37.7749, "lng": -122.4194, "country": "US"},
        "channel": "POS",
        "device_id": "device-terminal-01",
    }
    request = urllib.request.Request(
        "http://127.0.0.1:8080/v1/authorize",
        data=json.dumps(request_body).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "Idempotency-Key": f"smoke-{int(time.time())}",
            "Traceparent": "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            body = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"Smoke request failed with HTTP {exc.code}: {exc.read().decode('utf-8')}") from exc
    elapsed = time.monotonic() - started
    if body.get("decision") != "APPROVE":
        raise RuntimeError(f"Smoke request did not return APPROVE: {body}")
    print(json.dumps({"elapsed_seconds": round(elapsed, 3), "response": body}, indent=2))


def test() -> None:
    lint()
    run([sys.executable, "-m", "compileall", "scripts", "services/auth-service/dev"])
    smoke(ensure_stack=True)


def load() -> None:
    up()
    run(
        [
            "docker",
            "run",
            "--rm",
            "--add-host",
            "host.docker.internal:host-gateway",
            "-v",
            f"{ROOT}:/workspace",
            "-w",
            "/workspace",
            K6_IMAGE,
            "run",
            "loadgen/scenarios/smoke.js",
        ]
    )


def chaos() -> None:
    up()
    run(compose("kill", "smoke-auth"))
    time.sleep(3)
    run(compose("up", "-d", "--wait", "smoke-auth"))
    smoke(ensure_stack=False)


def sbom() -> None:
    run(docker_run(SYFT_IMAGE, "dir:/workspace", "-o", "table"))


def wait_for_ready(url: str, *, timeout_seconds: int) -> None:
    deadline = time.monotonic() + timeout_seconds
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                if response.status == 200:
                    return
        except Exception as exc:  # noqa: BLE001
            last_error = exc
        time.sleep(1)
    raise RuntimeError(f"Timed out waiting for {url}") from last_error


def main() -> int:
    parser = argparse.ArgumentParser(description="Run repository automation tasks.")
    parser.add_argument("command", choices=("up", "down", "lint", "test", "load", "chaos", "sbom", "proto", "smoke"))
    args = parser.parse_args()

    commands = {
        "up": up,
        "down": down,
        "lint": lint,
        "test": test,
        "load": load,
        "chaos": chaos,
        "sbom": sbom,
        "proto": proto,
        "smoke": smoke,
    }
    commands[args.command]()
    return 0


if __name__ == "__main__":
    sys.exit(main())
