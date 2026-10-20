export const snap = value => Math.max(0, Math.min(100000, Math.round(value / 8) * 8));

// Reordering follows row-major order in a tiled layout, or the column in Vertical.
export function insertionIndex(rects, x, y, vertical) {
  if (vertical) return rects.filter(r => y > r.top + r.height / 2).length;
  const row = rects.filter(r => y >= r.top && y <= r.bottom);
  if (row.length) {
    const first = rects.indexOf(row[0]);
    return first + row.filter(r => x > r.left + r.width / 2).length;
  }
  return rects.filter(r => y > r.bottom).length;
}

export function installStripDrag(list, options) {
  let cancelDrag = null, suppressClick = false;
  list.addEventListener('click', e => {
    if (suppressClick) { e.preventDefault(); e.stopImmediatePropagation(); suppressClick = false; }
  }, true);
  list.addEventListener('pointerdown', e => {
    suppressClick = false;
    const card = e.target.closest('.card');
    const dragTarget = e.target.closest('.card-title');
    if (!card || !dragTarget || e.button !== 0 || !e.isPrimary || !options.enabled()) return;
    e.preventDefault();
    const rect = card.getBoundingClientRect();
    const original = [...list.children];
    const free = options.layout() === 'custom';
    const bounds = list.getBoundingClientRect();
    const positions = Object.fromEntries(original.map((n, index) => {
      const r = n.getBoundingClientRect();
      return [n.dataset.id, {x: snap(r.left - bounds.left + list.scrollLeft), y: snap(r.top - bounds.top + list.scrollTop), z: index}];
    }));
    // Keep saved stacking order, including cards covered by another card.
    const saved = options.placements();
    if (document.body.dataset.freePlacement === 'true') {
      for (const n of original) if (saved[n.dataset.id]) positions[n.dataset.id] = {...saved[n.dataset.id]};
    }
    const origin = {...positions[card.dataset.id]};
    const scrollX = list.scrollLeft, scrollY = list.scrollTop;
    const preview = card.cloneNode(true);
    preview.classList.add('strip-drag-preview'); preview.inert = true;
    preview.setAttribute('aria-hidden', 'true');
    Object.assign(preview.style, {width: `${rect.width}px`, height: `${rect.height}px`, left: `${rect.left}px`, top: `${rect.top}px`});
    let moved = false, x = e.clientX, y = e.clientY, frame;
    options.busy(true);
    dragTarget.setPointerCapture(e.pointerId);
    function position() {
      if (!moved) return;
      preview.style.left = `${rect.left + x - e.clientX}px`;
      preview.style.top = `${rect.top + y - e.clientY}px`;
      const bounds = list.getBoundingClientRect();
      if (x < bounds.left || x > bounds.right || y < bounds.top || y > bounds.bottom) return;
      if (free) {
        const p = positions[card.dataset.id];
        p.x = snap(origin.x + x - e.clientX + list.scrollLeft - scrollX);
        p.y = snap(origin.y + y - e.clientY + list.scrollTop - scrollY);
        preview.style.left = `${bounds.left + p.x - list.scrollLeft}px`;
        preview.style.top = `${bounds.top + p.y - list.scrollTop}px`;
        return;
      }
      const others = [...list.children].filter(n => n !== card);
      const at = insertionIndex(others.map(n => n.getBoundingClientRect()), x, y, options.layout() === 'vertical');
      const next = others[at] || null;
      if (card.nextElementSibling !== next) {
        list.insertBefore(card, next);
        dragTarget.setPointerCapture(e.pointerId);
      }
    }
    function tick() {
      const b = list.getBoundingClientRect();
      if (moved && x >= b.left && x <= b.right && y >= b.top && y <= b.bottom) {
        list.scrollBy(x < b.left + 24 ? -8 : x > b.right - 24 ? 8 : 0,
                      y < b.top + 24 ? -8 : y > b.bottom - 24 ? 8 : 0);
      }
      position(); frame = requestAnimationFrame(tick);
    }
    function move(event) {
      if (event.pointerId !== e.pointerId) return;
      x = event.clientX; y = event.clientY;
      if (!moved && Math.hypot(x - e.clientX, y - e.clientY) > 4) {
        moved = true; suppressClick = true;
        document.body.append(preview); card.classList.add('strip-dragging');
      }
      position();
    }
    function finish(commit) {
      cancelDrag = null;
      cancelAnimationFrame(frame); preview.remove(); card.classList.remove('strip-dragging');
      dragTarget.removeEventListener('pointermove', move); dragTarget.removeEventListener('pointerup', up);
      dragTarget.removeEventListener('pointercancel', cancel); dragTarget.removeEventListener('lostpointercapture', cancel);
      window.removeEventListener('blur', cancel); document.removeEventListener('keydown', key, true);
      if (dragTarget.hasPointerCapture(e.pointerId)) dragTarget.releasePointerCapture(e.pointerId);
      const bounds = list.getBoundingClientRect();
      if (commit && moved && options.enabled() && x >= bounds.left && x <= bounds.right && y >= bounds.top && y <= bounds.bottom) {
        if (free) {
          const ordered = original.filter(n => n !== card).sort((a,b) => positions[a.dataset.id].z - positions[b.dataset.id].z);
          ordered.push(card);
          ordered.forEach((n,index) => { positions[n.dataset.id].z = index; });
          document.body.dataset.freePlacement = 'true';
          for (const n of original) {
            const p = positions[n.dataset.id];
            Object.assign(n.style, {left: `${p.x}px`, top: `${p.y}px`, zIndex: p.z});
          }
          options.place(positions);
        } else options.commit([...list.children].map(n => n.dataset.id));
      } else for (const node of original) list.append(node);
      options.busy(false);
    }
    const up = event => { if (event.pointerId === e.pointerId) { move(event); finish(true); } };
    const cancel = () => finish(false);
    const key = event => { if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); cancel(); } };
    cancelDrag = cancel;
    dragTarget.addEventListener('pointermove', move); dragTarget.addEventListener('pointerup', up);
    dragTarget.addEventListener('pointercancel', cancel); dragTarget.addEventListener('lostpointercapture', cancel);
    window.addEventListener('blur', cancel); document.addEventListener('keydown', key, true);
    frame = requestAnimationFrame(tick);
  });
  return () => cancelDrag?.();
}
