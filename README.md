# artstudio

artstudio is a gateway in front of a third-party API (the Art Institute of Chicago), built to show what belongs around any vendor call in production. It was built as a take-home for a Principal Infrastructure & SRE role.

> Walking skeleton: sections below are completed at every merge (see docs/time-log.md).

## Requirements

Docker with Compose v2, Go 1.27.2, make, curl.

## Start

```sh
git clone https://github.com/dctellya/artstudio && cd artstudio
cp .env.example .env    # set CONTACT_EMAIL
make certs              # local CA + TLS certs in deploy/certs
DOCKER_DEFAULT_PLATFORM= docker compose up -d --build --wait
```

## Try it

```sh
CA=deploy/certs/ca.pem
API=https://api.127.0.0.1.nip.io:8443
curl -s --cacert "$CA" "$API/health"
curl -s --cacert "$CA" "$API/art/monet?limit=3"
```

## Tests

```sh
make test               # unit tests, no containers
make test-acceptance    # end-to-end checks against the running stack
```

## How it was built

All prompts and answers: `docs/transcript.md`, `docs/agents/logs/`, `reports/`. Method: `docs/RULES.md`, `docs/SPEC-TEMPLATE.md`, `docs/TEAM-PROTOCOL.md`.
