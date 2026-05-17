from __future__ import annotations

import json
import os
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


REQUEST_COUNT = 0
START_TIME = time.time()


class SmokeHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _json(self, status: int, payload: dict[str, object]) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802
        if self.path in {"/healthz", "/readyz"}:
            self._json(200, {"status": "ok"})
            return
        if self.path == "/metrics":
            uptime = time.time() - START_TIME
            payload = (
                "# HELP smoke_auth_requests_total Total authorize requests served.\n"
                "# TYPE smoke_auth_requests_total counter\n"
                f"smoke_auth_requests_total {REQUEST_COUNT}\n"
                "# HELP smoke_auth_uptime_seconds Process uptime.\n"
                "# TYPE smoke_auth_uptime_seconds gauge\n"
                f"smoke_auth_uptime_seconds {uptime:.3f}\n"
            ).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; version=0.0.4")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        self._json(404, {"error": {"code": "NOT_FOUND", "message": "unknown route"}})

    def do_POST(self) -> None:  # noqa: N802
        global REQUEST_COUNT

        if self.path != "/v1/authorize":
            self._json(404, {"error": {"code": "NOT_FOUND", "message": "unknown route"}})
            return

        idempotency_key = self.headers.get("Idempotency-Key")
        if not idempotency_key:
            self._json(
                400,
                {
                    "error": {
                        "code": "INVALID_REQUEST",
                        "message": "Idempotency-Key header is required",
                    }
                },
            )
            return

        content_length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(content_length)
        try:
            request = json.loads(body or b"{}")
        except json.JSONDecodeError:
            self._json(
                400,
                {"error": {"code": "INVALID_REQUEST", "message": "request body must be valid JSON"}},
            )
            return

        REQUEST_COUNT += 1
        trace_id = uuid.uuid4().hex
        txn_id = f"txn_{uuid.uuid4().hex[:12]}"
        response = {
            "decision": "APPROVE",
            "risk_score": 127,
            "reason_code": "00",
            "auth_code": trace_id[:6].upper(),
            "txn_id": txn_id,
            "trace_id": trace_id,
            "merchant_id": request.get("merchant_id"),
            "idempotency_key": idempotency_key,
        }
        self._json(200, response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        message = format % args
        print(json.dumps({"service": "smoke-auth", "message": message, "ts": time.time()}))


def main() -> None:
    port = int(os.environ.get("PORT", "8080"))
    server = ThreadingHTTPServer(("0.0.0.0", port), SmokeHandler)
    print(json.dumps({"service": "smoke-auth", "event": "starting", "port": port}))
    server.serve_forever()


if __name__ == "__main__":
    main()
