# Every docker call is prefixed DOCKER_DEFAULT_PLATFORM= (native build; SPEC §2).
DOCKER        := DOCKER_DEFAULT_PLATFORM= docker
COMPOSE       := $(DOCKER) compose
API_URL       ?= https://localhost:8443
CA_FILE       ?= $(CURDIR)/deploy/certs/ca.pem
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE         ?= artstudio:local
GITLEAKS      := zricethezav/gitleaks:v8.30.1
HADOLINT      := hadolint/hadolint:v2.15.1
VACUUM        := dshanley/vacuum:v0.33.0
FAULT         ?=

export VERSION COMMIT BUILD_DATE

.PHONY: certs build test test-integration test-acceptance test-acceptance-chaos test-acceptance-stop up down lint vet vuln gitleaks hadolint image ci decisions chaos-up chaos heal obs-validate test-obs lint-api generate

certs:
	go run ./tools/gencert

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)" -o bin/ ./cmd/...

test:
	go test -race ./...

test-integration:
	go test -race -tags integration ./...

test-acceptance:
	API_URL=$(API_URL) CA_FILE=$(CA_FILE) go test -count=1 -tags acceptance ./test/acceptance/...

test-acceptance-chaos:
	API_URL=$(API_URL) CA_FILE=$(CA_FILE) ACCEPTANCE_CHAOS=1 go test -count=1 -tags acceptance ./test/acceptance/...

test-acceptance-stop:
	API_URL=$(API_URL) CA_FILE=$(CA_FILE) ACCEPTANCE_STOP=1 go test -count=1 -tags acceptance ./test/acceptance/...

up: certs
	@test -f .env || { echo "missing .env: cp .env.example .env"; exit 1; }
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

vet:
	go vet ./...

lint:
	golangci-lint run

lint-api:
	$(DOCKER) run --rm -v "$(CURDIR):/work:ro" $(VACUUM) lint -d /work/api/openapi.yaml

generate:
	cd $(CURDIR) && go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

vuln:
	govulncheck ./...

gitleaks:
	$(DOCKER) run --rm -v "$(CURDIR):/repo" $(GITLEAKS) dir /repo --config /repo/.gitleaks.toml --redact

hadolint:
	$(DOCKER) run --rm -i $(HADOLINT) < Dockerfile

image:
	$(DOCKER) build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE) -t $(IMAGE) .

ci: vet lint lint-api test vuln gitleaks hadolint image

decisions:
	@{ echo "# Decisions"; echo; \
	for f in $$(ls docs/decisions/D-*.md 2>/dev/null | sort); do \
	  t=$$(grep -m1 '^#' "$$f" | sed 's/^#* *//'); \
	  echo "- [$$t]($$f)"; \
	done; } > DECISIONS.md
	@echo "wrote DECISIONS.md"

COMPOSE_FILES := -f compose.yaml $(if $(wildcard compose.obs.yaml),-f compose.obs.yaml)

chaos-up: certs
	UPSTREAM_BASE_URL=http://stub:8080/api/v1 CACHE_TTL_SEARCH=1s CACHE_TTL_ARTWORK=1s $(COMPOSE) $(COMPOSE_FILES) --profile chaos up -d --build
	UPSTREAM_BASE_URL=http://stub:8080/api/v1 CACHE_TTL_SEARCH=1s CACHE_TTL_ARTWORK=1s $(COMPOSE) $(COMPOSE_FILES) up -d --wait api postgres

chaos:
	@test -n "$(FAULT)" || { echo "usage: make chaos FAULT=429|503|slow|timeout"; exit 1; }
	$(COMPOSE) $(COMPOSE_FILES) exec stub /stubserver ctl $(FAULT)

heal:
	$(COMPOSE) $(COMPOSE_FILES) exec stub /stubserver ctl clear

obs-validate:
	bash deploy/obs/validate.sh

test-obs:
	go test -tags obs ./test/obs/...
