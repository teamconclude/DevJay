#!/bin/bash
# Build a self-contained drag-to-Applications image without changing bin/DevJay.app.
set -euo pipefail

cd "$(dirname "$0")/.."
if [[ "$(uname -s)" != Darwin ]]; then
    echo "The installer must be built on macOS." >&2
    exit 1
fi
architecture="${ARCH:-universal}"
case "$architecture" in
    universal) architectures=(arm64 amd64); macho_architectures=(arm64 x86_64) ;;
    arm64) architectures=(arm64); macho_architectures=(arm64) ;;
    amd64) architectures=(amd64); macho_architectures=(x86_64) ;;
    *) echo "ARCH must be universal, arm64, or amd64." >&2; exit 1 ;;
esac
version=${VERSION:-$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' build/Info.plist)}
dist=${DIST_DIR:-dist}
identity=${SIGNING_IDENTITY:--}
notarize=${NOTARIZE:-0}
minimum=$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' build/Info.plist)
if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo "Expected a numeric major.minor.patch bundle version." >&2
    exit 1
fi
if [[ "$notarize" == 1 ]]; then
    [[ "$identity" != - ]] || { echo 'Notarization requires Developer ID signing.' >&2; exit 1; }
    : "${APPLE_ID:?}" "${APPLE_APP_SPECIFIC_PASSWORD:?}" "${APPLE_TEAM_ID:?}"
fi
notarize_file() {
    xcrun notarytool submit "$1" --apple-id "$APPLE_ID" --password "$APPLE_APP_SPECIFIC_PASSWORD" --team-id "$APPLE_TEAM_ID" --wait
}
sign_args=(--force --sign "$identity")
if [[ "$identity" != - ]]; then sign_args+=(--options runtime --timestamp); fi
if [[ -n "${SIGNING_KEYCHAIN:-}" ]]; then sign_args+=(--keychain "$SIGNING_KEYCHAIN"); fi
work=$(mktemp -d "${TMPDIR:-/tmp}/devjay-installer.XXXXXX")
mounted=false
cleanup() {
    local status=$?
    if "$mounted"; then hdiutil detach "$work/mounted" -quiet || true; fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
app="$work/image/DevJay.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$work/mounted" "$dist"
cp build/Info.plist "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $version" "$app/Contents/Info.plist"
cp build/DevJay.icns "$app/Contents/Resources/DevJay.icns"
cp LICENSE "$app/Contents/Resources/LICENSE.txt"
export MACOSX_DEPLOYMENT_TARGET="$minimum"
export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=$minimum"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=$minimum"
for arch in "${architectures[@]}"; do
    echo "Building DevJay $version for ${arch}..."
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" go build -trimpath \
        -ldflags="-s -w -X main.buildVersion=$version -extldflags=-mmacosx-version-min=$minimum" \
        -o "$work/DevJay-$arch" ./cmd/devjay
done
if [[ "$architecture" == universal ]]; then
    lipo -create "$work/DevJay-arm64" "$work/DevJay-amd64" -output "$app/Contents/MacOS/DevJay"
else
    cp "$work/DevJay-$architecture" "$app/Contents/MacOS/DevJay"
fi
chmod 755 "$app/Contents/MacOS/DevJay"
codesign "${sign_args[@]}" "$app"
if [[ "$notarize" == 1 ]]; then
    ditto -c -k --sequesterRsrc --keepParent "$app" "$work/notarize.zip"
    notarize_file "$work/notarize.zip"
    xcrun stapler staple "$app"
    xcrun stapler validate "$app"
fi
codesign --verify --deep --strict "$app"
for macho_arch in "${macho_architectures[@]}"; do
    lipo "$app/Contents/MacOS/DevJay" -verify_arch "$macho_arch"
done
ln -s /Applications "$work/image/Applications"
name="DevJay-$version-macos-$architecture.dmg"
hdiutil create -volname "DevJay $version" -srcfolder "$work/image" \
    -fs HFS+ -format UDZO "$work/$name"
if [[ "$notarize" == 1 ]]; then
    codesign "${sign_args[@]}" "$work/$name"
    notarize_file "$work/$name"
    xcrun stapler staple "$work/$name"
    xcrun stapler validate "$work/$name"
fi
hdiutil verify "$work/$name"
hdiutil attach "$work/$name" -readonly -nobrowse -mountpoint "$work/mounted" -quiet
mounted=true
codesign --verify --deep --strict "$work/mounted/DevJay.app"
for macho_arch in "${macho_architectures[@]}"; do
    lipo "$work/mounted/DevJay.app/Contents/MacOS/DevJay" -verify_arch "$macho_arch"
done
[[ "$(readlink "$work/mounted/Applications")" == /Applications ]]
cmp "$app/Contents/MacOS/DevJay" "$work/mounted/DevJay.app/Contents/MacOS/DevJay"
hdiutil detach "$work/mounted" -quiet
mounted=false
zip="DevJay-$version-macos-$architecture.zip"
ditto -c -k --sequesterRsrc --keepParent "$app" "$work/$zip"
mv -f "$work/$name" "$work/$zip" "$dist/"
(cd "$dist" && shasum -a 256 "$name" > "$name.sha256" && shasum -a 256 "$zip" > "$zip.sha256")
# Both architecture-specific packages share the same installer and version feed.
printf '%s\n' "$version" > "$dist/version.txt"
cp scripts/install.sh "$dist/install.sh"
echo "Created $dist/$name and $dist/$zip"
