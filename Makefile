# SockDock for Slack — common tasks. Tools come from mise.toml (go, node).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet bindings frontend app clean

build: ## CLI + gateway
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/sockdock ./cmd/sockdock

test:
	go test -race ./...

vet:
	go vet ./...

bindings: ## regenerate the GUI's TypeScript bindings (needs wails3)
	wails3 generate bindings -ts -d gui/frontend/bindings ./gui

frontend:
	cd gui/frontend && npm ci && npm run build && touch dist/.gitkeep

app: ## dist/SockDock.app (GUI with the CLI bundled)
	VERSION=$(VERSION) ./scripts/build-app.sh

clean:
	rm -rf bin dist gui/frontend/node_modules
	find gui/frontend/dist -mindepth 1 ! -name .gitkeep -delete
