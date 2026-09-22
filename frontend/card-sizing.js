// Only grow when content overflows. Keep manually chosen space and avoid
// shrinking/jumping each time a transient status or suggested action disappears.
export function requiredContentHeight(viewportHeight, scrollHeight, clientHeight) {
  if (![viewportHeight, scrollHeight, clientHeight].every(Number.isFinite) || viewportHeight <= 0 || clientHeight <= 0) return null;
  const overflow = scrollHeight - clientHeight;
  return overflow > 1 ? Math.ceil(viewportHeight + overflow) : null;
}

export function installCardSizing(root, list, requestFit, busy = () => false) {
  let frame = 0, lastRequest = '';
  function measure() {
    frame = 0;
    if (busy()) return;
    const container = document.body.classList.contains('workspace') ? root : list;
    const height = requiredContentHeight(window.innerHeight, container.scrollHeight, container.clientHeight);
    if (height === null) { lastRequest = ''; return; }
    const size = {height, viewportWidth: window.innerWidth, viewportHeight: window.innerHeight};
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
