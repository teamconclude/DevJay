#!/bin/bash
# Arguments come from the verified update state, never from web content.
set -euo pipefail
pid=$1; bundle=$2; work=$3; version=$4
recover() {
    status=$?
    if (( status != 0 )); then
        if [[ ! -e "$bundle" && -d "$work/previous.app" ]]; then /bin/mv "$work/previous.app" "$bundle" || true; fi
        if [[ -d "$bundle" ]]; then open "$bundle" || true; fi
    fi
    exit "$status"
}
trap recover EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
new="$work/unpacked/DevJay.app"
/usr/bin/codesign --verify --deep --strict "$new"
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$new/Contents/Info.plist")" == io.teamconclude.devjay ]]
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$new/Contents/Info.plist")" == "$version" ]]
# Both bundles are on the same volume; preserve the old bundle on failure.
mv "$bundle" "$work/previous.app"
if ! mv "$new" "$bundle"; then
    mv "$work/previous.app" "$bundle"
    exit 1
fi
if open "$bundle"; then rm -rf "$work"; fi
