BINARY_NAME := openstack
VERSION := 0.1.0-dev
BUILD_DIR := ./build
GO_FILES := $(shell git ls-files '*.go')
LDFLAGS := -ldflags "-X github.com/openstack-project/openstack/internal/cli.version=$(VERSION)"

.PHONY: all build secrets-proxy db-proxy start run clean test lint fmt vet ui-install ui-dev ui-build docker-build docker-build-ui docker-run

all: build

build: secrets-proxy db-proxy
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/openstack

secrets-proxy:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/secrets-proxy-linux ./cmd/secrets-proxy

db-proxy:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/db-proxy-linux ./cmd/db-proxy

start: build
	$(BUILD_DIR)/$(BINARY_NAME) start

run: build
	$(BUILD_DIR)/$(BINARY_NAME) start

install:
	go install $(LDFLAGS) ./cmd/openstack

clean:
	rm -rf $(BUILD_DIR)
	go clean

test:
	go test ./... -v -race

test-short:
	go test ./... -short

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w $(GO_FILES)

vet:
	go vet ./...

# Development helpers
dev: build
	$(BUILD_DIR)/$(BINARY_NAME) start --port 4566

docker-build:
	docker build -t openstack:$(VERSION) .

docker-build-ui:
	docker build --build-arg OPENSTACK_BUILD_UI=true -t openstack:$(VERSION)-ui .

docker-run:
	docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock openstack:$(VERSION)

ui-install:
	cd ui && bun install

ui-dev:
	cd ui && bun run dev

ui-build:
	cd ui && bun run build
