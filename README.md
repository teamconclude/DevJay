# DevJay

**A macOS control panel for parallel development with agents.**

Created by the [Conclude](https://conclude.io) team, DevJay brings your development folders into one compact overview. See branch status, local changes, incoming commits, and GitHub reviews, then jump to the desktop where the work is happening. Use any editor or coding agent; GitHub integration is optional.

DevJay was initially developed with GPT-6 Astra. The agent wrote all the code, with a firm hand from me directing the work through several iterations of code reviews, testing, and usability testing.

## What is a Site?

A **Site** is a local folder—usually a Git clone or worktree—with a small DevJay window that you place on its macOS desktop. Click its card in the control panel to switch to that desktop.

A Site follows its folder: checking out another branch updates the card. Switching Sites activates a window without changing branches or files. Any local folder can be a Site.

## Install or build

DevJay requires **macOS 13 or newer**. Git is needed for repository status; GitHub PR information also needs the GitHub CLI (`gh`) and your existing sign-in.

If you have a DevJay `.dmg`, quit any running copy and drag **DevJay.app** into **Applications**. Open it from Applications, then eject the disk image. Updates preserve your Sites and preferences. The universal installer contains Apple Silicon and Intel builds; Go and Node are not needed to run it.

Current installers are ad-hoc signed and not Apple-notarized. If macOS blocks a downloaded copy, attempt to launch it, then use **System Settings → Privacy & Security → Open Anyway**. Updates may ask for folder permissions again. See [Apple’s guidance](https://developer.apple.com/news/?id=saqachfa).

To build from a checkout, install **Go 1.27.1+** and **Xcode Command Line Tools**, then run:

```sh
make run
```

This builds and opens `bin/DevJay.app`. Development builds use separate Sites and preferences, so they can run beside an installed copy. Quit the development app before rebuilding. No npm installation or Wails CLI is needed.

## Get started

1. Open **Settings** with **⌘,**, or right-click a card and choose **DevJay settings…**.
2. Click **Add**, or drop a folder into the Sites list.
3. Use Mission Control to place its Site window on the desktop where you work.
4. Repeat for your other folders, then click control-panel cards to move between them.

In Settings, choose a horizontal, vertical, or custom layout, adjust window appearance, and select your preferred editor. Drag cards by their headings while Settings is open to rearrange them. Removing a Site unregisters it after confirmation and leaves its folder untouched.

## Check your work

Click a card’s status or change count to inspect file diffs and commit history in its existing changes window. Right-click for a new window or Settings. Changes windows refresh automatically. From the changes window, open a file in your editor or review an available Git action: update, rebase, push, return after a merge, or replace verified rewritten remote history. Each action checks the checkout and requires a separate confirmation.

In the Changes window, click **Switch branch…** beside the branch name to choose another branch. Switching requires a clean checkout and a separate confirmation. If the destination has a newer remote version with no local-only commits, DevJay offers to use it; branches with unpublished or diverged commits keep their local version.

Approved PR tags are green; failing CI takes priority in red. Click the colored PR tag in a card’s upper-right corner to open its pull request. DevJay can reuse tabs in Safari, Chrome, Edge, and Brave with macOS Automation permission; right-click the tag to open a new tab alongside existing PRs.

Background monitoring fetches remote information without changing your local branches or working files. Offline or failed checks show cached/unavailable status. GitHub support currently covers GitHub.com; fork PRs targeting another repository and GitHub Enterprise are not yet resolved.

## Useful controls

- **⌘1–9** selects Sites in card order; **⌘0** shows DevJay. These shortcuts work while the app is active.
- **⌘Q** quits and saves the layout. Activate a Site or Settings first if the control panel has left keyboard focus in another app.
- If desktop switching does not work, enable **System Settings → Desktop & Dock → Mission Control → When switching to an application, switch to a Space with open windows for the application**, and keep DevJay’s Dock assignment at **None**.
- If a window stays blank after wake, choose **View → Reload windows**.

Desktop restoration uses private macOS APIs and may be affected by OS updates. No Accessibility permission or editor integration is required.

## More information

- [User guide](docs/USER_GUIDE.md): settings, Git actions, recovery, and saved data.
- [Development and architecture](docs/ARCHITECTURE.md): source map, checks (`make check`, with Node installed), and installer builds (`make installer`).
- [Agent quick start](AGENTS.md) and [remaining work](PLAN.md).

DevJay is licensed under the [MIT License](LICENSE).
