// Heading buttons handle their own keyboard/click activation. The rest of a
// strip card activates on a plain click, without stealing controls or selection.
export function installCardActivation(list, activate) {
  list.addEventListener('click', event => {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.target.closest('button, a, input, select, textarea')) return;
    if (event.target.closest('.copyable') && !window.getSelection().isCollapsed) return;
    const card = event.target.closest('.card');
    if (card) activate(card.dataset.id);
  });
}
