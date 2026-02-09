# Smoke Tests (Blueprint)

These are intended to be run by CI or by an operator after deploy.

Principles:
- hit **public domains** (nginx)
- hit **localhost** endpoints (when running on the VM)
- verify the “thin waist” contracts (OpenAPI + health)

Scripts:
- `smoke_prod.sh`: public-domain smoke (nginx -> services)
- `smoke_prod_vm.sh`: VM-local smoke (runs `ssh` to hit `127.0.0.1:*` endpoints + RabbitMQ invariants)
  - `HOST=deployer@<prod-vm> ./smoke_prod_vm.sh`
  - `RUN_SLOW=1` to include slower v4 endpoints
- `capture_prod_public.sh`: capture public responses as deploy evidence
