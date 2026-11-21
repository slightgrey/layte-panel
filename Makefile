# Layte Panel — common tasks (Linux/macOS or Git Bash on Windows).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
VM      := layte-dev

.PHONY: web build build-linux test dev-agent dev-serve dev-web vm-up vm-shell vm-deploy test-integration clean

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o layte ./cmd/layte

build-linux: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/layte-linux-amd64 ./cmd/layte
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/layte-linux-arm64 ./cmd/layte

test:
	go vet ./...
	go test ./...
	cd web && npm test

# Local development: run each in its own terminal.
dev-agent:
	go run ./cmd/layte agent --dev
dev-serve:
	go run ./cmd/layte serve --dev
dev-web:
	cd web && npm run dev

# Ubuntu 24.04 VM for testing real system actions (never run the agent on your own machine).
vm-up:
	multipass launch 24.04 --name $(VM) --cpus 1 --memory 1G --disk 10G

vm-shell:
	multipass shell $(VM)

# Installs (or upgrades) the locally built binary with the real installer, so
# install.sh is tested too. The first run also installs the web stack.
vm-deploy: build-linux
	multipass transfer dist/layte-linux-amd64 $(VM):/tmp/layte
	multipass transfer deploy/install.sh $(VM):/tmp/install.sh
	multipass exec $(VM) -- sudo bash /tmp/install.sh --binary /tmp/layte --with-stack --yes --admin ''

# Real system tests: build a linux test binary here, run it as root in the VM
# against the running layte-agent (make vm-deploy first).
test-integration:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags integration -o dist/layte-integration.test ./test/integration
	multipass transfer dist/layte-integration.test $(VM):/tmp/layte-integration.test
	multipass exec $(VM) -- sudo /tmp/layte-integration.test -test.v

clean:
	rm -rf layte layte.exe dist .dev
