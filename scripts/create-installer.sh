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
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' build/Info.plist)
minimum=$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' build/Info.plist)
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Expected a numeric major.minor.patch bundle version." >&2
    exit 1
fi
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
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$work/mounted" dist
cp build/Info.plist "$app/Contents/Info.plist"
cp build/DevJay.icns "$app/Contents/Resources/DevJay.icns"
export MACOSX_DEPLOYMENT_TARGET="$minimum"
export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=$minimum"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=$minimum"
for arch in "${architectures[@]}"; do
    echo "Building DevJay $version for ${arch}..."
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" go build -trimpath \
        -ldflags="-s -w -extldflags=-mmacosx-version-min=$minimum" \
        -o "$work/DevJay-$arch" ./cmd/devjay
done
if [[ "$architecture" == universal ]]; then
    lipo -create "$work/DevJay-arm64" "$work/DevJay-amd64" -output "$app/Contents/MacOS/DevJay"
else
    cp "$work/DevJay-$architecture" "$app/Contents/MacOS/DevJay"
fi
chmod 755 "$app/Contents/MacOS/DevJay"
codesign --force --sign - "$app"
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

This development build is ad-hoc signed and has not been notarized by Apple.
If macOS 15 or later blocks a downloaded copy, attempt to open it, then use
System Settings > Privacy & Security > Open Anyway. Control-click > Open no
longer overrides Gatekeeper. See https://developer.apple.com/news/?id=saqachfa.
Ad-hoc signatures change with the build, so an update may require granting
privacy permissions again, including access to Documents or Desktop folders.
Public distribution requires Developer ID signing, hardened runtime, and
Apple notarization.

DevJay is made by the Conclude team and licensed under MIT (see LICENSE.txt).
INSTALL
name="DevJay-$version-macos-$architecture.dmg"
hdiutil create -volname "DevJay $version" -srcfolder "$work/image" \
    -fs HFS+ -format UDZO "$work/$name"
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
mv -f "$work/$name" "dist/$name"
(cd dist && shasum -a 256 "$name" > "$name.sha256")
echo "Created dist/$name"
