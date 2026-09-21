// Native tracking keeps inactive windows responsive without focus or clicks.
export function installCardHover(target, document) {
  let hovered = null;
  function update(x, y) {
    const next = document.elementFromPoint(x, y)?.closest('.card, .site-detail') || null;
    if (next === hovered) return;
    hovered?.classList.remove('pointer-hover');
    next?.classList.add('pointer-hover');
    hovered = next;
  }
  target.addEventListener('devjay:native-hover', event => {
    update(event.detail.x * target.innerWidth, event.detail.y * target.innerHeight);
  });
  target.addEventListener('pointermove', event => update(event.clientX, event.clientY));
  document.addEventListener('pointerleave', () => update(-1, -1));
}
