#!/bin/sh
set -eu
cd "$(dirname "$0")"
out=${1:-dist/skills-linux}
mkdir -p "$out/runtime"
GOWORK=off go build -trimpath -o "$out/runtime/oiapp-skills" ./cmd/worker
cp app.yaml "$out/app.yaml"
