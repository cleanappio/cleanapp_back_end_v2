# Smoke Tests (Blueprint)

These are intended to be run by CI or by an operator after deploy.

Principles:
- hit **public domains** (nginx)
- hit **localhost** endpoints (when running on the VM)
- verify the “thin waist” contracts (OpenAPI + health)

Start with `smoke_prod.sh` and evolve it into a real test suite.

