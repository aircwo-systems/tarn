BINARY_NAME := openstack
VERSION := 0.1.0-dev
BUILD_DIR := ./build
GO_FILES := $(shell find . -name '*.go' -not -path './vendor/*')
LDFLAGS := -ldflags "-X github.com/openstack-project/openstack/internal/cli.version=$(VERSION)"

.PHONY: all build run clean test lint fmt vet

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/openstack

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

docker-run:
	docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock openstack:$(VERSION)
