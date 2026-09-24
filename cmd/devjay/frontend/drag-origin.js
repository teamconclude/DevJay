// Pointer events precede Wails' mouse handlers. Only a real press in this
// webview may enable its window-drag regions; entering with a held button
// (for example while resizing another window) must not arm a window drag.
export function installDragOriginGuard(target, root) {
  const clear = () => root.removeAttribute('data-local-drag-press');
  target.addEventListener('pointerdown', event => {
    clear();
    if (event.isTrusted && event.button === 0 && event.isPrimary) {
      root.setAttribute('data-local-drag-press', '');
    }
  }, true);
  target.addEventListener('pointerup', clear, true);
  target.addEventListener('pointercancel', clear, true);
  target.addEventListener('blur', clear, true);
  target.addEventListener('pointermove', event => {
    if (!(event.buttons & 1)) clear();
  }, true);
}
