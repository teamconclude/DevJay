#!/bin/bash
# Apply DevJay's Darwin failed-exec fix without changing the installed toolchain.
set -euo pipefail
case "${1:-}" in
    build|test|vet|run) command=$1; shift ;;
    *) echo 'Usage: scripts/go.sh {build|test|vet|run} [go arguments...]' >&2; exit 2 ;;
esac
if [[ "$(go env GOOS)" != darwin ]]; then
    exec go "$command" "$@"
fi
# A second overlay could silently replace this fix. Keep one explicit owner.
if [[ "${GOFLAGS:-}" == *-overlay* ]]; then
    echo 'DevJay builds cannot be combined with another Go -overlay.' >&2
    exit 2
fi
for argument in "$@"; do
    case "$argument" in
        -overlay|-overlay=*) echo 'DevJay builds cannot be combined with another Go -overlay.' >&2; exit 2 ;;
    esac
done
script_dir=$(cd "$(dirname "$0")" && pwd)
goroot=$(go env GOROOT)
work=$(mktemp -d "${TMPDIR:-/tmp}/devjay-go-overlay.XXXXXX")
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Bootstrap on the host even when the app targets Intel from Apple Silicon.
# The generator uses only the standard library and needs neither cgo nor app flags.
GOOS= GOARCH= CGO_ENABLED=0 GOFLAGS= go run "$script_dir/go-overlay/main.go" "$goroot" "$work"
go "$command" -overlay="$work/overlay.json" "$@"
