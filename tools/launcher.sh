#!/usr/bin/env bash
# Runs golem's launcher with this checkout built in, on the golem version go.mod pins.
set -euo pipefail
cd "$(dirname "$0")/.."
version=$(go list -m -f '{{.Version}}' github.com/golem-mc/golem)
cfg=$(mktemp)
trap 'rm -f "$cfg"' EXIT
printf '{"server": "%s", "plugins": ["%s"], "updateCheck": false}\n' "$version" "$PWD" >"$cfg"
go tool launcher -config "$cfg" "$@"
