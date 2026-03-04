#!/bin/bash
set -euo pipefail

# Build Go binary for Linux
# Name must be 'bootstrap' for the provided.al2023 runtime
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bootstrap bootstrap.go
echo "Build complete: bootstrap"