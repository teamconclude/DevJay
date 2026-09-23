# DevJay

**A macOS control panel for parallel development with agents.**

DevJay was created by the [Conclude](https://conclude.io) team to make our parallel development workflow more effective. We work on multiple branches at the same time, often with separate coding-agent sessions, editors, and terminals spread across virtual desktops and monitors. Keeping track of which branch lives where—and whether it has local changes, new remote commits, or a reviewed pull request—takes attention away from the work itself.

DevJay brings those development contexts into one compact overview. It shows the state of each branch and lets us jump straight to the desktop where that work is happening.

## What is a Site?

We call each development context a **Site**. A Site is associated with a local folder, usually a Git clone or worktree checked out on a particular branch. Its desktop can contain whichever editor, terminals, browser windows, and agent sessions you use for that work.

Each Site has a small window that you place on the appropriate macOS desktop. The DevJay control panel shows a card for every Site, with its folder, current branch, and Git/GitHub status. Click a card to bring its Site window forward and switch to that desktop.

A Site follows its folder: if you check out another branch there, DevJay shows the new branch. Switching Sites takes you to an existing development context; it does not run a Git checkout or change your files.

## Why we built it

Our workflow might have one agent implementing a feature, another investigating a bug, and a third checkout open for reviewing a colleague’s changes. DevJay helps us:

- See which branch is checked out in each folder.
- Spot local edits, incoming commits, diverged branches, and pull-request review status.
- Move between development contexts without hunting through windows or repository lists.
- Keep a useful overview across multiple monitors and virtual desktops.

Although it grew out of Conclude’s workflow, DevJay is a general-purpose tool. It works with any local folder, Git clone, or Git worktree. GitHub integration is optional, and you can use any editor or coding agent—or no agent at all.

## Get started

1. Install DevJay from a disk image, or build and run it using the instructions below.
2. Open Settings with **⌘,**. Click **Add**, or drop a folder into the Sites list.
3. Place the Site window on the desktop where you work on that checkout.
4. Repeat for your other development contexts.
5. Use the control panel to check branch status and switch between Sites.

## Install

Open the DevJay `.dmg`, quit any running copy, and drag **DevJay.app** onto **Applications**. Launch it from Applications, then eject the disk image. Replacing an older app preserves your Sites and preferences. macOS 13 or newer is required; the universal installer supports Apple Silicon and Intel Macs. Go and Node are not needed to run the installed app. Git is required for repository status; GitHub PR information also requires `gh` and your existing sign-in.

To create an installer locally:

```sh
make installer
```

This builds both architectures in a temporary directory, leaving the development app in `bin/` untouched, and produces `dist/DevJay-0.1.0-macos-universal.dmg` with a SHA-256 checksum. The version comes from `build/Info.plist`. To build for one architecture, use `make installer ARCH=arm64` or `make installer ARCH=amd64`. Packaging verifies the disk image, mounts it read-only, and checks its application signature, architectures, and Applications link.

These development installers use an ad-hoc signature and are not Apple-notarized. macOS may block downloaded copies. Public distribution still requires Developer ID signing and Apple notarization.

## Run

Requires macOS 13 or newer, Go 1.25 or newer, and Xcode Command Line Tools. Git is required for repository status. GitHub PR status optionally uses `gh` with your existing sign-in. Node is needed for JavaScript checks and tests. No npm installation or Wails CLI is required.

```sh
make run
```

This builds an ad-hoc-signed `bin/DevJay.app`. Quit any running copy before rebuilding.

## Sites and Settings

Open Settings and drop a folder from Finder into the Sites list. Any folder is accepted; Git clones and worktrees get branch/status information. Symlink aliases of an existing Site are rejected. Site registration never creates, deletes, or checks out a repository. The **×** beside a Site unregisters it and closes its window, leaving its folder unchanged.

**Window decoration** offers **None** (default, content only), **Minimal** (borderless with small controls on both the panel and Sites), and **Normal** (native macOS title bars). The choice applies immediately to the panel and all Sites and is saved between runs. Settings uses a normal macOS title bar and fits its content height when opened, up to the available screen height. Escape and the native close button close it. Hover a card and click its top-right cog, or press Command-comma, to open Settings. Hover tracking also works while DevJay is inactive, without taking focus from another app. Right-clicking no longer opens Settings. Click a panel card, above or below the divider, to activate its Site. Settings, other controls, and selecting status text retain their own behavior. With Settings closed, drag that heading to move the whole panel. Drag an independent Site by its heading; the cog and status area do not move it. Place each Site window on its desired monitor and desktop using Mission Control. Choose **By name** to sort by directory name (case-insensitive, with the full path breaking ties), or **Manual** to use the saved custom order. Drag a row by its dotted handle to reorder; this switches to Manual. Keyboard users can focus the handle and press Up or Down. The selected order also controls the main panel.

Cards show a subdued parent path, bold directory name, branch, local edits, upstream comparison, and GitHub pull-request review decision when available. Hover a card for its full path, desktop, upstream, and last successful remote-check time. The strip can be resized and scrolls horizontally when needed. Cards show their full status content; Site windows and the control panel grow taller when necessary, preserving their width and bottom-edge placement. They keep the extra space when status becomes shorter. If the display cannot fit all content, scrolling is available in the outer window or card list.

**Control panel always visible** keeps the main window on ordinary desktops on its monitor; it does not duplicate it across monitors. **Show all windows on top** floats all DevJay windows. Both default to enabled, persist between runs, and remain available in the View menu. Settings is a separate window; closing it or pressing Escape leaves DevJay running.

The Sites menu provides Command-1 through Command-9 for the current card order, and Command-0 for DevJay. These are app shortcuts, not global hotkeys. Command-Q quits DevJay when the app is active and saves the layout; clicking its non-activating control panel alone may leave keyboard shortcuts with the previous app. The panel’s × button is available in Minimal decoration, and the native close button is available in Normal decoration. None hides these controls; activate a Site or Settings to use Command-Q. Remove Sites in Settings; Site windows have no close controls. Selecting a minimized Site restores it.

For navigation, enable System Settings → Desktop & Dock → Mission Control → **When switching to an application, switch to a Space with open windows for the application** if needed. Keep the app's Dock assignment at **None** so windows can occupy different Spaces. No Accessibility permission or editor integration is required.

## Control panel layouts

Settings offers **Horizontal** (default), **Vertical**, and **Custom**. Custom initially wraps cards into tiles as the window is resized. Only while Settings is open, drag a card by its heading: Horizontal and Vertical reorder the cards; Custom saves free positions on an invisible 8-pixel grid and allows overlap. Moving a card brings it above overlapping cards. **Reset card positions** in Settings returns Custom to automatic tiling. Layout and custom positions survive restarts. Dragging while Settings is open switches sorting to Manual. Status text remains selectable for copying; drag the panel background to move the window while rearranging cards.

## Inspect changes

Card status texts highlight on hover, even in inactive windows. Click **In sync**, another status, or a card’s **edits**, **local commits**, or **remote commits** count to open a read-only changes window, from either the control panel or an independent Site. Local edits are grouped into staged, unstaged, and untracked files. Select a file to see its unified diff, with line numbers and colored additions and deletions. Available suggested actions appear inside the viewer and update with the current checkout. Up to three direct action buttons appear, with the recommended action on the right and secondary choices subdued. Clicking an action such as **Push 3 commits** starts safety checks and opens **Confirm push**; a separate confirmation is required to apply it. Opening a viewer never changes the checkout. Every click opens an independent viewer, including multiple views of the same Site. Each window keeps its own selected commit, file, and snapshot. The file summary and staged/unstaged/untracked section headings show file counts and total added/removed lines. The overall count includes each path once; line totals sum staged and unstaged changes separately. Binary changes are excluded from line counts, and unavailable or limited counts are labeled partial. The viewer does not stage, commit, or change files.

Select a file and click **Open in GoLand** (or your chosen editor) beside its diff. In **Settings → Editor**, choose one preferred editor for all Sites. Enable **Automatically use the editor with this project open** to prefer a detected project editor, falling back to your preferred editor when no unique match is available. Installed editors and **Choose another application…** are available there. Project detection recognizes a running JetBrains IDE reporting that exact checkout open, using its optional discovery metadata and verifying the live application. If detection is disabled, DevJay always uses the preferred editor. macOS asks the existing application to open the file; project-window routing is handled by that editor and is best effort. No Accessibility permission is needed. Opening always uses the current working-tree file, even while viewing an older commit. Deleted files and links outside the checkout cannot be opened from the viewer.

The local-commits view shows commits not yet in the upstream history. **Remote commits** shows the full upstream history, in pages of 100, with each commit labeled **Incoming** or **Already local** relative to the captured local branch. These labels compare commit IDs, so rebased equivalents can still be marked incoming. Select a commit, then a file, to inspect its changes. Merge commits are compared with their first parent. **Rebased · ready to publish** also opens the viewer; its tabs let you inspect both sides of rewritten history.

**Refresh** reloads the comparison from local Git data; background monitoring fetches remote updates separately. Commit comparisons stay pinned to their captured revisions. Local file contents are read when selected, so refresh the file list after editing. Binary files show a notice, and large file lists and diffs have explicit display limits. Escape or the native close button closes the viewer.

## Git monitoring

Local status is polled every 3 seconds. Local commands have four reserved slots, separate from the two network-command slots. Failed local readings preserve the last complete status and PR findings with an explicit cached-status warning, without forgetting the upstream or retry schedule. Remote and GitHub checks run in the background about once a minute, independently of local polling, with at least a minute between completed attempts and retries. Status distinguishes no upstream, detached HEAD, an empty repository, local changes, merge conflicts, ahead, behind, diverged, and a removed upstream branch. Non-Git and unavailable folders remain usable Sites with an explanatory status.

Automatic monitoring fetches the configured upstream and the remote default branch into their remote-tracking refs, with tags, submodule recursion, maintenance, and configured extra ref mappings disabled. Monitoring never changes checked-out files or local branches. Offline/authentication failures retain cached comparisons and label them explicitly; they are never represented as a fresh “Up to date.” When a feature branch has moved onto a newer default-branch base and every remote-only commit has an equivalent local patch, the card shows **Rebased · ready to publish** instead of misleading incoming/outgoing counts. Commit counts remain in the tooltip. Unverified remote status is labeled explicitly; genuinely new remote patches or unverified merge commits retain normal divergence warnings. Publishing rebased history remains manual with force-with-lease; DevJay does not suggest pulling the old history. A non-fast-forward change to the observed remote-tracking ref is flagged as rewritten history. Rewrites that occurred before the first observation or were fetched by another tool between checks cannot always be identified.

Suggested actions appear in a Site’s changes window when its checkout is at the exact tip of a merged PR, has local commits ahead of origin, has incoming upstream commits, or is missing commits from the remote default branch. The compact cards have no action buttons; click a status to open the changes window. Return-to-default suggestions require an exact repository, branch-name, and merged-tip match with no staged, unstaged, or untracked edits. Reusing a previously merged branch name does not qualify. Click the specific action to fetch and check the exact source and destination commits, then explicitly confirm **Update branch**, **Return to the default branch**, **Rebase branch**, or **Push commits**. Confirmations expire after five minutes and cannot be reused. Cancel, ×, and Escape discard pending confirmation. Returning after a merge offers optional local branch deletion in the confirmation dialog; changing that option repeats the safety checks. Once an operation is applying, closing waits until it finishes; quitting also waits for the bounded operation.

- Updates only fast-forward a clean checkout to its confirmed upstream commit. Divergence requires manual handling.
- Returning after a merge switches to the repository’s default branch and fast-forwards it, creating it with remote tracking if absent. Both returning and optional deletion require the old branch tip to match a PR merged into that default branch. Deletion happens only after a successful switch and update; another worktree using the branch blocks it.
- Rebases are tested in a disposable repository against the latest fetched default branch, preserving merges. Conflicts show the files at the first stop and recommend a manual rebase. Confirmation repeats the trial and installs only its conflict-free result, using your Git committer identity. The original branch moves with an expected-old-commit check, and no rebase is left in progress in the Site. No repository hooks, custom merge drivers, filters, or saved conflict resolutions run in the trial. Repositories requiring signed commits must be rebased manually.

A normal push is suggested for local-only commits with an existing same-name origin upstream. Review fetches origin and verifies that its tip is an ancestor of the local branch; confirmation pins the source commit and destination, rechecks both, and pushes only that branch without force or tags. Different/multiple push URLs require a manual push. Remote changes, divergence, or local edits stop the action. The result is checked against the remote even after a command error or timeout, with no automatic retry. Rebased history still requires manual publishing.

Actions recheck checkout identity, HEAD, branch, remote, clean index/files, and Git operation state. They refuse staged/unstaged/untracked work, hidden index flags, sparse checkouts, and submodules. Git’s no-overwrite-ignore behavior protects incoming collisions with ignored files. Actions are serialized per common Git directory; they cannot provide atomic protection against arbitrary concurrent writes by other tools or unsaved editor buffers. No automatic stash/pop, destructive reset, clean, or push is performed. Errors report the observed checkout state rather than attempting a blind rollback. If an external change interrupts the rebase’s branch reattachment, the message explicitly explains the detached checkout and manual next step.

Before updating, rebasing, or switching a local branch, DevJay preserves the original commits at **refs/devjay/backups/<action-id>**. The result window displays that reference. List retained recovery points with `git for-each-ref refs/devjay/backups/`; create a separate recovery branch with `git branch recovered-work <recovery-ref>`. These references survive restart and are not automatically deleted. They preserve commits, not uncommitted or ignored files.


For GitHub.com remotes, `gh pr list` supplies open PR number, draft state, and review decision. DevJay also reads all pages of submitted reviews and PR discussion comments, showing blocker and warning counts from the latest recognizable report. It accepts explicit `<!-- review blockers=1 warnings=7 ... -->` metadata or Markdown Blockers/Warnings sections with top-level list items (or “None”). Counts are reported findings, not an inferred GitHub approval decision. GitHub approval takes precedence over “No findings” and stays visible alongside any reported warnings or blockers; the tooltip retains the report date and URL. Reports for an earlier PR commit keep their counts visible with **Changes pushed since review**, and unavailable posts are labeled **Review unavailable**. When no open PR exists, DevJay checks the latest 20 merged PRs for the same repository and branch. Only a checkout at the merged PR’s exact tip can show **Merged · Branch left over after merge**. A different tip has no association with the old PR. When the local reflog retains the branch’s creation record, a creation date after the merge also rules out the old PR, even at the same commit. Recreating a branch invalidates its cached GitHub status. If creation history is unavailable or expired, DevJay requires the exact tip and never infers a relationship from the name or shared ancestry alone. Local edits remain visible, and this is not a claim that deleting the branch is safe. Default branches do not show this leftover status. Dismissed and pending reviews are excluded. Ordinary discussion does not replace a report, and findings are not summed across repeated reviews. Missing CLI/authentication or network failures show **GitHub unavailable**. This version does not track individual unread comments or provide a review inbox. GitHub Enterprise and fork PRs targeting another repository are not resolved yet. Commands have bounded duration, output, and concurrency; interactive authentication prompts are disabled.

## Persistence and desktop APIs

Sites and custom order are stored atomically in `~/Library/Application Support/DevJay/sites.json`. A malformed configuration is reported and never silently overwritten. Window positions, display identity, desktop UUID, and appearance preferences are stored in macOS preferences under the `io.teamconclude.devjay` bundle identifier. Each folder has a stable identifier, so removing and re-adding it recovers its previous placement.

The control panel remembers its size. Windows within 10% of the usable screen width or height of the nearer right or bottom edge preserve their gap in points when the resolution changes. Restored windows are clamped 8 points inside the usable display area. Missing displays fall back to the nearest available screen. Desktop identification and restoration use runtime-loaded private SkyLight APIs. Failed restores preserve the saved desktop assignment until the Site is moved; removed desktops or unavailable APIs leave windows accessible. These undocumented APIs may change with macOS updates. The main window skips desktop restoration when shown on all desktops.

## Recovering blank windows

After display/system wake or returning to the login session, DevJay reloads its web views once the display has settled. WebContent process termination also triggers recovery if WebKit's own reload does not restore a rendered snapshot. Reloads preserve native window positions, visibility, and backend Git operations; they do not repeat or confirm an action. Transient UI state such as text selection and scroll position may reset.

Recovery waits for fresh frontend state and retries at most three times per incident, with a rate limit for repeated crashes. Regular snapshots are paused while the display or session sleeps. If windows remain blank, use **View → Reload windows** to retry without quitting. Recovery events are logged with the prefix `UI recovery:`. A persistent macOS GPU failure can still require restarting DevJay or macOS.

## Checks

```sh
make check
make build
```

Checks cover window-clamping geometry, Site persistence and sorting, Git status parsing, and disposable local repositories exercising remote commits, divergence, force pushes, deleted branches, and preservation of edited/untracked files and local HEAD. No tests modify development repositories or contact GitHub.

Manual checks: add Git and plain folders; drag and reset ordering; click cards after another window has focus; minimize and restore a Site; move Sites between monitors/Spaces; quit/reopen and verify placement and order; remove a Site and confirm its folder remains.

Implementation: Go with pinned Wails v3 beta.23, embedded HTML/CSS/JavaScript, and an AppKit/SkyLight bridge. The broader project plan is in [PLAN.md](PLAN.md).

## Notes

The project is also referred to as **DJ**.

## License

DevJay is licensed under the [MIT License](LICENSE).
