#!/bin/sh
set -eu
cd "$(dirname "$0")"
out=${1:-dist/file-store-linux}
mkdir -p "$out/runtime"
GOWORK=off go build -trimpath -o "$out/runtime/oiapp-file-store" ./cmd/worker
cp app.yaml "$out/app.yaml"
