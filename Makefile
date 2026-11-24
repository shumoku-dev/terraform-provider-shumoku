default: build

.PHONY: build install lint generate fmt test testacc

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run make fmt" && exit 1)

# Formats the examples and regenerates docs/ with tfplugindocs.
generate:
	cd tools && go generate -tags generate ./...

fmt:
	gofmt -s -w -e .
	terraform fmt -recursive ./examples/

# Unit tests and the mock-server provider tests (needs the terraform CLI on PATH).
test:
	go test -v -cover -timeout=300s ./...

# Acceptance tests against a real, disposable Shumoku Server with an admin API token:
#   SHUMOKU_ENDPOINT=http://127.0.0.1:8080 SHUMOKU_TOKEN=shmk_... make testacc
testacc:
	TF_ACC=1 go test -v -cover -timeout 30m ./internal/provider -run '^TestAcc'
