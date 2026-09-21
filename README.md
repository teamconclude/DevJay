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

1. Build and run DevJay using the instructions below.
2. Open Settings with **⌘,**. Click **Add**, or drop a folder into the Sites list.
3. Place the Site window on the desktop where you work on that checkout.
4. Repeat for your other development contexts.
5. Use the control panel to check branch status and switch between Sites.

## Run

Requires macOS 13 or newer, Go 1.25 or newer, and Xcode Command Line Tools. Git is required for repository status. GitHub PR status optionally uses `gh` with your existing sign-in. Node is needed for JavaScript checks and tests. No npm installation or Wails CLI is required.

```sh
make run
```

This builds an ad-hoc-signed `bin/DevJay.app`. Quit any running copy before rebuilding.

## Sites and Settings

Open Settings and drop a folder from Finder into the Sites list. Any folder is accepted; Git clones and worktrees get branch/status information. Symlink aliases of an existing Site are rejected. Site registration never creates, deletes, or checks out a repository. The **×** beside a Site unregisters it and closes its window, leaving its folder unchanged.

**Window decoration** offers **None** (default, content only), **Minimal** (borderless with small controls on both the panel and Sites), and **Normal** (native macOS title bars). The choice applies immediately to the panel and all Sites and is saved between runs. Settings retains its native title bar. Hover a card and click its top-right cog, or press Command-comma, to open Settings. Hover tracking also works while DevJay is inactive, without taking focus from another app. Right-clicking no longer opens Settings. Click the folder/branch heading above the divider in the panel to activate its Site. With Settings closed, drag that heading to move the whole panel. Drag an independent Site by its heading; the cog and status area do not move it. Place each Site window on its desired monitor and desktop using Mission Control. Choose **By name** to sort by directory name (case-insensitive, with the full path breaking ties), or **Manual** to use the saved custom order. Drag a row by its dotted handle to reorder; this switches to Manual. Keyboard users can focus the handle and press Up or Down. The selected order also controls the main panel.

Cards show a subdued parent path, bold directory name, branch, local edits, upstream comparison, and GitHub pull-request review decision when available. Hover a card for its full path, desktop, upstream, and last successful remote-check time. The strip can be resized and scrolls horizontally when needed.

**Control panel always visible** keeps the main window on ordinary desktops on its monitor; it does not duplicate it across monitors. **Show all windows on top** floats all DevJay windows. Both default to enabled, persist between runs, and remain available in the View menu. Settings is a separate window; closing it or pressing Escape leaves DevJay running.

The Sites menu provides Command-1 through Command-9 for the current card order, and Command-0 for DevJay. These are app shortcuts, not global hotkeys. Command-Q quits DevJay when the app is active and saves the layout; clicking its non-activating control panel alone may leave keyboard shortcuts with the previous app. The panel’s × button is available in Minimal decoration, and the native close button is available in Normal decoration. None hides these controls; activate a Site or Settings to use Command-Q. Remove Sites in Settings; Site windows have no close controls. Selecting a minimized Site restores it.

For navigation, enable System Settings → Desktop & Dock → Mission Control → **When switching to an application, switch to a Space with open windows for the application** if needed. Keep the app's Dock assignment at **None** so windows can occupy different Spaces. No Accessibility permission or editor integration is required.

## Control panel layouts

Settings offers **Horizontal** (default), **Vertical**, and **Custom**. Custom initially wraps cards into tiles as the window is resized. Only while Settings is open, drag a card by its heading or dotted move handle: Horizontal and Vertical reorder the cards; Custom saves free positions on an invisible 8-pixel grid and allows overlap. Moving a card brings it above overlapping cards. **Reset card positions** in Settings returns Custom to automatic tiling. Layout and custom positions survive restarts. Dragging while Settings is open switches sorting to Manual. Status text remains selectable for copying; drag the panel background or its grip to move the window while rearranging cards.

## Git monitoring

Local status is polled every 3 seconds. Local commands have four reserved slots, separate from the two network-command slots. Failed local readings preserve the last complete status and PR findings with an explicit cached-status warning, without forgetting the upstream or retry schedule. Remote and GitHub checks run in the background about once a minute, independently of local polling, with at least a minute between completed attempts and retries. Status distinguishes no upstream, detached HEAD, an empty repository, local changes, merge conflicts, ahead, behind, diverged, and a removed upstream branch. Non-Git and unavailable folders remain usable Sites with an explanatory status.

DevJay fetches only the configured upstream into its remote-tracking ref, with tags, submodule recursion, maintenance, and configured extra ref mappings disabled. It never pulls, resets, merges, stashes, stages, or changes checked-out files or local branches. This version has no Git action buttons. Offline/authentication failures retain cached comparisons and label them explicitly; they are never represented as a fresh “Up to date.” A non-fast-forward change to the observed remote-tracking ref is flagged as rewritten history. Rewrites that occurred before the first observation or were fetched by another tool between checks cannot always be identified.

For GitHub.com remotes, `gh pr list` supplies open PR number, draft state, and review decision. DevJay also reads all pages of submitted reviews and PR discussion comments, showing blocker and warning counts from the latest recognizable report. It accepts explicit `<!-- review blockers=1 warnings=7 ... -->` metadata or Markdown Blockers/Warnings sections with top-level list items (or “None”). Counts are reported findings, not an inferred GitHub approval decision. GitHub approval takes precedence over “No findings” and stays visible alongside any reported warnings or blockers; the tooltip retains the report date and URL. Reports for an earlier PR commit keep their counts visible with **Changes pushed since review**, and unavailable posts are labeled **Review unavailable**. Dismissed and pending reviews are excluded. Ordinary discussion does not replace a report, and findings are not summed across repeated reviews. Missing CLI/authentication or network failures show **GitHub unavailable**. This version does not track individual unread comments or provide a review inbox. GitHub Enterprise and fork PRs targeting another repository are not resolved yet. Commands have bounded duration, output, and concurrency; interactive authentication prompts are disabled.

## Persistence and desktop APIs

Sites and custom order are stored atomically in `~/Library/Application Support/DevJay/sites.json`. A malformed configuration is reported and never silently overwritten. Window positions, display identity, desktop UUID, and appearance preferences are stored in macOS preferences under the `io.teamconclude.devjay` bundle identifier. Each folder has a stable identifier, so removing and re-adding it recovers its previous placement.

The control panel remembers its size. Windows within 10% of the usable screen width or height of the nearer right or bottom edge preserve their gap in points when the resolution changes. Restored windows are clamped 8 points inside the usable display area. Missing displays fall back to the nearest available screen. Desktop identification and restoration use runtime-loaded private SkyLight APIs. Failed restores preserve the saved desktop assignment until the Site is moved; removed desktops or unavailable APIs leave windows accessible. These undocumented APIs may change with macOS updates. The main window skips desktop restoration when shown on all desktops.

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
