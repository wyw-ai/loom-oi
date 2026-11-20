#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
export GOWORK=off
exec go run ./cmd/package "$@"

