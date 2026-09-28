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
| Card menus and PR browser navigation | `card_menu*`, `pr_navigation*`, `native_scripts/pr_browser.js`, `frontend/card-menu.js` |
| Diff/history data and native viewer lifecycle | `git_viewer*.go` |
| Suggested actions and confirmation lifecycle | `git_suggestions.go`, `suggestions_darwin.go`, `checkout_actions*.go`, `action_lifetime.go` |
| Rebase trials, pushes, rewritten-history replacement | `rebase_preview.go`, `git_push.go`, `git_replace.go` |
| Editor selection, discovery, and file opening | `editors*.go`, `editors_darwin.m`, `editor_catalog.go`, `settings_editors_darwin.go`, `viewer_editors_darwin.go` |
| Observed development app windows on Site desktops | `site_apps.go`, `site_apps_darwin.*` |
| Best-effort Claude/Codex presence within development apps | `agent_processes.go`, `agent_processes_darwin.*` |
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

Development app labels use a separate read-only observer. Supported apps include IDEs, terminals, and the standalone Codex/ChatGPT and Claude main app bundles; helper processes and URL handlers are excluded. The batched AppKit snapshot supplies Site window numbers and desktop positions. One background worker scans WindowServer metadata and supported running application identities for all Sites, matching single-Space membership through runtime-loaded SkyLight functions. It does not read window titles or window pixels, send Apple Events, inspect terminal contents, activate windows, or alter the file-opening editor preference. All supported matches are retained; this is desktop co-location, not verified project ownership. Results flow through the manager queue. Changed Site/window/display identities invalidate old readings and reject stale in-flight results. Scans normally start ten seconds after completion, with a two-second minimum spacing for placement changes, and pause with UI recovery during display/session sleep. Wake invalidates the cache. Missing APIs or unknown desktop membership produce an unknown state rather than a false empty result. No subprocesses, per-Site polling loops, or persisted app assignments are added.

`SiteConfig.showIDEStatus` defaults to true when absent and is exposed in snapshot preferences. Only Settings can change it. Disabling clears readings, rejects pending results through the monitor generation, and prevents new scans; an already running native metadata call may finish. Re-enabling requests a fresh scan while preserving the one-worker limit. Saving a preference failure leaves the previous setting in effect and republishes authoritative state. The display has one icon presentation, controlled only by the on/off setting. Legacy `appStatusView` values are ignored when loading and omitted on the next ordinary save. The indicator groups a 22-point host icon with 16-point agent icons in a passive capsule, or shows the host icon alone when no agents are detected. App groups wrap intact and have accessible names and initials fallbacks. Only the capsule/single icon carries `data-app-tooltip`; their row, padding, and gaps are not hover targets. Hover text is a short app/agent label such as “GoLand with Claude Code and Codex.” App status hover uses the always-active AppKit tracker and a dedicated mouse-ignoring, nonactivating panel because WebKit title tooltips do not reliably appear on the inactive strip. After a 400 ms delay, the tracker rereads bounded `data-app-tooltip` text and normalized DOM bounds, rejecting stale replies before showing the content-sized panel within the anchor monitor, capped at 380 points wide. It never requests activation, key/main status, or a Space move. Exit, clicks/scrolling, window/Space changes, application switching, and sleep cancel pending/visible tooltips. Only panel/Site senders may request a tooltip refresh when their relevant snapshot content changes; ordinary Git/icon updates do not add hover work. The native refresh is a no-op unless a tooltip is already pending or visible.

Installed app icons come from `NSWorkspace.iconForFile` and are rasterized to transparent 40×40 PNGs on the AppKit thread. A bounded cache keyed by bundle path and modification time holds the small encoded images, including failed lookups. Only initial misses visit AppKit; subsequent scans reuse the cached data. No app is activated, and no screen pixels or downloaded icon assets are used. The frontend accepts observed host icons as PNG data URLs only. Two bundled PNG identity marks in `frontend/icons/` represent Claude Code and Codex independently of whether their desktop apps are installed; only the fixed agent-name allowlist selects those asset paths. Loading these static assets adds no polling or app discovery.

The same worker adds best-effort embedded Claude Code/Codex presence from a bounded native process snapshot. It recognizes native executable paths, reads candidate working directories, and walks parent IDs, including root-owned login intermediates. Go resolves symlinks off the UI thread, chooses the longest matching Site folder, then requires the owning editor/terminal PID among windows on that Site desktop. Native iTerm persistent shell servers have a narrowly recognized helper location and use iTerm's bundle identity instead of its independent GUI PID; a same-desktop iTerm window is still required. Nested/ambiguous folders, unrelated or detached processes, and desktop AI helper processes are not assigned to another Site by fallback. Folder paths participate in monitor generation checks, including Sites whose native window is not yet available. Only deduplicated agent names and display metadata (app name, icon, window count) reach the frontend; intermediate process IDs, ancestry and cwd are discarded. Presence is inferred, not exact window/tab association or working/waiting state. No argument/environment buffers, shell commands, terminal APIs, session databases, OCR, or added polling loops are used.

Changes windows have their own snapshots, selection, cancellation, and generation checks. Opening a category reuses a viewer for that Site (preferring a matching category, then the most recently used); an explicit new-window request bypasses reuse. Requests are routed using Wails' sending window, not just client-supplied Site IDs. Each comparison pins revisions. Viewer refreshes run every ten seconds after completion and when Site Git data changes, with at most two background readers. Refreshes preserve selections by file path/group and commit ID, reject stale generations, and avoid emitting unchanged content. Commit recovery checks bounded ranges around the previous page and the position predicted by the commit-count change; an explanatory note appears if recovery fails. User requests, closure, and display/session pause cancel background reads.

Card context menus use a small synchronous AppKit popup on the main thread, called off the manager queue. The selected action returns to the queue and revalidates Site/window/PR identity. PR links are derived from trusted repository identity; browser commands never accept a frontend URL. A fixed embedded JXA script inspects supported running browsers only on explicit PR actions, with Automation permission and a separate bounded command slot. Default-browser fallback occurs only when the script confirms that no navigation was attempted.

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

Site windows are ordinary Wails windows. Explicit Site navigation uses the original sequence: restore a minimized target, make it key/front and main if eligible, then activate DevJay. Background snapshots and Space notifications never invoke that navigation path.

The control strip has separate focus behavior from Site windows. It remains a nonactivating Wails panel, but a marked-instance override of the public `canBecomeKeyWindow` method refuses keyboard focus. Other instances retain their original behavior; neither the native class identity nor the base AppKit window classes are changed. The strip uses stationary/ignore-cycle collection behavior and still joins all ordinary Spaces on its monitor when enabled. Showing the strip through the reopen handler orders it front without activating DevJay. Keyboard interaction remains available in ordinary Site, Settings, and viewer windows; the strip itself is mouse-only.

On 27 September at 14:18, the user confirmed the original empty-desktop reproduction works with this change. The trace shows explicit card navigation switching to its target, then two entries onto an empty desktop with Finder frontmost, DevJay inactive, the strip present, and all Sites off-Space; neither entry caused a return switch. User testing also found the bounce absent with the strip on a side monitor. Native tests check strip-only focus refusal, unchanged Site navigation, class/method isolation, and collection-behavior flags. Broader mouse/keyboard interaction, dragging, window decorations, minimized windows, and multi-monitor/older-OS cases still require validation. The successful reproduction does not isolate whether focus refusal, stationary behavior, or their combination prevents the unwanted activation.

UI recovery reloads web views after wake/session return or WebContent failures, with bounded retries. It preserves backend operations and native placement and must never replay a confirmation. `View → Reload windows` provides manual recovery.

For unexpected desktop switches, quit the development app and launch its executable with `DEVJAY_TRACE_FOCUS=1 bin/DevJay.app/Contents/MacOS/DevJay`. The opt-in `DevJay focus:` unified log records explicit focus requests, web navigation completion, application activation, key/main-window changes, and window visibility on each active-Space change. The trace only observes state; it does not change focus policy. Use `log show --last 5m --style compact --predicate 'process == "DevJay" AND (eventMessage BEGINSWITH "DevJay focus:" OR eventMessage CONTAINS "UI recovery:")'` after reproducing. A card highlight reports Space visibility, not main-window ownership; correlate both before attributing a switch to navigation or recovery.

## Build and validation

Use macOS 13+, Go 1.27.1+, Xcode Command Line Tools, Git, and Node for checks. `gh` with existing authentication is optional for live GitHub state. `go.mod` pins Wails; no Wails CLI or npm install is needed.

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
