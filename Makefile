.PHONY: help gitleaks hooks ci ci-go ci-analyzer ci-ingest-v1 fmt-go test-go vet-go lint-go rust-fmt rust-clippy analyzer-build-dev analyzer-tag-prod prometheus-install watchdog-install report-auth-up report-auth-down report-auth-logs report-auth-test

help:
	@echo "Common commands:"
	@echo "  make gitleaks            - secret scan working tree"
	@echo "  make hooks               - install repo git hooks (pre-commit)"
	@echo "  make ci                  - run local CI (no deploy; requires docker)"
	@echo "  make fmt-go              - verify gofmt across all Go modules"
	@echo "  make test-go             - run go test across all Go modules"
	@echo "  make vet-go              - run go vet across all Go modules"
	@echo "  make lint-go             - run golangci-lint across all Go modules (requires golangci-lint)"
	@echo "  make rust-fmt            - cargo fmt --check (selected Rust crates)"
	@echo "  make rust-clippy         - cargo clippy (selected Rust crates; requires Rust toolchain)"
	@echo "  make ci-analyzer         - run analyzer golden-path locally (requires docker)"
	@echo "  make ci-ingest-v1        - run v1 fetcher-key ingest golden-path (requires docker)"
	@echo "  make analyzer-build-dev  - build+push analyzer image to :dev (Cloud Build)"
	@echo "  make analyzer-tag-prod   - promote analyzer :dev build to :prod tag"
	@echo "  make prometheus-install  - install prod Prometheus (HOST=deployer@<ip>)"
	@echo "  make watchdog-install    - install prod watchdog (HOST=deployer@<ip>)"
	@echo "  make report-auth-up      - start auth-service + report-auth-service locally (docker compose)"
	@echo "  make report-auth-logs    - tail logs for local report-auth stack"
	@echo "  make report-auth-down    - stop local report-auth stack"
	@echo "  make report-auth-test    - curl/jq smoke test against local report-auth endpoints"

ci: gitleaks ci-go rust-fmt ci-analyzer ci-ingest-v1

ci-go: fmt-go test-go vet-go

gitleaks:
	gitleaks detect --no-git --redact

hooks:
	./scripts/install_git_hooks.sh

fmt-go:
	./scripts/ci/go_fmt_check.sh

test-go:
	./scripts/ci/go_test_all.sh

vet-go:
	./scripts/ci/go_vet_all.sh

lint-go:
	./scripts/ci/golangci_lint_all.sh

rust-fmt:
	./scripts/ci/rust_fmt_check.sh

rust-clippy:
	./scripts/ci/rust_clippy_check.sh

ci-analyzer:
	./platform_blueprint/tests/ci/analyzer/run.sh

ci-ingest-v1:
	./platform_blueprint/tests/ci/ingest-v1/run.sh

analyzer-build-dev:
	cd report-analyze-pipeline && CLOUDSDK_CONFIG=/tmp/codex-gcloud-cleanapp ./build_image.sh -e dev

analyzer-tag-prod:
	cd report-analyze-pipeline && CLOUDSDK_CONFIG=/tmp/codex-gcloud-cleanapp ./build_image.sh -e prod

prometheus-install:
	HOST?=deployer@34.122.15.16
	HOST=$(HOST) ./platform_blueprint/ops/observability/install_prod_observability.sh

watchdog-install:
	HOST?=deployer@34.122.15.16
	HOST=$(HOST) ./platform_blueprint/ops/watchdog/install_prod_watchdog.sh

REPORT_AUTH_COMPOSE=conf/compose/docker-compose.report-auth.yml

report-auth-up:
	docker compose -f $(REPORT_AUTH_COMPOSE) up --build -d

report-auth-logs:
	docker compose -f $(REPORT_AUTH_COMPOSE) logs -f

report-auth-down:
	docker compose -f $(REPORT_AUTH_COMPOSE) down

report-auth-test:
	./scripts/legacy/test_report_auth_auth.sh
