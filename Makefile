APP := bin/DevJay.app

.PHONY: build run check
build:
	mkdir -p "$(APP)/Contents/MacOS" "$(APP)/Contents/Resources"
	CGO_ENABLED=1 go build -o "$(APP)/Contents/MacOS/DevJay" .
	cp build/DevJay.icns "$(APP)/Contents/Resources/DevJay.icns"
	cp build/Info.plist "$(APP)/Contents/Info.plist"
	codesign --force --sign - "$(APP)"

run: build
	open "$(APP)"

check:
	clang -Wall -Wextra -Werror tests/layout_geometry_test.c -o /private/tmp/devjay-layout-test
	/private/tmp/devjay-layout-test
	go test ./...
	go vet ./...
	node --input-type=module --check < frontend/app.js
	node --test tests/*.test.mjs
	plutil -lint build/Info.plist
