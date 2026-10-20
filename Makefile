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
	@set -e; monitor_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-native-monitor.XXXXXX"); \
	trap 'rm -f "$$monitor_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework CoreGraphics tests/native_monitor_test.m -o "$$monitor_bin"; \
	"$$monitor_bin"
	@set -e; review_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-native-review.XXXXXX"); \
	trap 'rm -f "$$review_bin"' EXIT; \
	clang -fblocks -framework AppKit -framework WebKit tests/native_review_test.m -o "$$review_bin"; \
	"$$review_bin"
	@set -e; sizing_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-card-sizing.XXXXXX"); \
	trap 'rm -f "$$sizing_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework WebKit tests/card_sizing_test.m -o "$$sizing_bin"; \
	"$$sizing_bin"
	@set -e; settings_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-settings-layout.XXXXXX"); \
	trap 'rm -f "$$settings_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework WebKit tests/settings_layout_test.m -o "$$settings_bin"; \
	"$$settings_bin"
	@set -e; author_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-pr-author.XXXXXX"); \
	trap 'rm -f "$$author_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework WebKit tests/pr_author_layout_test.m -o "$$author_bin"; \
	"$$author_bin"
	@set -e; menu_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-card-menu.XXXXXX"); \
	trap 'rm -f "$$menu_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework WebKit tests/card_menu_test.m -o "$$menu_bin"; \
	"$$menu_bin"
	@set -e; tooltip_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-app-tooltip.XXXXXX"); \
	trap 'rm -f "$$tooltip_bin"' EXIT; \
	clang -fblocks -framework AppKit -framework WebKit -framework QuartzCore tests/app_tooltip_test.m -o "$$tooltip_bin"; \
	"$$tooltip_bin"
	@set -e; apps_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-site-apps.XXXXXX"); \
	trap 'rm -f "$$apps_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework CoreGraphics tests/site_apps_test.m -o "$$apps_bin"; \
	"$$apps_bin"
	@set -e; agents_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-agent-processes.XXXXXX"); \
	trap 'rm -f "$$agents_bin"' EXIT; \
	clang -Wall -Wextra -Werror -framework Foundation tests/agent_processes_test.m -o "$$agents_bin"; \
	"$$agents_bin"
	@set -e; strip_bin=$$(mktemp "$${TMPDIR:-/tmp}/devjay-strip-controls.XXXXXX"); \
	trap 'rm -f "$$strip_bin"' EXIT; \
	clang -Wall -Wextra -Werror -fblocks -framework AppKit -framework Carbon -framework QuartzCore tests/strip_controls_test.m -o "$$strip_bin"; \
	"$$strip_bin"
	go test -ldflags="$(GO_LINK_FLAGS)" ./...
	go vet ./...
	node --input-type=module --check < cmd/devjay/frontend/app.js
	node --test tests/*.test.mjs
	plutil -lint build/Info.plist
