# Infrastructure

Infrastructure assets are split by responsibility:

- `envoy/` for edge listener and filter configuration
- `helm/` for the umbrella chart and per-service subcharts
- `terraform/` for reusable modules and composed environments
- `k8s/chaos/` for Chaos Mesh experiments

Environment-specific resources belong in Terraform environment directories, never inline at the repo root.
