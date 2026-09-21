import { Events, Window } from '/wails/runtime.js';
import { cardStatus } from './card-status.js';
import { installPanelDrag } from './panel-drag.js';
import { installDragOriginGuard } from './drag-origin.js';
installDragOriginGuard(window, document.documentElement);
const params = new URLSearchParams(location.search);
const siteID = params.get('site');
const settings = params.has('settings');
const root = document.querySelector('#app');
let snapshot = {sites: []}, dragging = null, panelDragging = false;
const el = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ''; if (text !== undefined) n.textContent = text; return n; };
const button = (text, action, cls = '') => { const n = el('button', cls, text); n.type = 'button'; n.onclick = action; return n; };
const notice = el('footer', 'notice copyable'); notice.setAttribute('role', 'status');
function report(text, error = false) { const message = error ? text : ''; if (notice.textContent !== message) notice.textContent = message; notice.classList.toggle('error', error); }
async function emit(name, data) { try { await Events.Emit(`devjay:${name}`, data); } catch { report('Could not complete the request. Try again.', true); } }
function pathLabel(site) { const n = el('div', 'path'); n.title = site.path; n.append(el('span', 'prefix', site.prefix), el('strong', '', site.name)); return n; }
function details(site, parent) {
  const {checked, ...displayGit} = site.git;
  const signature = JSON.stringify([site.path, site.prefix, site.name, displayGit]);
  if (parent.dataset.details === signature) return;
  parent.dataset.details = signature;
  parent.replaceChildren();
  parent.append(pathLabel(site), el('div', 'branch', site.git.branch || 'Folder'));
  const status = cardStatus(site.git);
  const body = el('div', 'card-status');
  function line(text, tone, symbol, title = text) {
    const row = el('div', `status-line copyable ${tone}`);
    const icon = el('span', 'status-symbol', symbol); icon.setAttribute('aria-hidden', 'true');
    row.append(icon, el('span', '', text)); row.title = title; body.append(row);
  }
  function metrics(label, values) {
    if (!values.length) return;
    const row = el('div', 'metric-row copyable'); row.append(el('span', 'metric-label', label));
    for (const item of values) {
      const badge = el('span', `metric ${item.tone}`); badge.title = item.title; badge.setAttribute('aria-label', item.title);
      const symbol = el('span', 'metric-symbol', item.symbol); symbol.setAttribute('aria-hidden', 'true');
      badge.append(symbol);
      if (item.value !== '') badge.append(el('strong', '', item.value));
      badge.append(el('span', 'metric-unit', item.label)); row.append(badge);
    }
    body.append(row);
  }
  if (status.alert) line(status.alert.text, status.alert.tone, status.alert.symbol, site.git.summary);
  if (status.quiet) line(status.quiet, 'good quiet-status', '✓');
  metrics('Local', status.local); metrics('Remote', status.remote);
  if (status.pr) line(status.pr, `pr-status ${status.prTone}`, status.prTone === 'good' ? '✓' : '⑂', site.git.github);
  if (status.freshness) line(status.freshness, 'freshness muted', '◷', [site.git.remoteNote, site.git.remoteChecked ? `Last checked ${new Date(site.git.remoteChecked).toLocaleString()}` : ''].filter(Boolean).join('\n'));
  parent.append(body);
}

let list, decoration, layout, siteError, siteErrorTimer;
if (settings) {
  document.body.className = 'settings';
  const sitesHeading = el('div', 'sites-heading'); sitesHeading.append(el('h2', '', 'Sites'), button('Add', () => emit('browse'))); root.append(sitesHeading);
  root.append(el('p', 'help', 'Drop a folder below to add a site.'));
  list = el('div', 'site-list'); list.id = 'site-drop-zone'; list.setAttribute('data-file-drop-target', ''); list.setAttribute('aria-label', 'Sites');
  const dropArea = el('div', 'site-drop-area');
  siteError = el('div', 'site-drop-error'); siteError.hidden = true; siteError.setAttribute('role', 'alert');
  dropArea.append(list, siteError); root.append(dropArea);
  const sorting = el('div', 'sorting'); sorting.setAttribute('role', 'radiogroup'); sorting.setAttribute('aria-label', 'Sorting'); sorting.append(el('span', '', 'Sorting:'));
  for (const [value, title] of [['name', 'By name'], ['manual', 'Manual']]) {
    const label = el('label'); const radio = el('input'); radio.type = 'radio'; radio.name = 'sorting'; radio.value = value;
    radio.onchange = () => { if (radio.checked) emit('sorting', value); };
    label.append(radio, document.createTextNode(title)); sorting.append(label);
  }
  root.append(sorting);
  root.append(el('h2', '', 'View settings'));
  for (const [key, title] of [['allDesktops', 'Control panel always visible'], ['onTop', 'Show all windows on top']]) {
    const label = el('label', 'preference'); const box = el('input'); box.type = 'checkbox'; box.dataset.pref = key; box.onchange = () => emit(key, box.checked);
    label.append(box, document.createTextNode(title)); root.append(label);
  }
  const panelLayout = el('label', 'decoration-setting', 'Control panel layout');
  layout = el('select'); layout.setAttribute('aria-label', 'Control panel layout');
  for (const value of ['horizontal', 'vertical', 'custom']) {
    const option = el('option', '', value[0].toUpperCase() + value.slice(1)); option.value = value; layout.append(option);
  }
  layout.onchange = () => emit('layout', layout.value);
  panelLayout.append(layout); root.append(panelLayout);
  const resetCards = button('Reset card positions', () => emit('reset-placements'), 'reset-card-positions'); root.append(resetCards);
  const appearance = el('label', 'decoration-setting', 'Window decoration');
  decoration = el('select'); decoration.setAttribute('aria-label', 'Window decoration');
  for (const [value, title] of [['none', 'None'], ['minimal', 'Minimal'], ['normal', 'Normal']]) {
    const option = el('option', '', title); option.value = value; decoration.append(option);
  }
  decoration.onchange = () => emit('decoration', decoration.value);
  appearance.append(decoration); root.append(appearance);
  const actions = el('div', 'settings-actions');
  actions.append(button('Done', () => emit('settings-close'), 'done'));
  root.append(notice, actions);
  document.addEventListener('keydown', e => { if (e.key === 'Escape') emit('settings-close'); });
} else if (siteID) {
  document.body.className = 'workspace'; document.body.dataset.decoration = 'none'; root.title = 'Drag to move this Site';
  root.addEventListener('contextmenu', e => { e.preventDefault(); emit('settings-open'); });
  const header = el('header');
  const grip = el('span', 'grip', '⠿'); grip.title = 'Drag to move this Site';
  const gear = button('⚙', () => emit('settings-open'), 'gear'); gear.setAttribute('aria-label', 'Settings'); gear.title = 'Settings';
  const minimise = button('−', () => Window.Minimise(), 'minimise-window'); minimise.setAttribute('aria-label', 'Minimize Site'); minimise.title = 'Minimize Site';
  header.append(grip, gear, minimise);
  list = el('div', 'site-detail'); root.append(header, list, notice);
} else {
  document.body.className = 'strip'; document.body.dataset.decoration = 'none';
  root.addEventListener('contextmenu', e => { e.preventDefault(); emit('settings-open'); }); const header = el('header');
  const gear = button('⚙', () => emit('settings-open'), 'gear'); gear.setAttribute('aria-label', 'Settings'); gear.title = 'Settings';
  const grip = el('span', 'grip', '⠿'); grip.title = 'DevJay · Drag to move';
  const close = button('×', () => Window.Close(), 'close-window'); close.setAttribute('aria-label', 'Quit DevJay'); close.title = 'Quit DevJay';
  header.append(grip, gear, close); list = el('nav', 'cards'); list.setAttribute('aria-label', 'Sites'); root.append(header, list, notice);
  installPanelDrag(list, {
    enabled: () => snapshot.settingsOpen,
    layout: () => snapshot.layout || 'horizontal',
    busy: value => { panelDragging = value; },
    commit: ids => emit('order', ids),
    placements: () => cardPlacements(snapshot),
    place: positions => emit('placements', positions),
  });
  // Wails starts native window dragging on mouse movement. Suppress the
  // subsequent click as well, even if the native drag has already ended.
  let press = null;
  root.addEventListener('mousedown', e => {
    press = e.button === 0 && e.target.closest('.card') ? {x: e.screenX, y: e.screenY, moved: false} : null;
  }, true);
  root.addEventListener('mousemove', e => {
    if (press && (e.buttons & 1) && (e.screenX !== press.x || e.screenY !== press.y)) press.moved = true;
  }, true);
  root.addEventListener('click', e => {
    if (e.detail !== 0 && (press?.moved || (e.target.closest('.copyable') && !window.getSelection().isCollapsed))) { e.preventDefault(); e.stopImmediatePropagation(); }
    press = null;
  }, true);

}
function startSiteDrag(event, row, handle) {
  if (event.button !== 0 || dragging) return;
  event.preventDefault();
  const rect = row.getBoundingClientRect();
  const ids = snapshot.sites.map(s => s.id);
  const others = [...list.children].filter(n => n !== row);
  const preview = row.cloneNode(true);
  preview.classList.add('site-drag-preview'); preview.setAttribute('aria-hidden', 'true'); preview.inert = true;
  Object.assign(preview.style, {width: `${rect.width}px`, left: `${rect.left}px`, top: `${rect.top}px`});
  // The floating row is outside the shared grid; preserve its measured columns.
  preview.style.gridTemplateColumns = ['.handle', '.path', '.row-status', '.remove-site'].map(selector => `${row.querySelector(selector).getBoundingClientRect().width}px`).join(' ');
  const marker = el('div', 'site-drop-marker'); marker.setAttribute('aria-hidden', 'true');
  document.body.append(preview, marker);
  dragging = row.dataset.id; row.classList.add('dragging'); document.body.classList.add('sorting-sites');
  handle.setPointerCapture(event.pointerId);
  let x = event.clientX, y = event.clientY, target = ids.indexOf(dragging), frame, moved = false;
  const offsetX = x - rect.left, offsetY = y - rect.top;
  function position() {
    const bounds = list.getBoundingClientRect();
    preview.style.left = `${x - offsetX}px`; preview.style.top = `${y - offsetY}px`;
    const inside = x >= bounds.left && x <= bounds.right && y >= bounds.top && y <= bounds.bottom;
    marker.hidden = !inside || !moved;
    target = inside ? others.filter(n => { const r = n.getBoundingClientRect(); return y > r.top + r.height / 2; }).length : -1;
    if (target >= 0) {
      const next = others[target];
      const edge = next ? next.getBoundingClientRect().top : (others.at(-1)?.getBoundingClientRect().bottom ?? bounds.top + 6);
      Object.assign(marker.style, {left: `${bounds.left + 6}px`, top: `${edge - 1}px`, width: `${bounds.width - 12}px`});
    }
  }
  function tick() {
    // Let a long list scroll while the pointer remains near a window edge.
    if (moved && x >= 0 && x <= innerWidth) {
      if (y < 36) window.scrollBy(0, -8);
      else if (y > innerHeight - 36) window.scrollBy(0, 8);
    }
    position(); frame = requestAnimationFrame(tick);
  }
  function move(e) {
    if (e.pointerId !== event.pointerId) return;
    x = e.clientX; y = e.clientY;
    moved ||= Math.hypot(x - event.clientX, y - event.clientY) > 3;
    position();
  }
  function finish(commit) {
    if (!dragging) return;
    const id = dragging; dragging = null;
    cancelAnimationFrame(frame); preview.remove(); marker.remove(); row.classList.remove('dragging'); document.body.classList.remove('sorting-sites');
    handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', up); handle.removeEventListener('pointercancel', cancel); handle.removeEventListener('lostpointercapture', cancel);
    document.removeEventListener('keydown', key, true); window.removeEventListener('blur', cancel);
    if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
    if (commit && moved && target >= 0) {
      const order = ids.filter(value => value !== id); order.splice(target, 0, id); emit('order', order);
    }
    render(snapshot);
  }
  const up = e => { if (e.pointerId === event.pointerId) { move(e); finish(true); } };
  const cancel = () => finish(false);
  const key = e => { if (e.key === 'Escape') { e.preventDefault(); e.stopImmediatePropagation(); finish(false); } };
  handle.addEventListener('pointermove', move); handle.addEventListener('pointerup', up); handle.addEventListener('pointercancel', cancel); handle.addEventListener('lostpointercapture', cancel);
  document.addEventListener('keydown', key, true); window.addEventListener('blur', cancel);
  position(); frame = requestAnimationFrame(tick);
}

function reorder(id, target) {
  const ids = snapshot.sites.map(s => s.id), from = ids.indexOf(id);
  if (from < 0 || target < 0 || target >= ids.length) return;
  ids.splice(from, 1); ids.splice(target, 0, id); emit('order', ids);
}
function cardPlacements(state) {
  const positions = {};
  state.sites.forEach((site, index) => {
    positions[site.id] = state.placements?.[site.id] || {x: (index % 3) * 224, y: Math.floor(index / 3) * 160, z: index};
  });
  return positions;
}
function render(state) {
  snapshot = state;
  if (panelDragging) return;
  document.body.dataset.layout = state.layout || 'horizontal';
  document.body.dataset.settingsOpen = String(!!state.settingsOpen);
  document.body.dataset.cardDrag = String(!!state.settingsOpen);
  if (layout) layout.value = state.layout || 'horizontal';
  const free = state.layout === 'custom' && Object.keys(state.placements || {}).length > 0;
  document.body.dataset.freePlacement = String(free);
  const resetCards = document.querySelector('.reset-card-positions');
  if (resetCards) resetCards.hidden = !free;
  document.body.dataset.decoration = state.decoration || 'none';
  if (decoration) decoration.value = state.decoration || 'none';
  if (state.error) report(state.error, true);
  if (settings) {
    document.querySelectorAll('[data-pref]').forEach(n => { n.checked = state.preferences[n.dataset.pref]; });
    document.querySelectorAll('[name=sorting]').forEach(n => { n.checked = n.value === (state.manual ? 'manual' : 'name'); });
    if (dragging) return;
    const signature = state.sites.map(s => s.id).join(',');
    if (list.dataset.signature !== signature) {
      list.dataset.signature = signature; list.replaceChildren();
      state.sites.forEach((site, index) => {
        const row = el('div', 'site-row'); row.dataset.id = site.id;
        const description = el('div', 'row-description'); description.append(pathLabel(site), el('div', 'row-status help')); row.append(button('⠿', () => {}, 'handle'), description);
        const remove = button('×', () => emit('remove', site.id), 'remove-site');
        remove.setAttribute('aria-label', `Remove ${site.name}`); remove.title = 'Remove Site'; row.append(remove);
        const handle = row.querySelector('.handle'); handle.setAttribute('aria-label', `Reorder ${site.name}`); handle.title = 'Drag to reorder, or use Up and Down arrow keys';
        handle.onkeydown = e => { if (e.key === 'ArrowUp' || e.key === 'ArrowDown') { e.preventDefault(); reorder(site.id, index + (e.key === 'ArrowUp' ? -1 : 1)); } };
        handle.onpointerdown = e => startSiteDrag(e, row, handle);
        list.append(row);
      });
    }
    for (const site of state.sites) { const row = [...list.children].find(n => n.dataset.id === site.id); const status = row.querySelector('.row-status'); status.textContent = [site.git.branch, site.position.label || 'Desktop unknown'].filter(Boolean).join(' · '); status.title = status.textContent; }
  } else if (siteID) {
    const site = state.sites.find(s => s.id === siteID); if (!site) return;
    details(site, list); root.title = `${site.path}\n${site.position.label || 'Desktop unknown'}\nDrag to move this Site`;
  } else {
    if (!state.sites.length) { list.replaceChildren(button('Add your first Site in Settings', () => emit('settings-open'), 'empty')); return; }
    for (const child of [...list.children]) if (!state.sites.some(s => s.id === child.dataset.id)) child.remove();
    state.sites.forEach((site, index) => {
      let card = [...list.children].find(n => n.dataset.id === site.id);
      if (!card) { card = button('', () => emit('activate', site.id), 'card'); card.dataset.id = site.id; }
      const position = cardPlacements(state)[site.id];
      card.style.left = free ? `${position.x}px` : '';
      card.style.top = free ? `${position.y}px` : '';
      card.style.zIndex = free ? position.z : '';
      card.classList.toggle('visible', site.window.onDesktop);
      card.setAttribute('aria-label', `Activate ${site.path}, ${site.git.branch || 'folder'}, ${site.git.summary}${site.window.onDesktop ? ', visible on current desktop' : ''}`);
      details(site, card);
      let handle = card.querySelector('.card-move-handle');
      if (!handle) {
        handle = el('span', 'card-move-handle', '⠿');
        handle.title = 'Drag to move this card';
        handle.setAttribute('aria-label', 'Drag to move this card');
        handle.onclick = e => e.stopPropagation();
        card.append(handle);
      }
      handle.hidden = !state.settingsOpen;
      card.title = [site.window.onDesktop ? 'Visible on current desktop' : '', site.path, site.git.branch, site.git.summary, site.git.github, site.git.remoteNote, site.position.label, site.git.tracking ? `Tracking ${site.git.tracking}` : '', site.git.remoteChecked ? `Remote checked ${new Date(site.git.remoteChecked).toLocaleString()}` : ''].filter(Boolean).join('\n');
      if (list.children[index] !== card) list.insertBefore(card, list.children[index] || null);
    });
  }
}
if (settings) Events.On('devjay:site-error', event => {
  clearTimeout(siteErrorTimer);
  siteError.textContent = event.data;
  siteError.hidden = false;
  siteErrorTimer = setTimeout(() => { siteError.hidden = true; siteError.textContent = ''; }, 5000);
});
Events.On('devjay:snapshot', event => render(event.data));
Events.On('devjay:notice', event => { report(event.data.text, event.data.error);  });
emit('hello');
