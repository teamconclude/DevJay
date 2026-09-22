# DevJay development roadmap

Updated 21 September 2026.

DevJay is a general-purpose macOS control panel for parallel development. The Conclude team created it to work more effectively on multiple branches with coding agents. Each development context is a **Site**: a local folder and an app-owned window placed on the desktop where that work happens. The control panel provides branch status and navigation between Sites, independently of the editor or agent in use.

Checked items describe implemented behavior, not certification across all supported Macs. Unchecked items are remaining work. Future features are separate implementation milestones.

## Current baseline

- [x] Go application with Wails v3 beta.23, embedded JavaScript/CSS, and a small native macOS bridge.
- [x] Register arbitrary folders through a picker or Finder drop; support Git clones, worktrees, and non-Git folders. Remove registration without deleting files.
- [x] One Site window per folder, independent of the checked-out branch, with desktop navigation through window activation.
- [x] Control panel cards showing folder, branch, Git status, and available GitHub PR review state; highlight Sites on currently visible desktops.
- [x] Settings with compact Site rows, name/manual sorting, and interactive drag ordering.
- [x] Horizontal, Vertical, and Custom control panel layouts. Layout changes fit the panel to the screen; Custom supports automatic tiles or saved overlapping positions with grid snapping.
- [x] Control panel cards can be rearranged only while Settings is open, with visible drag handles. With Settings closed, card dragging moves the panel.
- [x] None, Minimal, and Normal window decoration; optional all-desktop control panel and always-on-top windows.
- [x] Save panel size, window positions, monitor identity, desktop assignments, preferences, and custom card positions.
- [x] Resolution-aware edge anchoring: within 10% of usable screen width/height of the nearer right/bottom edge, preserve the gap in points and clamp to usable bounds.
- [x] Copyable status/error text, Settings-local duplicate-folder errors, and removal of the unreliable activation-timeout warning.
- [x] Local polling independent of remote polling, bounded Git commands, upstream-only fetches, ahead/behind/divergence, and observed remote-history rewrites.
- [x] GitHub.com open-PR status using the user's existing `gh` authentication.
- [x] App-local keyboard shortcuts, launchable ad-hoc-signed app bundle, and a bundled blue jay icon.
- [x] MIT license file and purpose-oriented README. The bundled icon uses the refined stylized blue jay with curved feathers, lighter accents, and an eye; source iterations and prompts are retained.

The current product monitors and navigates; it has no checkout-update buttons. Git fetches update remote-tracking references but do not alter checked-out files or local branches. Desktop restoration uses runtime-loaded private SkyLight APIs; ordinary activation uses AppKit. DevJay uses its own application-support directory and the `io.teamconclude.devjay` bundle identifier.

## Review follow-up — 21 September 2026

- [x] Distinguish failed Git readings from confirmed non-repositories and missing upstreams. Keep the last complete status, PR findings, cache identity, and retry time on transient errors; show an explicit cached-status warning.
- [x] Reserve four command slots for local work and two for network work. Wait at least a minute after a completed remote attempt before retrying.
- [x] Reuse porcelain ahead/behind counts and cache tool discovery. Batch native snapshot reads into one main-thread visit, share display topology for 200 ms, and suppress unchanged timer broadcasts while still answering explicit UI requests.
- [x] Preserve the empty-state button across refreshes. Keep success notices silent, remove their unused strings, and route Settings-operation errors to the Settings drop area.
- [x] Guard failed desktop-position JSON serialization, consolidate obsolete CSS, and give user-facing validation messages an explicit type.
- [x] Align the compiler, linker, and app bundle on macOS 13.0; use a unique temporary geometry-test binary and clarify which quit controls are visible.

Validation on Apple M4 Max, macOS 27.0, Go 1.27.1: `make check`, race-enabled Go tests, `staticcheck`, app build, code-signature verification, and deployment-target inspection passed. The test suite includes command failures at each local lookup, blocked network capacity, local timeout, cache/review preservation, recovery, actual branch changes, and Git ahead/behind display preferences. The binary and plist both report macOS 13.0; the dependency's duplicate `-lobjc` linker warning remains.

Live smoke checks confirmed restored Site desktop labels, a control-panel click navigating to a Site, and unchanged Settings/card appearance after CSS consolidation. The existing PR report display still updates, including the earlier-commit label. This does not replace the broader display, resolution, empty-state keyboard, and hardware matrix below.

A 20-read local benchmark on the development checkout measured 54.3 ms/read before and 53.9 ms/read after, effectively unchanged at this sample size. Local subprocess count fell from six to five per tracked Site reading; native snapshot main-thread visits fell from roughly 2N+2 to one. Full idle CPU, memory, and large-repository/network measurements remain open in section 2; per-window placement polling remains to detect otherwise silent Space moves.

## 1. Stabilize the current experience

This is the first priority before adding more features or publishing a release.

### Windows and desktops

- [ ] Repeat the exact reported resolution scenario: lower resolution, place a Site near bottom-right, quit, increase resolution, and relaunch. Verify both edge gaps, monitor, and desktop assignment. Repeat for the control panel and for a window overlapping the Dock's reserved area.
- [ ] Test live resolution changes, display scaling, negative monitor coordinates, monitor rearrangement/disconnection, Dock position/auto-hide changes, and restoration after sleep or reboot.
- [ ] Verify all three decoration modes and layouts, including restoring a manually resized panel and fitting layouts without moving to the wrong monitor.
- [ ] Test navigation from an inactive app, a focused control panel, and another Site; include minimized windows and Sites on other monitors. Confirm that unrelated monitors retain their active desktops.
- [ ] Exercise unavailable private APIs, removed Spaces, and failed desktop restoration. Keep windows reachable and avoid replacing a valid saved assignment with a failed launch placement.
- [ ] Record the tested macOS versions and hardware. The deployment target is not evidence that every older version works.

### Card interactions and Settings

- [ ] Complete live dragging checks for Horizontal, Vertical, and Custom: ordering previews, overlapping cards, snapping, scrolling, and persistence after restart.
- [ ] Confirm that closing Settings disables rearranging immediately, including an in-progress drag. A cancelled or completed drag must not activate a Site accidentally.
- [ ] Test add/remove and incoming status updates during a drag, overlapping card access, and adding Sites to an existing custom layout.
- [ ] Verify Settings resizing never moves a window underneath it, and that Done remains reachable in short windows and sits at the bottom when space permits.
- [ ] Verify copying status/error messages from both window types while background refresh continues.
- [ ] Check long folder/branch names, many Sites, narrow windows, keyboard navigation, VoiceOver labels, focus indicators, and color-independent status cues.

Acceptance: retain a short manual test record with machine/macOS details and outcomes. Fix reproducible failures and document actual platform limits. Existing geometry and JavaScript tests supplement these checks; they do not prove native multi-monitor behavior.

## 2. Make status monitoring dependable across repositories

- [ ] Inspect asynchronous refresh publication: discard results belonging to a branch, upstream, or folder identity that changed during the command sequence.
- [ ] Coordinate fetches by common Git directory for linked worktrees, while retaining bounded concurrency across independent clones.
- [ ] Persist observed remote tips and rewrite acknowledgment so a warning survives restart. Distinguish an observed rewrite from ordinary divergence and insufficient ancestry evidence in shallow repositories.
- [ ] Add explicit Git operation state for merge, rebase, cherry-pick, revert, and bisect where relevant. Keep file changes, commit divergence, operations, and PR state separate.
- [ ] Test shared worktrees, shallow repositories, unusual upstream names, branch deletion, folders moved or made inaccessible, and external Git clients fetching at the same time.
- [ ] Add deliberate manual refresh and bounded wake/network-recovery refresh; back off on repeated failures and rate limits.
- [ ] Measure idle CPU, memory, process creation, and network use with many Sites and large repositories. Set practical budgets from measurements rather than guesses.
- [ ] Verify Git and `gh` discovery when launched from Finder without a development shell. Offer actionable setup guidance for missing tools or authentication.

Acceptance: stale data is visibly stale, local status continues offline, no process queue grows without bound, and monitoring preserves HEAD, index, working files, untracked/ignored files, and stashes in disposable test fixtures.

## 3. Prepare an initial open-source release

The initial public release can remain a monitoring/navigation tool. Guarded checkout updates and richer review tracking are not prerequisites for opening the source.

### Identity and documentation

- [x] Select and integrate the refined blue jay logo, retaining source artwork and generation provenance.
- [ ] Verify icon appearance at small Dock sizes and on light/dark backgrounds across supported macOS versions.
- [x] Establish DevJay product naming, a private `teamconclude/DevJay` repository, and dedicated app storage and bundle identity.
- [ ] Confirm copyright attribution in the MIT license and review dependency/asset notices; include necessary notices in distributed app bundles.
- [ ] Add contribution guidance, issue/bug-report templates, a changelog, and instructions for reporting security issues privately.
- [ ] Keep onboarding generic: no Conclude account, repository, editor, or agent is required. Document optional GitHub authentication and current fork/Enterprise limitations.
- [ ] Document the private desktop-restoration API dependency, supported macOS versions, and recovery steps when restoration fails.

### Packaging and release checks

- [ ] Add CI for the existing Go, JavaScript, native geometry, and plist checks on macOS; verify a clean checkout builds using documented prerequisites.
- [ ] Decide the supported Apple Silicon/Intel matrix and verify the chosen targets on suitable machines.
- [ ] Prepare a versioned release archive or installer, Developer ID signing and notarization for public binaries, and a clean-machine/Finder launch test.
- [ ] Inspect tracked files and Git history for private material, secrets, machine-specific assumptions, and unintended generated files before publication.
- [ ] Verify a first-run user can add Sites, place their windows, see useful status, and navigate without assistance.
- [ ] Obtain explicit approval before making the repository public or publishing a release. Choosing MIT does not itself publish the repository.

Acceptance: a new user can build or install DevJay from the README; release limitations are explicit; the supported platform matrix and test record are available; the owner approves publication.

## 4. Improve GitHub review awareness

These are follow-on product features, not existing capabilities.

- [ ] Resolve PRs by host, base repository, head repository, and branch. Support fork PRs and GitHub Enterprise, repository/PR overrides, and ambiguous matches.
- [ ] Provide an accessible PR link and useful details: draft/open/merged/closed state, review decision, and whether the local checkout includes the PR head.
- [ ] Track **New feedback** from submitted reviews and comments using durable identifiers and a local acknowledgment baseline. Do not infer that the user must respond.
- [ ] Add an explicit **Mark seen** action. Initialize the baseline without marking an entire historical discussion unread; persist it across restart.
- [ ] Deduplicate polling for shared PRs and handle dismissed approvals, API failures, and rate limits.

Acceptance: tests cover fork matching, multiple candidate PRs, changed review decisions, new feedback, restart acknowledgment, and a local branch behind the reviewed commit. Reviewing, commenting, approving, and merging remain browser actions.

## 5. Add guarded checkout updates as a separate milestone

Implemented with explicit Review action and confirmation: fast-forward updates, return to the default branch after a merged PR, optional guarded local branch deletion, and conflict-checked rebases. Monitoring remains read-only apart from remote-tracking fetches. Rewritten review-branch replacement remains deferred.

### Fast-forward first

- [x] Permit an explicit update only for a clean checkout that is solely behind a freshly fetched, pinned upstream commit.
- [x] Recheck checkout identity, branch, upstream, HEAD, index, files, conflicts, and in-progress Git operations immediately before mutation.
- [x] Block staged/unstaged edits, untracked files, local commits, divergence, and incoming collisions with ignored files. Clean `git status` alone is not sufficient evidence.
- [x] Serialize app-owned operations by common Git directory. Account for external editors and Git clients; do not promise atomic protection from arbitrary external writes or unsaved editor buffers.
- [x] Keep unsupported/uncertain configurations disabled, including unverified sparse-checkout, submodule, and index-flag cases.
- [x] Verify actual resulting state after success, error, timeout, cancellation, or hook failure. Never retry or roll back blindly.

### Refresh rewritten review branches later

- [ ] Persist a last-known synchronized HEAD per checkout, branch, and upstream. Fetching alone must never advance this baseline.
- [ ] Allow refresh only for an explicitly designated review checkout whose current HEAD matches the known untouched baseline.
- [ ] Show the exact checkout and source/target commits and require confirmation. Create and verify a unique recovery reference before any branch change.
- [ ] Preserve upstream configuration and make recovery references discoverable after restart. Recovery must refuse to overwrite work added after the update.

No force-update override, automatic stash/pop, `git clean`, destructive hard-reset fallback, unprompted merge/rebase, or automatic push. A backup of committed history is not protection for uncommitted or ignored files.

Acceptance: use disposable repositories to assert preservation of branch/HEAD, index contents, working files, untracked/ignored files, symlink/file-directory collisions, and existing stashes. Cover normal and repeated force pushes, dirty states, failed backups, hooks, timeouts, disk errors, and external changes. Keep any update path disabled if its safety cannot be demonstrated.

## 6. Later enhancements

Consider these after the core experience is stable and user feedback supports them:

- Configurable global shortcuts and an optional menu-bar entry. Current shortcuts are app-local and follow card order.
- A compact details view for long status messages and freshness information.
- Optional automatic app updates after a reliable signed release process exists.
- Optional visibility limited to desktops containing Sites, if platform support proves reliable.

Out of scope for this roadmap: managing editors or agent sessions, creating Spaces automatically, running development servers/tunnels/proxies, becoming a full Git client, automatically merging or pushing, cross-platform UI, and Mac App Store distribution.

## Working rules

Preserve the existing user configuration and local work. Keep monitoring, navigation, and future Git mutation paths separate. Make changes in small reviewable increments, run relevant automated checks, and record native UI checks honestly. Do not mark a feature validated solely because it builds. Commit, push, and publish only when explicitly requested.
