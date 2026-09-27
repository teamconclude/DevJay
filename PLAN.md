# Remaining work

Reviewed against the implementation on 24 September 2026. Keep this file: the items below are still missing features or unresolved validation/release work. They are candidates for future work, not instructions to implement them all. Current behavior belongs in the [user guide](docs/USER_GUIDE.md) and [architecture reference](docs/ARCHITECTURE.md).

## What has already shipped in the implementation

The old roadmap understated the current app. Diff/history viewers, editor opening, guarded fast-forward updates, return after a merged PR with optional branch deletion, trial rebases, ordinary pushes, and rewritten-upstream replacement are implemented. Fetches coordinate by common Git directory, asynchronous remote results use identity/generation checks, and wake/WebKit recovery is present. Universal DMG packaging and separate development storage are also implemented.

Rewritten-upstream replacement uses exact upstream reflog evidence plus confirmation and recovery refs. This supersedes the old proposal to require a designated review checkout and a separately persisted synchronized-HEAD baseline. Recovery refs survive restart and are exposed in action results; a dedicated recovery browser is not implemented. These mechanisms still need validation under external changes and failures; implementation is not platform certification.

## Monitoring and GitHub features

- [ ] Persist observed remote-tip/rewrite warning state and acknowledgment across restarts; distinguish observed rewrites from uncertain shallow-history comparisons. Existing Git reflog evidence for replacement is a separate mechanism.
- [ ] Display explicit merge, rebase, cherry-pick, revert, and bisect state on cards. Actions already reject these operation states.
- [ ] Add a deliberate remote refresh and bounded refresh after wake/network recovery, with backoff for repeated failures/rate limits. The viewer's Refresh rereads local Git data; UI wake recovery reloads web views.
- [ ] Resolve fork PRs targeting another repository and GitHub Enterprise PRs by host/base/head identity, with overrides and ambiguous-match handling. Unrecognized SSH aliases currently show GitHub unavailable.
- [ ] Add fuller closed-PR/head-inclusion details. Cards now have clickable PR tags, draft/review/merged status, and browser-tab reuse; live Automation-permission and cross-desktop browser behavior still need validation.
- [ ] Track new feedback by durable review/comment IDs, with **Mark seen** and a persisted acknowledgment baseline. Existing report counts/freshness are not an unread inbox.
- [ ] Deduplicate polling for shared PRs; extend coverage for changed/dismissed reviews, ambiguous matches, authentication failures, and rate limits.

## Validation still needed

Existing automated tests and prior smoke checks do not close these broader validation tasks. Record machine/macOS versions, scenarios, and outcomes; see [CODE_REVIEW_FIXES.md](CODE_REVIEW_FIXES.md) for prior results.

- [ ] Repeat the reported resolution case: place a Site near bottom-right at lower resolution, quit, increase resolution, relaunch, and verify both edge gaps, monitor, and desktop. Repeat for the panel and Dock overlap.
- [ ] Exercise live resolution/scaling changes, negative monitor coordinates, rearrangement/disconnection, Dock position/auto-hide, sleep/reboot, removed Spaces, and unavailable private APIs. Keep windows reachable and valid saved placement intact.
- [ ] Test all decorations/layouts, resized panels, inactive-app navigation, minimized Sites, and multi-monitor switching without changing unrelated monitors' desktops.
- [ ] Complete broader validation of the mouse-only control strip: app reactivation, native Window-menu navigation, minimized Sites, separate monitor Spaces, all decorations, dragging, Settings/viewer open-close cycles, and supported older macOS versions.
  - Validated on macOS 27 on 27 September: the user confirmed card navigation and the original empty-desktop reproduction work. At 14:18:31 and 14:18:49, the trace shows Finder frontmost and DevJay inactive on the empty desktop, with the strip present and all Sites off-Space; neither entry caused an automatic return switch. This does not isolate whether focus refusal, stationary behavior, or their combination is decisive.
  - Full `make check` passed (Go tests/vet, 89 JavaScript tests, native checks and plist validation), and the development app built successfully. Ordinary Site windows and their original explicit activation sequence are preserved; the strip alone refuses keyboard focus and uses stationary/ignore-cycle collection behavior.
- [ ] Complete live drag checks: overlap, snapping, scrolling, restart persistence, Settings closing mid-drag, add/remove/status updates during dragging, and no accidental Site activation.
- [ ] Check Settings size/placement, reachable controls, copyable status during refresh, long names, many Sites, narrow windows, keyboard focus, VoiceOver, and color-independent status cues.
- [ ] Extend repository checks for shared worktrees, shallow history, unusual upstream names, deleted branches, moved/inaccessible folders, and simultaneous external fetches/edits.
- [ ] Exercise action failure paths and recovery with repeated force pushes, ignored/untracked collisions, symlinks, failed backups, disk errors, timeouts, and external changes. Verify preservation of local work and stashes in disposable fixtures.
- [ ] Measure idle CPU, memory, subprocess creation, and network use with many Sites and large repositories; set budgets from measurements.
- [ ] Verify Git/gh discovery and actionable setup guidance from Finder on a clean machine; test slow-network quitting and native recovery end to end.

## Release preparation

- [ ] Add macOS CI for the existing Go, JavaScript, native, and plist checks; verify a clean checkout builds with the documented prerequisites.
- [ ] Validate the macOS/Apple Silicon/Intel support matrix on suitable machines. The deployment target and universal binary alone do not prove compatibility.
- [ ] Verify icon appearance at small sizes and on light/dark backgrounds.
- [ ] Confirm MIT attribution and dependency/asset notices, including notices needed in distributed bundles.
- [ ] Add contribution guidance, issue/bug-report templates, a changelog, and private security-reporting instructions.
- [ ] Add Developer ID signing, hardened runtime, and notarization for public binaries. Versioned DMG/checksum generation already exists.
- [ ] Inspect tracked files/history for private material and machine-specific assumptions before publication; verify first-run installation, folder permissions, Site setup, and navigation without assistance.
- [ ] Obtain the owner's explicit approval before publishing a release or changing repository visibility.

## Optional later enhancements

- Configurable global shortcuts and an optional menu-bar entry (current shortcuts are app-local).
- A compact freshness/status details view and a discoverable recovery-reference browser.
- Automatic app updates after a reliable signed release process exists.
- Visibility limited to desktops containing Sites, if platform support is reliable.

Out of scope: managing editors/agent sessions, creating Spaces, running development servers/tunnels/proxies, becoming a full Git client, automatic merging/pushing, cross-platform UI, and Mac App Store distribution. Preserve the Git safety boundaries documented in [AGENTS.md](AGENTS.md) and [the architecture reference](docs/ARCHITECTURE.md#git-safety-boundaries).
