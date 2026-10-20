# DevJay

**A macOS strip for parallel development with agents.**

Created by the [Conclude](https://conclude.io) team, DevJay brings your development folders into one compact overview. See branch status, local changes, incoming commits, and GitHub reviews, then jump to the desktop where the work is happening. Use any editor or coding agent; GitHub integration is optional.

DevJay was initially developed with GPT-6 Astra. The agent wrote all the code, with a firm hand from me directing the work through several iterations of code reviews, testing, and usability testing.

## What is a Site?

A **Site** is a local folder—usually a Git clone or worktree—with a small DevJay window that you place on its macOS desktop. Click its card in the strip to switch to that desktop.

A Site follows its folder: checking out another branch updates the card. Switching Sites activates a window without changing branches or files. Any local folder can be a Site.

Registered Sites also follow recognized folder renames and moves. If a folder cannot be found, **Settings → Sites → Locate…** reconnects it while keeping its desktop, position, and card order.

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
4. Repeat for your other folders, then click strip cards to move between them.

In Settings, choose a horizontal, vertical, or custom layout, adjust window appearance, and select your preferred editor. Drag cards by their headings while Settings is open to rearrange them. Removing a Site unregisters it after confirmation and leaves its folder untouched.

Settings has sidebar pages for **Sites**, **Profiles**, **IDEs & editors**, **Windows and layout**, and **Keyboard shortcuts**. Profiles remember arrangements for monitor combinations such as Office, Home, and Laptop, and can switch automatically when displays change. Use **Apply changes to** to edit one or several Profiles. See the [Profile guide](docs/USER_GUIDE.md#profiles-and-settings-pages) for setup and placement recovery. Keyboard shortcuts lists the global bindings and lets you record custom combinations or restore defaults.

## Check your work

Click a card’s status or change count to inspect file diffs and a single branch history in its existing changes window. The **Changes** sidebar lists each uncommitted file once. **History** combines local and remote commits, marking **Not pushed** and **Incoming** work and keeping shared history quiet. Right-click for Settings or, when that Site already has a changes window, to open another window. Folder and branch menus choose Changes when edits exist and History otherwise; PR menus explicitly open a browser. Click an IDE/terminal icon, or choose its first menu action, to switch to that Site’s desktop and show the running app. Changes windows refresh automatically. From the changes window, open a file in your editor or review an available Git action: update, rebase and push, push existing commits, return after a merge, or replace verified rewritten remote history. Each action checks the checkout and requires a separate confirmation. Push allows local edits and publishes only committed work. **Update <branch>** fetches and fast-forwards only a clean checkout, including default branches such as main.

In the Changes window, click **Switch...** beside the branch name to choose another branch. Switching requires a clean checkout and a separate confirmation. If the destination has a newer remote version with no local-only commits, DevJay offers to use it; branches with unpublished or diverged commits keep their local version.

Right-click an incoming count for **Fetch and update <branch>…**, or an outgoing count for **Push N commits…**. Card and branch menus also suggest the relevant rebase, rebased push, merged-branch cleanup, or verified replacement from rewritten remote history. Each uses the same safety checks and separate confirmation as the Changes window.

Approved and merged PR tags are green; failing CI takes priority in red. Click the colored PR tag in a card’s upper-right corner to open its pull request. DevJay can reuse tabs in Safari, Chrome, Edge, and Brave with macOS Automation permission; right-click the tag to open a new tab alongside existing PRs.

When a PR is authored by someone else, their GitHub avatar appears at the card’s bottom-right. Hover for their username; click to open the PR. Your own PRs have no avatar.

Background monitoring fetches remote information without changing your local branches or working files. Offline or failed checks show cached/unavailable status. GitHub support currently covers GitHub.com; fork PRs targeting another repository and GitHub Enterprise are not yet resolved.

## Useful controls

- Right-click the strip and choose **Minimize strip** for a shallow row of clickable Site names, or **Hide strip** to hide it completely. **⌃⌥⌘D** hides/shows the strip; **⌃⌥⌘S** hides/shows Site windows. Their hidden state survives restarts and desktop navigation. **⌃⌥⌘M** toggles the compact row globally; **⌃⌥⌘0** restores the full strip.
- **⌃⌥⌘1–9** visits Site desktops in card order from any app (Control–Option–Command plus a number). Hold the modifiers first to see numbered hints; a hidden strip briefly shows its row until you release them.
- **⌘1–9** selects Sites in card order; **⌘0** shows DevJay. These shortcuts work while the app is active.
- **⌘Q** quits and saves the layout. Activate a Site or Settings first if the strip has left keyboard focus in another app.
- If desktop switching does not work, enable **System Settings → Desktop & Dock → Mission Control → When switching to an application, switch to a Space with open windows for the application**, and keep DevJay’s Dock assignment at **None**.
- If a window stays blank after wake, choose **View → Reload windows**.

Desktop restoration uses private macOS APIs and may be affected by OS updates. No Accessibility permission or editor integration is required.

## More information

- [User guide](docs/USER_GUIDE.md): settings, Git actions, recovery, and saved data.
- [Development and architecture](docs/ARCHITECTURE.md): source map, checks (`make check`, with Node installed), and installer builds (`make installer`).
- [Agent quick start](AGENTS.md) and [remaining work](PLAN.md).

DevJay is licensed under the [MIT License](LICENSE).
