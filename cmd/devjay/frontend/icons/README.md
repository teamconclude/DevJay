# Agent identity icons

These 40×40 PNG app marks identify detected Claude Code and Codex processes inside an editor or terminal. They are bundled so a CLI installation can be represented without requiring its desktop app to be installed.

- `claude.png`: Claude app icon, extracted from the installed `com.anthropic.claudefordesktop` bundle.
- `codex.png`: OpenAI app icon, extracted from the installed unified ChatGPT/Codex `com.openai.codex` bundle.

Both were rasterized using macOS `NSWorkspace.iconForFile` on 28 September 2026. The marks belong to their respective owners; DevJay's source-code license does not grant rights to those marks. Host IDE/terminal and standalone app icons are read from the user's installed apps instead.
