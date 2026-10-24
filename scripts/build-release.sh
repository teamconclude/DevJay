#!/bin/bash
# Tag-driven release build. CSC_LINK is a base64-encoded Developer ID .p12.
set -euo pipefail
cd "$(dirname "$0")/.."
tag="${1:-${GITHUB_REF_NAME:-}}"
if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo 'Expected a release tag such as v0.8.0.' >&2
    exit 1
fi
export VERSION="${tag#v}"
work=$(mktemp -d "${TMPDIR:-/tmp}/devjay-release.XXXXXX")
keychain="$work/signing.keychain-db"
cleanup() {
    local status=$?
    if [[ -f "$keychain" ]]; then security delete-keychain "$keychain" || true; fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Never fall back to ad-hoc signing if signing was requested but is incomplete.
if [[ -n "${CSC_LINK:-}" ]]; then
    : "${CSC_KEY_PASSWORD:?Missing certificate password}"
    : "${APPLE_ID:?Missing Apple ID}" "${APPLE_APP_SPECIFIC_PASSWORD:?Missing Apple password}" "${APPLE_TEAM_ID:?Missing Apple team}"
    printf '%s' "$CSC_LINK" | base64 --decode > "$work/certificate.p12"
    password=$(openssl rand -hex 32)
    security create-keychain -p "$password" "$keychain"
    security set-keychain-settings -lut 21600 "$keychain"
    security unlock-keychain -p "$password" "$keychain"
    security import "$work/certificate.p12" -k "$keychain" -P "$CSC_KEY_PASSWORD" -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$password" "$keychain" >/dev/null
    SIGNING_IDENTITY=$(security find-identity -v -p codesigning "$keychain" | sed -n 's/.*"\(Developer ID Application:.*\)"/\1/p')
    [[ -n "$SIGNING_IDENTITY" && "$SIGNING_IDENTITY" != *$'\n'* ]] || { echo 'Expected one Developer ID Application identity.' >&2; exit 1; }
    export SIGNING_IDENTITY SIGNING_KEYCHAIN="$keychain" NOTARIZE=1
else
    export SIGNING_IDENTITY=- NOTARIZE=0
fi
for architecture in arm64 amd64; do
    ARCH="$architecture" ./scripts/create-installer.sh
done
