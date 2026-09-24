// Native tracking keeps inactive windows responsive without focus or clicks.
export function installCardHover(target, document) {
  let hovered = null, hoveredStatus = null;
  function update(x, y) {
    const hit = document.elementFromPoint(x, y);
    const nextStatus = hit?.closest('.status-link') || null;
    if(nextStatus !== hoveredStatus){
      hoveredStatus?.classList.remove('status-hover');
      nextStatus?.classList.add('status-hover');
      hoveredStatus = nextStatus;
    }
    const next = hit?.closest('.card, .site-detail') || null;
    if (next === hovered) return;
    hovered?.classList.remove('pointer-hover');
    next?.classList.add('pointer-hover');
    hovered = next;
  }
  target.addEventListener('devjay:native-hover', event => {
    update(event.detail.x * target.innerWidth, event.detail.y * target.innerHeight);
  });
  target.addEventListener('pointermove', event => update(event.clientX, event.clientY));
}
