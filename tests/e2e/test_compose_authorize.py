import json
import subprocess
import sys
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
READY_URL = "http://127.0.0.1:8080/readyz"
AUTHORIZE_URL = "http://127.0.0.1:8080/v1/authorize"
JAEGER_TRACE_URL = "http://127.0.0.1:16686/api/traces/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
SMOKE_IDEMPOTENCY_KEY = "smoke-authorize-demo"
TRACEPARENT = "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"


def run(command: list[str]) -> str:
    completed = subprocess.run(
        command,
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    )
    return completed.stdout


def wait_for_ready(url: str, timeout_seconds: int) -> None:
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
    raise RuntimeError(f"timed out waiting for {url}") from last_error


def wait_for_trace(timeout_seconds: int) -> dict:
    deadline = time.monotonic() + timeout_seconds
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(JAEGER_TRACE_URL, timeout=5) as response:
                payload = json.loads(response.read().decode("utf-8"))
            if payload.get("data"):
                return payload
        except urllib.error.HTTPError as exc:
            last_error = exc
            if exc.code != 404:
                raise
        time.sleep(1)
    raise RuntimeError("timed out waiting for Jaeger trace") from last_error


class ComposeAuthorizeFlowTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        try:
            wait_for_ready(READY_URL, timeout_seconds=5)
        except RuntimeError:
            run([sys.executable, "scripts/tasks.py", "up"])
            wait_for_ready(READY_URL, timeout_seconds=60)

    def test_authorize_flow_reaches_ledger_and_jaeger(self) -> None:
        request_body = {
            "card_token": "tok_demo_card",
            "amount": {"currency": "USD", "minor_units": 2599},
            "merchant_id": "mch_demo_grocery",
            "geo": {"lat": 37.7749, "lng": -122.4194, "country": "US"},
            "channel": "POS",
            "device_id": "device-terminal-01",
        }
        request = urllib.request.Request(
            AUTHORIZE_URL,
            data=json.dumps(request_body).encode("utf-8"),
            headers={
                "Content-Type": "application/json",
                "Idempotency-Key": SMOKE_IDEMPOTENCY_KEY,
                "Traceparent": TRACEPARENT,
            },
            method="POST",
        )

        with urllib.request.urlopen(request, timeout=10) as response:
            body = json.loads(response.read().decode("utf-8"))

        self.assertEqual("APPROVE", body["decision"])

        table = json.loads(
            run(
                [
                    "docker",
                    "compose",
                    "exec",
                    "-T",
                    "localstack",
                    "awslocal",
                    "dynamodb",
                    "scan",
                    "--table-name",
                    "cc-ledger-local",
                    "--output",
                    "json",
                ]
            )
        )
        ledger_items = [
            item
            for item in table["Items"]
            if item.get("idempotency_key", {}).get("S") == SMOKE_IDEMPOTENCY_KEY
        ]
        self.assertEqual(1, len(ledger_items))
        self.assertEqual("acct_demo_card", ledger_items[0]["account_id"]["S"])
        self.assertEqual("mch_demo_grocery", ledger_items[0]["merchant_id"]["S"])
        self.assertEqual("DECISION_APPROVE", ledger_items[0]["decision"]["S"])

        trace_payload = wait_for_trace(timeout_seconds=20)
        spans = trace_payload["data"][0]["spans"]
        operation_names = {span["operationName"] for span in spans}
        self.assertTrue(
            {
                "auth.authorize.http",
                "auth.orchestrate",
                "balance.authorize",
                "ledger.write",
            }.issubset(operation_names)
        )


if __name__ == "__main__":
    unittest.main()
