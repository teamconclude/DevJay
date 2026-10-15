// Independent Site windows only grow when their content overflows.
export function requiredContentHeight(viewportHeight, scrollHeight, clientHeight) {
  if (![viewportHeight, scrollHeight, clientHeight].every(Number.isFinite) || viewportHeight <= 0 || clientHeight <= 0) return null;
  const overflow = scrollHeight - clientHeight;
  return overflow > 1 ? Math.ceil(viewportHeight + overflow) : null;
}

// Measure the card row's natural height without stretching it to fill a saved
// window. Restore the styles before painting; horizontal scrollbars still count.
export function fittedPanelHeight(list) {
  const overhead = window.innerHeight - list.clientHeight;
  const {flex, height} = list.style;
  try {
    list.style.flex = 'none';
    list.style.height = 'max-content';
    return Math.ceil(overhead + list.scrollHeight);
  } finally {
    list.style.flex = flex;
    list.style.height = height;
  }
}

export function installCardSizing(root, list, requestFit, busy = () => false) {
  let frame = 0, lastRequest = '';
  function measure() {
    frame = 0;
    if (busy() || !list.children.length) return;
    const workspace = document.body.classList.contains('workspace');
    const decoration = document.body.dataset.decoration;
    const refit = !workspace;
    // WebKit can clip a flex card internally without overflowing the list.
    // Re-measure its natural content after status/app rows arrive, too.
    // Leave one small line of breathing room beyond the fitted content. Fit in
    // both directions so disappearing status rows release their old height.
    const panelHeight = workspace ? null : Math.max(120, fittedPanelHeight(list)) + 12;
    const height = workspace ? requiredContentHeight(window.innerHeight, root.scrollHeight, root.clientHeight)
      : Math.abs(panelHeight - window.innerHeight) > 1 ? panelHeight : null;
    if (height === null) { lastRequest = ''; return; }
    const size = {height, viewportWidth: window.innerWidth, viewportHeight: window.innerHeight, decoration, refit};
    const key = JSON.stringify(size);
    // A screen-limited window may still overflow. Don't keep requesting a size
    // the native side cannot provide until its content or viewport changes.
    if (key !== lastRequest) { lastRequest = key; requestFit(size); }
  }
  function schedule() { if (!frame) frame = requestAnimationFrame(measure); }
  new MutationObserver(schedule).observe(root, {childList:true, subtree:true, characterData:true});
  new MutationObserver(schedule).observe(document.body, {attributes:true, attributeFilter:['data-layout','data-free-placement','data-decoration']});
  new ResizeObserver(schedule).observe(root);
  window.addEventListener('resize', schedule);
  // A live window drag/resize may have deferred native fitting.
  window.addEventListener('pointerup', () => { lastRequest = ''; schedule(); });
  document.fonts?.ready.then(schedule);
  schedule();
  return schedule;
}
