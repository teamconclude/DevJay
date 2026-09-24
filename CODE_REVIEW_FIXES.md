# Claude review follow-up

Applied to `a1751ad`, including the replacement action added after the reviewed `7811d95`. The original `CODE_REVIEW.md` is preserved.

| Findings | Resolution |
| --- | --- |
| M1, M5 | Start command timeouts after queuing. Reserve action capacity, serialize fetches by common Git directory, and let local mutations finish without deadline cancellation. Remove the overall apply timeout. |
| M2 | Preserve the environment's SSH command or the repository's effective `core.sshCommand`, including configuration includes, with noninteractive options. The rebase trial uses the source repository's SSH identity for its fetch. |
| M3, M4 | Clear obsolete rewrite warnings once the upstream is contained locally, while preserving the newer verified-replacement behavior. Compare Git's own shortened upstream spelling so ambiguous ref names work. |
| M6 | Defer quit through `ShouldQuit`, display a waiting message, reject new actions, and wait outside AppKit before requesting quit again. |
| M7 | Cache editor discovery for 30 seconds, refresh it in Settings, and enumerate applications outside the main thread. Copy cached data before annotating a viewer's project matches. |
| M8, L17 | Reuse a Changes window for the same Site and mode. Replace closed confirmation parents with the Site window or panel, with a native screen-center fallback. |
| L1, L2 | Determine push success from the remote SHA. Treat edits after a successful local update as a note. Allow rebasing when the branch's upstream is the default branch itself. |
| L3 | Apply first-mouse handling to a runtime subclass of the panel's own view; confirmation views and base AppKit classes remain unaffected. |
| L4 | Display bridge errors in every view, acknowledge action requests from Go, time out missing replies, restore failed buttons, and clear errors after successful requests. |
| L5, L6 | Render the latest snapshot when dragging ends and preserve Site-row focus across keyboard reordering. |
| L7, L8 | Confirm Site removal. Check busy state and save successfully before closing viewers. Broadcast authoritative configuration after failed order/sorting saves. |
| L9 | Enable single-instance handling. Give development builds a separate bundle ID, instance lock, and Sites configuration. |
| L10 | Retain the ten latest verified successful recoveries; preserve failed, partial, and legacy backups. Completed markers distinguish safe pruning candidates; compare-and-swap deletion protects changed refs. Legacy backups are intentionally retained because their action outcome is unknown. |
| L11, L12 | Stream tracked-file/index-flag checks instead of collecting the entire index. Disable hooks for background fetches. |
| L13 | Cancel the previous branch's refresh when its cache identity changes; discard results from obsolete refresh generations. |
| L14 | Show an explicit GitHub-unavailable note for unrecognized SSH hosts, including aliases. Alias resolution is not attempted; this follows the report's fallback option. |
| L15, L16 | Reject invalid UTF-8 native paths/identifiers before constructing URLs or reading layout. Restore the original desktop compatibility tag after moving a window; use the fallback if tag-reading APIs are absent. |
| L18 | Explain ad-hoc signature privacy resets and the macOS 15+ Open Anyway flow in the README and installer text, linking Apple's guidance. |
| T1–T3, dead code | Test busy actions with otherwise eligible Sites; exercise native hover exit; remove the unused desktop-order parameter, standalone rebase-check UI/handler, and unreachable merged-status branches. |

## Validation

- Full Go race suite passed. macOS deployment flags were set to 13.0 and the Go build cache was placed in `/tmp` for the sandbox.
- `make check` passed: Go tests and vet, 57 JavaScript tests, native geometry/power/review checks, syntax validation, and plist validation.
- A development app was built and ad-hoc signed in `/tmp/devjay-review-build/DevJay.app`.
- Replayed the queue-timeout, mutation-deadline, ambiguous-upstream, autosave, and default-tracking rebase regressions against the original checkout. All failed there and pass with these changes.
- Corrected the pre-existing rebase-preview test to detect DevJay temporary-checkout leaks without rejecting Apple's unrelated `xcrun_db` cache file.

The native checks exercise first-mouse isolation, invalid paths, and tag restoration without moving real windows or launching editors. Slow-network quitting and live desktop/window behavior have not been exercised end to end in an interactive app session.

## Notice regression follow-up

Claude's follow-up against `cad3f78` confirmed that the stylesheet still hid non-error notices, including the pending-quit message. Notices now hide only when empty. A pure notice-state helper tracks whether the current message came from a snapshot or the bridge, so resolved snapshot messages clear without erasing unrelated bridge errors. Successful bridge calls likewise preserve active snapshot messages. The duplicate snapshot-error report has been removed.

- Reproduced the hidden quit message against the original stylesheet in a browser, then verified visible informational notices, hidden empty notices, and unchanged error colors in all five view contexts after the fix.
- Reproduced stale snapshot text and bridge success clearing an active snapshot notice against the original renderer. Added unit coverage for notice ownership and renderer coverage across all five views; existing bridge-error preservation remains covered.
- `make check` passed with 67 JavaScript tests, and the full Go race suite passed in 131 seconds. Both used the same macOS 13.0 deployment settings and temporary Go cache described above.
