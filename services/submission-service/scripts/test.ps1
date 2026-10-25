# Runs vet + tests (with the race detector) in a Linux Go container on the docker-compose network.
# Why a container: -race needs cgo, and Windows Application Control can block test binaries built in %TEMP%.
#   docker compose up -d --wait postgres rabbitmq   # from the repo root
#   .\scripts\test.ps1                               # extra args go to `go test`, e.g. -run TestReapStuck
$service = Split-Path -Parent $PSScriptRoot
docker run --rm `
    -v "${service}:/src" -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build `
    -w /src --network distributed-judge-engine_default `
    -e TEST_DATABASE_URL=postgres://postgres:postgres@postgres:5432/postgres?sslmode=disable `
    -e TEST_AMQP_URL=amqp://judge:judge@rabbitmq:5672/ `
    golang:1.27 sh -c "go vet ./... && go test -race -count=1 ./... $args"
exit $LASTEXITCODE
