# Ometto, the route planner for Trentino-Alto Adige (omettomaps.com).
#
# One binary serves the API, the page and the workers that pop the `routes`
# queue; it needs a Queen broker beside it. How to run it, the API and the
# limits: README.md. Deployment: deploy/ometto/README.md.

SHELL := /bin/bash

BROKER_PROJECT := ometto-dev
BROKER_COMPOSE := deploy/ometto/dev-broker.compose.yml
QUEEN_URL ?= http://localhost:6633

ROUTER_BIN := bin/router
ROUTER_LOG := log/router.log
ROUTER_URL := http://localhost:8100
ROUTER_FLAGS := -city web/public/taa.json \
	-places data/osm-taa/places.json -pois data/osm-taa/pois.json \
	-layers web/public -static web/app/dist \
	-addr :8100 -queen $(QUEEN_URL) -region taa -workers 4

.PHONY: build test router router-run web routes broker-up broker-down map-check

## build: compile everything and vet it
build:
	go build ./...
	go vet ./...

## test: the engine's and the service's unit tests
test:
	go test ./...

## router: compile Ometto into bin/router
router:
	go build -o $(ROUTER_BIN) ./cmd/router
	go vet ./cmd/router/... ./internal/route/...

## router-run: restart Ometto on :8100 and wait for /api/health (broker must be up)
router-run: map-check
	-@pkill -f '$(ROUTER_BIN) -city' 2>/dev/null || true
	@sleep 2
	@mkdir -p $(dir $(ROUTER_LOG))
	@nohup $(ROUTER_BIN) $(ROUTER_FLAGS) > $(ROUTER_LOG) 2>&1 & \
	echo "starting the router, log: $(ROUTER_LOG)"
	@for i in $$(seq 1 60); do \
		if curl -sf $(ROUTER_URL)/api/health >/dev/null; then \
			curl -s $(ROUTER_URL)/api/health; echo; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "the router did not answer on $(ROUTER_URL) in 60s"; \
	tail -20 $(ROUTER_LOG); \
	exit 1

## map-check: the region map is derived and is not in git; say so before failing
map-check:
	@test -f web/public/taa.json || { \
		echo "web/public/taa.json is missing: the map is derived and is not in git."; \
		echo "Build it once with  tools/refresh-region.sh  (~4 min, see data/README-TAA.md)."; \
		exit 1; }

## web: typecheck and build the page into web/app/dist, staged
##      (never a plain `vite build`: it empties dist while :8100 serves it)
web:
	cd web/app && ./build-staged.sh

## routes: the published reference routes against a running :8100
routes:
	python3 tests/run-routes.py --no-extras

## broker-up: start the development Queen broker (:6633) and its Postgres (:5471)
broker-up:
	docker compose -p $(BROKER_PROJECT) -f $(BROKER_COMPOSE) up -d
	@echo "waiting for the broker on $(QUEEN_URL) ..."
	@for i in $$(seq 1 60); do \
		if curl -sf $(QUEEN_URL)/health >/dev/null; then \
			curl -s $(QUEEN_URL)/health; echo; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "the broker did not become healthy in 60s"; \
	docker compose -p $(BROKER_PROJECT) -f $(BROKER_COMPOSE) logs --tail=50 queen; \
	exit 1

## broker-down: stop it and delete its volume (favourites and history with it)
broker-down:
	docker compose -p $(BROKER_PROJECT) -f $(BROKER_COMPOSE) down -v
