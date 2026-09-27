// Native menus can extend beyond the small card window. Send only the clicked
// Site/category/PR identity; the backend validates it and supplies the actions.
export function cardMenuTarget(target) {
  const card = target?.closest('.card, .site-detail');
  if (!card?.dataset.id) return null;
  const content = target.closest('[data-pr-number], [data-view-kind]');
  const number = Number(content?.dataset.prNumber);
  const kind = content?.dataset.viewKind;
  return {siteID: card.dataset.id, kind: ['edits', 'local', 'remote'].includes(kind) ? kind : 'edits',
    number: Number.isSafeInteger(number) && number > 0 ? number : 0};
}

export function installCardMenu(root, open) {
  function show(event, keyboard = false) {
    const target = cardMenuTarget(event.target);
    if (!target) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = keyboard ? event.target.getBoundingClientRect() : null;
    const x = keyboard ? rect.left + Math.min(rect.width / 2, 20) : event.clientX;
    const y = keyboard ? rect.top + rect.height : event.clientY;
    open({...target, x: Math.round(x), y: Math.round(y)});
  }
  root.addEventListener('contextmenu', event => show(event));
  root.addEventListener('keydown', event => {
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) show(event, true);
  });
}
