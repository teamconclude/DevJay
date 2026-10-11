// Only grow when content overflows. Keep manually chosen space and avoid
// shrinking/jumping each time a transient status or suggested action disappears.
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
  let frame = 0, lastRequest = '', fittedContext;
  function measure() {
    frame = 0;
    if (busy() || !list.children.length) return;
    const workspace = document.body.classList.contains('workspace');
    const decoration = document.body.dataset.decoration;
    const context = [decoration, document.body.dataset.layout, document.body.dataset.freePlacement].join('/');
    const refit = !workspace && fittedContext !== context;
    // WebKit can clip a flex card internally without overflowing the list.
    // Re-measure its natural content after status/app rows arrive, too.
    const panelHeight = workspace ? null : Math.max(120, fittedPanelHeight(list));
    const height = workspace ? requiredContentHeight(window.innerHeight, root.scrollHeight, root.clientHeight)
      : refit ? panelHeight : requiredContentHeight(window.innerHeight, panelHeight, window.innerHeight);
    fittedContext = context;
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
