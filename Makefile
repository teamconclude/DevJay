APP := bin/DevJay.app
MACOS_MIN := 13.0
export MACOSX_DEPLOYMENT_TARGET := $(MACOS_MIN)
CGO_CFLAGS ?= -O2 -g
export CGO_CFLAGS := $(CGO_CFLAGS) -mmacosx-version-min=$(MACOS_MIN)
GO_LINK_FLAGS := -extldflags=-mmacosx-version-min=$(MACOS_MIN)

.PHONY: build run check installer
build:
	mkdir -p "$(APP)/Contents/MacOS" "$(APP)/Contents/Resources"
	CGO_ENABLED=1 go build -ldflags="$(GO_LINK_FLAGS) -X main.buildChannel=development" -o "$(APP)/Contents/MacOS/DevJay" ./cmd/devjay
	cp build/DevJay.icns "$(APP)/Contents/Resources/DevJay.icns"
	cp build/Info.plist "$(APP)/Contents/Info.plist"
	/usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier io.teamconclude.devjay.development" "$(APP)/Contents/Info.plist"
	codesign --force --sign - "$(APP)"

installer:
	./scripts/create-installer.sh

run: build
	open "$(APP)"

check:
	@set -e; geometry_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-layout.XXXXXX"); \
	trap 'rm -f "$$geometry_bin"' EXIT; \
	clang -Wall -Wextra -Werror tests/layout_geometry_test.c -o "$$geometry_bin"; \
	"$$geometry_bin"
	@set -e; power_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-power.XXXXXX"); \
	trap 'rm -f "$$power_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit tests/ui_power_test.m -o "$$power_bin"; \
	"$$power_bin"
	@set -e; review_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-native-review.XXXXXX"); \
	trap 'rm -f "$$review_bin"' EXIT; \
	clang -fblocks -framework AppKit -framework WebKit tests/native_review_test.m -o "$$review_bin"; \
	"$$review_bin"
	go test -ldflags="$(GO_LINK_FLAGS)" ./...
	go vet ./...
	node --input-type=module --check < cmd/devjay/frontend/app.js
	node --test tests/*.test.mjs
	plutil -lint build/Info.plist
