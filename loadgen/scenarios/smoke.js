import http from "k6/http";
import { check } from "k6";

export const options = {
  scenarios: {
    smoke: {
      executor: "constant-arrival-rate",
      rate: 100,
      timeUnit: "1s",
      duration: "30s",
      preAllocatedVUs: 25,
      maxVUs: 100,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(99)<1000"],
  },
};

export default function () {
  const payload = JSON.stringify({
    card_token: "tok_demo_card",
    amount: { currency: "USD", minor_units: 2599 },
    merchant_id: "mch_demo_grocery",
    geo: { lat: 37.7749, lng: -122.4194, country: "US" },
    channel: "POS",
    device_id: `device-${__ITER}`,
  });

  const response = http.post("http://host.docker.internal:8080/v1/authorize", payload, {
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": `load-${__VU}-${__ITER}`,
      Traceparent: "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
    },
  });

  check(response, {
    "authorize returns 200": (res) => res.status === 200,
    "authorize returns APPROVE": (res) => res.json("decision") === "APPROVE",
  });
}
