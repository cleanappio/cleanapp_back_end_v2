.PHONY: help gitleaks hooks ci-analyzer analyzer-build-dev analyzer-tag-prod prometheus-install watchdog-install

help:
	@echo "Common commands:"
	@echo "  make gitleaks            - secret scan working tree"
	@echo "  make hooks               - install repo git hooks (pre-commit)"
	@echo "  make ci-analyzer         - run analyzer golden-path locally (requires docker)"
	@echo "  make analyzer-build-dev  - build+push analyzer image to :dev (Cloud Build)"
	@echo "  make analyzer-tag-prod   - promote analyzer :dev build to :prod tag"
	@echo "  make prometheus-install  - install prod Prometheus (HOST=deployer@<ip>)"
	@echo "  make watchdog-install    - install prod watchdog (HOST=deployer@<ip>)"

gitleaks:
	gitleaks detect --no-git --redact

hooks:
	./scripts/install_git_hooks.sh

ci-analyzer:
	./platform_blueprint/tests/ci/analyzer/run.sh

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

