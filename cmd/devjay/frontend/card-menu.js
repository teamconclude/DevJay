// Native menus can extend beyond the small card window. Send only the clicked
// Site/category/PR identity; the backend validates it and supplies the actions.
export function cardMenuTarget(target) {
  const card = target?.closest('.card, .site-detail');
  if (!card?.dataset.id) return null;
  const content = target.closest('[data-pr-number], [data-view-kind], [data-app-bundle]');
  const number = Number(content?.dataset.prNumber);
  const kind = content?.dataset.viewKind;
  const fallback = number > 0 && content?.matches?.('.pr-status, .metric') ? 'pr-status' : 'site';
  return {siteID: card.dataset.id, kind: ['auto', 'edits', 'branch', 'local', 'remote', 'review-status', 'return-status'].includes(kind) ? kind : fallback,
    number: Number.isSafeInteger(number) && number > 0 ? number : 0, bundleID: content?.dataset.appBundle || '',
    ...(content?.dataset.baseBranch ? {baseBranch:content.dataset.baseBranch} : {}),
    ...(content?.dataset.publishRebased === 'true' ? {publishRebased:true} : {}),
    ...(['update', 'push'].includes(content?.dataset.gitAction) ? {gitAction:content.dataset.gitAction} : {}),
    ...(content?.dataset.publishButton === 'true' ? {publishButton:true} : {})};
}

export function cardBounds(target) {
  const rect = target?.closest('.card, .site-detail')?.getBoundingClientRect();
  return rect ? {x:rect.left,y:rect.top,width:rect.width,height:rect.height} : undefined;
}

export function installCardMenu(root, open, strip = false) {
  function show(event, keyboard = false) {
    const target = cardMenuTarget(event.target) || (strip ? {kind:'strip'} : null);
    if (!target) return;
    event.preventDefault();
    event.stopPropagation();
    // WebKit can select a word on right-click. Do not let that selection
    // suppress later ordinary card clicks after the native menu closes.
    root.ownerDocument?.getSelection()?.removeAllRanges();
    const rect = keyboard ? event.target.getBoundingClientRect() : null;
    const x = keyboard ? rect.left + Math.min(rect.width / 2, 20) : event.clientX;
    const y = keyboard ? rect.top + rect.height : event.clientY;
    open({...target, anchor:target.kind === 'strip' ? undefined : cardBounds(event.target), x: Math.round(x), y: Math.round(y)});
  }
  root.addEventListener('contextmenu', event => show(event));
  root.addEventListener('keydown', event => {
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) show(event, true);
  });
}
