# Agent quick start

DevJay is a macOS strip for parallel development. A **Site** is a local folder plus an app-owned window on a macOS desktop. Sites follow folders, not branches. Navigation activates windows; Git changes are separate, explicitly confirmed actions.

## Read first

- [README.md](README.md): human onboarding and build prerequisites.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): source map, state flow, safety boundaries, storage, and validation.
- [docs/USER_GUIDE.md](docs/USER_GUIDE.md): detailed current behavior and recovery instructions.
- [PLAN.md](PLAN.md): remaining work and validation gaps, not a description of current capabilities.

## Working in this repository

- Application sources and Go tests live in `cmd/devjay/`, including its embedded `frontend/` assets. Native/JavaScript test harnesses live in `tests/`; module files and build commands remain at the root.

- Stack: Go 1.27.1+, pinned Wails v3 beta.23, embedded vanilla JavaScript/CSS, and Objective-C/AppKit bridges. Build on macOS 13+ with Xcode Command Line Tools; Node is needed for checks. No npm install or Wails CLI is required.
- `make check` runs Go tests/vet, JavaScript tests/syntax checks, native checks, and plist validation. `make build` creates an ad-hoc-signed development app; `make run` launches it. Packaging details are in the architecture doc.
- Preserve the separation between background monitoring, window navigation, and confirmed Git mutations. Never add automatic stash/pop, destructive reset/clean, unconditional force push, or blind rollback/retry paths. Rebased publication, including a combined rebase-and-push action, requires explicit confirmation and a remote-SHA lease after conflict and history-preservation checks.
- Keep manager state changes on its command queue, AppKit work on the main thread, and slow Git/editor discovery off the UI thread. Reject stale asynchronous results and validate the sending window for UI requests.
- Preserve user configuration and local work. Tests use disposable repositories; do not exercise destructive cases against a user's checkout. Development and installed builds share configuration and one single-instance identity. Quit the running copy before switching builds; tests must use disposable configuration.
- Run checks appropriate to the change. For native behavior, record what was actually tested; compilation and geometry tests do not prove multi-monitor/Space behavior.
- Keep docs aligned when behavior changes. Commit, push, and publish only when requested.
