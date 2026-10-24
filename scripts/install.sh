#!/bin/bash
# Install or update the selected app bundle; both build channels share settings.
set -euo pipefail
[[ "$(uname -s)" == Darwin ]] || { echo 'DevJay requires macOS.' >&2; exit 1; }
case "$(uname -m)" in
    arm64) architecture=arm64 ;;
    x86_64)
        # A Terminal running under Rosetta must still install the native ARM app.
        if [[ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" == 1 ]]; then architecture=arm64; else architecture=amd64; fi ;;
    *) echo 'Unsupported Mac architecture.' >&2; exit 1 ;;
esac
repo=https://github.com/teamconclude/DevJay
version=$(curl -fsSL "$repo/releases/latest/download/version.txt")
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'Invalid release version.' >&2; exit 1; }
dest=${DEVJAY_INSTALL_DIR:-/Applications}
if [[ ! -w "$dest" && -z "${DEVJAY_INSTALL_DIR:-}" ]]; then dest="$HOME/Applications"; fi
mkdir -p "$dest"
work=$(mktemp -d "$dest/.devjay-install.XXXXXX")
cleanup() {
    local status=$?
    if [[ -e "$work/previous.app" && ! -e "$dest/DevJay.app" ]]; then
        echo "Previous app retained at $work/previous.app; restore it before retrying." >&2
    else
        rm -rf "$work"
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
name="DevJay-$version-macos-$architecture.zip"
url="$repo/releases/download/v$version"
echo "Downloading DevJay ${version}..."
curl -fL --progress-bar "$url/$name" -o "$work/$name"
curl -fsSL "$url/$name.sha256" -o "$work/checksum"
# Require exactly the expected archive in the downloaded checksum file.
read -r digest filename < "$work/checksum"
[[ "$digest" =~ ^[0-9a-f]{64}$ && "$filename" == "$name" ]] || { echo 'Invalid checksum manifest.' >&2; exit 1; }
(cd "$work" && printf '%s  %s\n' "$digest" "$name" | shasum -a 256 -c -)
ditto -x -k "$work/$name" "$work/unpacked"
app="$work/unpacked/DevJay.app"
codesign --verify --deep --strict "$app"
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Contents/Info.plist")" == io.teamconclude.devjay ]]
[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")" == "$version" ]]
running() {
    local state
    state=$(osascript -e 'application id "io.teamconclude.devjay" is running') || return 1
    [[ "$state" == true || "$state" == false ]] || return 1
    printf '%s' "$state"
}
state=$(running)
if [[ "$state" == true ]]; then
    osascript -e 'tell application id "io.teamconclude.devjay" to quit'
    for ((i=0; i<120; i++)); do
        state=$(running)
        if [[ "$state" == false ]]; then break; fi
        sleep 1
    done
    if [[ "$state" == true ]]; then echo 'DevJay is still finishing work. Quit it and run this installer again.' >&2; exit 1; fi
fi
# Keep the previous app recoverable until replacement succeeds.
if [[ -e "$dest/DevJay.app" ]]; then mv "$dest/DevJay.app" "$work/previous.app"; fi
if ! mv "$app" "$dest/DevJay.app"; then
    if [[ -e "$work/previous.app" ]]; then mv "$work/previous.app" "$dest/DevJay.app"; fi
    exit 1
fi
echo "Installed $dest/DevJay.app"
open "$dest/DevJay.app"
