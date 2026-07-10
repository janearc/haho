.PHONY: build test test-unit test-integration proto clean lint

build:
	go build -o bin/haho ./cmd/haho

test:
	go test ./... -v

test-unit:
	go test ./internal/... -v -short

test-integration:
	go test ./test/... -v -tags=integration

# Requires buf.yaml to be configured with proto source and plugin definitions.
proto:
	buf generate

clean:
	rm -rf bin/

lint:
	go vet ./...
