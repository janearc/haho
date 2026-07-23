.PHONY: build test test-unit test-integration proto proto-check clean lint

build:
	go build -o bin/haho ./cmd/haho
	go build -o bin/hahod ./cmd/hahod

test:
	go test ./... -v

test-unit:
	go test ./internal/... -v -short

test-integration:
	go test ./test/... -v -tags=integration

# Generates Go into gen/ per buf.yaml + buf.gen.yaml.
# Requires protoc-gen-go and protoc-gen-go-grpc on PATH:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
proto:
	buf generate

# Verify the contracts compile and lint (no plugins required).
proto-check:
	buf build
	buf lint

clean:
	rm -rf bin/

lint:
	go vet ./...
