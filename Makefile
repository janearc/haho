.PHONY: build test test-unit test-integration proto proto-check clean lint \
	daemon-up daemon-down daemon-status

build:
	go build -o bin/haho ./cmd/haho
	go build -o bin/hahod ./cmd/hahod

# hahod as a real daemon: a launchd user agent, started and stopped like
# any other. Logs (structured JSON) go to LOG_DIR; SIGTERM via bootout is
# the graceful stop (drain-and-land); KeepAlive restarts a crashed daemon,
# which the startup recovery sweep makes safe.
PLIST_LABEL := com.janearc.hahod
PLIST := $(HOME)/Library/LaunchAgents/$(PLIST_LABEL).plist
LOG_DIR := $(HOME)/.local/state/haho/log

daemon-up: build
	mkdir -p $(LOG_DIR) $(HOME)/Library/LaunchAgents
	sed -e 's|@BIN@|$(CURDIR)/bin/hahod|' -e 's|@LOG@|$(LOG_DIR)/hahod.log|' \
		deploy/$(PLIST_LABEL).plist.in > $(PLIST)
	-launchctl bootout gui/$$(id -u)/$(PLIST_LABEL) 2>/dev/null
	launchctl bootstrap gui/$$(id -u) $(PLIST)

daemon-down:
	launchctl bootout gui/$$(id -u)/$(PLIST_LABEL)

daemon-status:
	@launchctl print gui/$$(id -u)/$(PLIST_LABEL) 2>/dev/null | grep -E '^\s*(state|pid)' || echo "not loaded"
	@curl -s --max-time 2 127.0.0.1:8790/health || true
	@echo

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
