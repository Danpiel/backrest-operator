.PHONY: test lint docker chart-package build

GO ?= go

build:
	$(GO) build -o bin/operator ./cmd/operator
	$(GO) build -o bin/mcp ./cmd/mcp
	$(GO) build -o bin/export-proxy ./cmd/export-proxy

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...

docker:
	docker build -t backrest-operator:0.2.0 -f Dockerfile .
	docker build -t backrest-mcp:0.2.0 -f Dockerfile.mcp .

chart-package:
	helm package charts/backrest-operator -d dist
