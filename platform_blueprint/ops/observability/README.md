# Observability (Prometheus)

This blueprint installs a minimal Prometheus stack on the prod VM, bound to `127.0.0.1` only.

Goals:
- Provide metrics visibility for key services (start with `report-analyze-pipeline`).
- Add basic alert rules (initially visible in the Prometheus UI; outbound notifications can be wired later).

Install:
- `HOST=deployer@<prod-ip> platform_blueprint/ops/observability/install_prod_observability.sh`

Access (from the VM):
- Prometheus UI: `http://127.0.0.1:9090`

