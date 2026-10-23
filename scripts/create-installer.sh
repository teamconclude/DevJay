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
    lipo -verify_arch "$macho_arch" "$app/Contents/MacOS/DevJay"
done
ln -s /Applications "$work/image/Applications"
cp LICENSE "$work/image/LICENSE.txt"
cat > "$work/image/Installation.txt" <<INSTALL
DevJay $version — macOS $minimum or newer

1. Quit any running copy of DevJay.
2. Drag DevJay.app onto the Applications folder.
3. Open DevJay from Applications, then eject this disk image.

Updating replaces the application while keeping your Sites and preferences.
Go and Node are not needed to run DevJay. Git is needed for repository status.
GitHub PR information additionally requires GitHub CLI (gh) and an existing login.

$(if [[ "$notarize" == 1 ]]; then
    echo 'This build is Developer ID signed and notarized by Apple.'
else
    echo 'This build is not notarized by Apple. If macOS blocks a downloaded copy,'
    echo 'attempt to open it, then use System Settings > Privacy & Security > Open Anyway.'
fi)

DevJay is made by the Conclude team and licensed under MIT (see LICENSE.txt).
INSTALL
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
    lipo -verify_arch "$macho_arch" "$work/mounted/DevJay.app/Contents/MacOS/DevJay"
done
[[ "$(readlink "$work/mounted/Applications")" == /Applications ]]
cmp "$app/Contents/MacOS/DevJay" "$work/mounted/DevJay.app/Contents/MacOS/DevJay"
hdiutil detach "$work/mounted" -quiet
mounted=false
zip="DevJay-$version-macos-$architecture.zip"
ditto -c -k --sequesterRsrc --keepParent "$app" "$work/$zip"
mv -f "$work/$name" "$work/$zip" "$dist/"
(cd "$dist" && shasum -a 256 "$name" > "$name.sha256" && shasum -a 256 "$zip" > "$zip.sha256")
# The terminal installer always downloads the universal archive.
if [[ "$architecture" == universal ]]; then
    printf '%s\n' "$version" > "$dist/version.txt"
    cp scripts/install.sh "$dist/install.sh"
fi
echo "Created $dist/$name and $dist/$zip"
