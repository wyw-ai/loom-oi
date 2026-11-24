#!/bin/sh
set -eu
cd "$(dirname "$0")"
out=${1:-dist/loom-oi-linux}
mkdir -p "$out/runtime"
GOWORK=off go build -trimpath -o "$out/runtime/oiapp-loom-oi" ./cmd/worker
cp app.yaml "$out/app.yaml"
