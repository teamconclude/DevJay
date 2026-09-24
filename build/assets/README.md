# DevJay icon

`devjay-icon.png` is the selected DevJay artwork: a stylized blue jay with curved wing feathers, pale-blue accents, and a dark eye. It has a white background. `../DevJay.icns` contains the macOS icon sizes generated from this PNG.

Both development and installer builds copy `build/DevJay.icns` into the app's `Contents/Resources/`. At startup, DevJay also reads that bundled file and sets its application icon explicitly, so the running app does not rely on a cached Dock icon. Launch the intended `.app` by its full path when older DevJay checkouts are present.

Created with the built-in imagegen tool. Design iterations and their prompts are retained here:

- `devjay-logo-simple.png`: initial minimal mark; see `devjay-logo-simple-prompt.md`.
- `devjay-logo-refined.png`: curved feather edges and lighter details; see `devjay-logo-refined-prompt.md`.
- `devjay-logo-refined-eye.png`: selected artwork, identical to `devjay-icon.png`; see `devjay-logo-refined-eye-prompt.md`.

The final edit used the earlier illustrated icon as an eye/face reference only. The generation prompts describe those inputs at the time of editing.

## macOS icon sizes

The icon set includes 16, 32, 128, 256, and 512 point sizes, each at 1× and 2× resolution. Convert resized PNGs with `sips`, then package the `.iconset` directory using `iconutil -c icns`.
