# Every docker call is prefixed DOCKER_DEFAULT_PLATFORM= (native build; SPEC §2).
DOCKER        := DOCKER_DEFAULT_PLATFORM= docker
COMPOSE       := $(DOCKER) compose
API_URL       ?= https://localhost:8443
CA_FILE       ?= $(CURDIR)/deploy/certs/ca.pem
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
IMAGE         ?= artstudio:local
GITLEAKS      := zricethezav/gitleaks:v8.30.1
HADOLINT      := hadolint/hadolint:v2.15.1
FAULT         ?=

define not_yet
	@echo "not yet: $(1)"; exit 1
endef

.PHONY: certs build test test-integration test-acceptance up down lint vet vuln gitleaks hadolint image ci decisions chaos-up chaos heal obs-validate test-obs

certs:
	go run ./tools/gencert

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/ ./cmd/...

test:
	go test -race ./...

test-integration:
	go test -race -tags integration ./...

test-acceptance:
	API_URL=$(API_URL) CA_FILE=$(CA_FILE) go test -count=1 -tags acceptance ./test/acceptance/...

up: certs
	@test -f .env || { echo "missing .env: cp .env.example .env"; exit 1; }
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

vet:
	go vet ./...

lint:
	golangci-lint run

vuln:
	govulncheck ./...

gitleaks:
	$(DOCKER) run --rm -v "$(CURDIR):/repo" $(GITLEAKS) dir /repo --redact

hadolint:
	$(DOCKER) run --rm -i $(HADOLINT) < Dockerfile

image:
	$(DOCKER) build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE) .

ci: vet lint test vuln gitleaks hadolint image

decisions:
	@{ echo "# Decisions"; echo; \
	for f in $$(ls docs/decisions/D-*.md 2>/dev/null | sort); do \
	  t=$$(grep -m1 '^#' "$$f" | sed 's/^#* *//'); \
	  echo "- [$$t]($$f)"; \
	done; } > DECISIONS.md
	@echo "wrote DECISIONS.md"

chaos-up: certs
	UPSTREAM_BASE_URL=http://stub:8080/api/v1 $(COMPOSE) --profile chaos up -d --build --wait

chaos:
	@test -n "$(FAULT)" || { echo "usage: make chaos FAULT=429|503|slow|timeout"; exit 1; }
	$(COMPOSE) exec stub /stubserver ctl $(FAULT)

heal:
	$(COMPOSE) exec stub /stubserver ctl clear

obs-validate:
	$(call not_yet,compose.obs.yaml and deploy/obs (team E))

test-obs:
	$(call not_yet,observability stack (team E, after G3))
