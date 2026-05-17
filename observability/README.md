# Observability

Observability artifacts live here.

- `prometheus/rules/` for SLO and alert rules
- `runbooks/` for alert response and recovery procedures
- `grafana/` for dashboards
- `loki/` for logging configuration
- `otel/` for OpenTelemetry collector assets

Every behavior change should eventually connect to at least one metric, log field, or trace attribute.
