BACKEND_DIR := backend
IMAGE ?= customer-service-backend:test

.PHONY: test build compose-check preflight-prod smoke commercial-acceptance lan-start lan-stop lan-status

test:
	cd $(BACKEND_DIR) && go test ./...

build:
	docker build -t $(IMAGE) ./backend

compose-check:
	docker compose -f deployments/docker-compose.dev.yml config >/tmp/customer-service-compose-dev.out
	docker compose --env-file deployments/.env.example -f deployments/docker-compose.prod.example.yml config >/tmp/customer-service-compose-prod.out

preflight-prod:
	scripts/ops/preflight-prod.sh

smoke:
	scripts/smoke/local-smoke.sh

commercial-acceptance:
	scripts/ops/commercial-acceptance.sh

lan-start:
	scripts/ops/start-lan.sh

lan-stop:
	scripts/ops/stop-lan.sh

lan-status:
	scripts/ops/status-lan.sh
