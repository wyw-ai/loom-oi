#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
mkdir -p runtime
GOWORK=off CGO_ENABLED=0 GOOS=linux go build -trimpath -o runtime/oiapp-bash ./cmd/worker
