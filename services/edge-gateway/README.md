# edge-gateway

Skeleton for the ingress tier.

- `extauthz/` is reserved for the Go authorization sidecar.
- Envoy configuration lives under `infra/envoy/`.
- PAN tokenization stays at the edge boundary only.
