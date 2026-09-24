# DevJay architecture and development

DevJay is a macOS-only Go application with Wails v3 beta.23 and embedded HTML/CSS/JavaScript. Application sources live in `cmd/devjay/`. All Go files belong to package `main`; there is no frontend package manager or bundler. The application manages its own windows and observes local folders, Git repositories, and optional GitHub.com PR state. It does not own editors, agent sessions, or macOS Spaces.

## Source map

| Area | Entry points |
| --- | --- |
| Startup, windows, menus, single instance | `main.go` |
| Site lifecycle, settings events, snapshots | `site_manager.go` |
| Site identity, atomic JSON persistence, sorting | `sites.go` |
| Git polling, command runner, caches, remote refresh | `git_status.go`, `git_execution.go` |
| Comparison and branch identity | `git_comparison.go`, `git_branch_identity.go`, `git_rebased.go` |
| GitHub reports and merged-branch recognition | `github_reviews.go`, `github_merged.go` |
| Diff/history data and native viewer lifecycle | `git_viewer*.go` |
| Suggested actions and confirmation lifecycle | `git_suggestions.go`, `suggestions_darwin.go`, `checkout_actions*.go`, `action_lifetime.go` |
| Rebase trials, pushes, rewritten-history replacement | `rebase_preview.go`, `git_push.go`, `git_replace.go` |
| Editor selection, discovery, and file opening | `editors*.go`, `editors_darwin.m`, `editor_catalog.go`, `settings_editors_darwin.go`, `viewer_editors_darwin.go` |
| Native activation, layout, Spaces | `windows_darwin.*`, `layout_darwin.m`, `layout_geometry.h`, `spaces_darwin.*` |
| WebKit/wake recovery | `ui_recovery*`, `frontend/ui-ready.js` |
| Shared UI, events, and styling | `frontend/app.js`, `frontend/bridge.js`, `frontend/index.html`, `frontend/style.css` |
| Focused UI helpers | Other `frontend/*.js` modules, paired with `tests/*.test.mjs` |
| Packaging | `Makefile`, `scripts/create-installer.sh`, `build/Info.plist`, `build/DevJay.icns` |

Application paths in this table are relative to `cmd/devjay/`; `tests/`, `Makefile`, `scripts/`, and `build/` are relative to the repository root. Go regression tests live beside the implementation; JavaScript and native tests live in root-level `tests/`. Keep the frontend beneath the application package because Go embedding cannot reference parent directories.

## State and event flow

`cmd/devjay/main.go` embeds its adjacent `frontend/` and creates the control panel, Settings, Site windows, and action confirmation UI. The shared frontend selects its view through URL parameters (`site`, `settings`, `viewer`, or `actions`). The control panel is a non-activating AppKit panel, so clicking it does not necessarily give DevJay keyboard focus.

`siteManager.run` processes closures sent through `m.send`. Background Git readers and event handlers return results through this queue. The manager's mutex protects runtime membership against shutdown; it is not a substitute for the queue. Native bridges marshal AppKit work onto the main thread.

The manager combines configuration, Git state, and a batched native window snapshot into `appSnapshot`, emitted as `devjay:snapshot`. A one-second timer suppresses unchanged snapshots, ignoring poll timestamps; explicit requests still get a response. Frontend events use the `devjay:` namespace. Action preparation and confirmation require an application acknowledgment, with bridge failures shown in the UI.

Each Site starts `watchGit` once its window is ready. Local status polls every three seconds. Remote/GitHub refreshes run independently, at least a minute apart after completed attempts. Cache identity and refresh generations reject obsolete results after checkout changes. Transient failures preserve the last complete reading with an explicit cached/unavailable label.

Changes windows have their own snapshots, selection, cancellation, and generation checks. Reopening the same Site and mode focuses its existing viewer. Requests are routed using Wails' sending window, not just client-supplied Site IDs. Commit comparisons pin revisions; working-tree contents are read when a file is selected.

## Git safety boundaries

Monitoring can fetch remote-tracking refs but must not change local branches, HEAD, the index, or working files. Fetches disable hooks, tags, submodule recursion, maintenance, and extra ref mappings. Local reads, background network work, and explicit actions have separate command capacity (four, two, and two slots). Fetches are serialized by common Git directory for linked worktrees. Subprocess timeouts start after queueing.

Mutations require preparation followed by an explicit, expiring, single-use confirmation. Implemented actions are fast-forward update, return to the default branch after an exact merged-PR match (with optional local branch deletion), conflict-checked rebase, ordinary push, and verified replacement from a rewritten upstream.

Preparation pins the checkout and source/target commits. Applying rechecks identity, branch/upstream, clean files/index, operation state, and other-worktree use. Unsupported sparse checkouts, submodules, and hidden index flags are refused. Incoming ignored-file collisions must remain protected. App-owned mutations serialize by common Git directory, but cannot lock out arbitrary external edits or unsaved editor buffers.

Rebases are tried in disposable repositories; only a conflict-free result is installed. Replacement requires the exact local HEAD among the last 256 upstream reflog tips, not patch similarity or commit dates. Ordinary pushes verify the remote result and never force or retry automatically. Local mutations run without deadline cancellation once started; quitting waits for confirmed actions without blocking AppKit.

Before changing local branches, actions preserve commits under `refs/devjay/backups/<action-id>`. Completion markers under `refs/devjay/completed/` allow pruning to the ten latest verified successes; failed, partial, and legacy backups remain. These refs do not protect uncommitted files. Inspect post-operation state even on errors; never blindly roll back. See the [user guide](USER_GUIDE.md#git-monitoring) for recovery commands and action restrictions.

## Persistence and native behavior

`sites.go` stores Site registration, ordering, layout, and editor preferences in an atomically replaced JSON file. Invalid configuration is reported and not overwritten. Site IDs are stable for canonical folder paths. Window geometry, display/desktop identity, and native appearance preferences use macOS preferences.

| Build | Sites file | Bundle/preferences identity |
| --- | --- | --- |
| Installed | `~/Library/Application Support/DevJay/sites.json` | `io.teamconclude.devjay` |
| Development | `~/Library/Application Support/DevJay Development/sites.json` | `io.teamconclude.devjay.development` |

Each channel has a separate single-instance identity. Avoid changing production data when testing.

Activation uses AppKit; desktop identification/restoration depends on runtime-loaded private SkyLight APIs. Missing APIs/displays/Spaces must leave windows reachable and must not erase a valid saved assignment after a failed restore. Geometry preserves nearby bottom/right edge gaps and clamps windows to usable screen bounds. Automated geometry checks do not certify real desktop switching.

UI recovery reloads web views after wake/session return or WebContent failures, with bounded retries. It preserves backend operations and native placement and must never replay a confirmation. `View → Reload windows` provides manual recovery.

## Build and validation

Use macOS 13+, Go 1.25+, Xcode Command Line Tools, Git, and Node for checks. `gh` with existing authentication is optional for live GitHub state. `go.mod` pins Wails; no Wails CLI or npm install is needed.

```sh
make check     # Go tests/vet, JS tests/syntax, native checks, plist validation
make build     # bin/DevJay.app, development identity, ad-hoc signed
make run       # build and open the development app
make installer # universal DMG and SHA-256 checksum in dist/
```

Run these commands from the repository root. Direct Go builds target `./cmd/devjay`; `go test ./...` and `go vet ./...` still cover the entire module. Quit the development app before rebuilding it. `make installer ARCH=arm64` or `ARCH=amd64` selects one architecture. Packaging uses a temporary build, leaves `bin/` untouched, verifies signatures/architectures and the mounted image, and reads the version from `build/Info.plist`. Installers are ad-hoc signed, not notarized. A universal binary and macOS 13 deployment target are not evidence of testing on every supported machine.

For changes to concurrency or operation lifetime, also run the Go race suite with the same deployment target as the Makefile:

```sh
MACOSX_DEPLOYMENT_TARGET=13.0 CGO_CFLAGS='-O2 -g -mmacosx-version-min=13.0' \
  go test -race -ldflags='-extldflags=-mmacosx-version-min=13.0' ./...
```

Tests use disposable repositories and do not contact GitHub or modify development repositories. Native checks cover geometry, power/recovery behavior, and bridge regressions; live interaction still needs manual testing. Exercise relevant add/remove, sorting/dragging, inactive-window clicks, minimized Sites, multi-monitor/Space placement, quit/relaunch, and display changes. Record machine/macOS details and actual outcomes. Remaining validation and feature gaps are in [PLAN.md](../PLAN.md); prior review validation is in [CODE_REVIEW_FIXES.md](../CODE_REVIEW_FIXES.md).
