#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

VERSION=$(git describe --tags --always --dirty)
MODULE=$(head -1 go.mod | awk '{print $2}')

docker run --rm -v "$(pwd)":/workspace -w /workspace golang:1.26 \
  go build -buildvcs=false \
  -ldflags "-X '${MODULE}/pkg/versionconstants.DdevVersion=${VERSION}'" \
  -o /workspace/ddev_install ./cmd/ddev/

mv ddev_install ~/.local/bin/ddev

ddev version | grep "DDEV version"
