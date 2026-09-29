import { prCheckIndicator, prTagTone } from './pr-status.js';
import { Events, Window } from '/wails/runtime.js';
import { createBridge } from './bridge.js';
import { nextNotice } from './notice.js';
import { uiReady } from './ui-ready.js';
import { installCardSizing } from './card-sizing.js';
import { mountEditorSettings } from './editor-settings.js';
import { mountGitViewer } from './git-viewer.js';
import { cardStatus } from './card-status.js';
import { mountSuggestedActions, mountViewerSuggestions } from './suggested-actions.js';
import { installPanelDrag } from './panel-drag.js';
import { installCardActivation } from './card-interaction.js';
import { installDragOriginGuard } from './drag-origin.js';
import { installCardHover } from './card-hover.js';
import { installCardMenu } from './card-menu.js';
installCardHover(window, document);
installDragOriginGuard(window, document.documentElement);
const reportUIReady = uiReady(id => Events.Emit('devjay:ui-ready', id));
const params = new URLSearchParams(location.search);
const siteID = params.get('site');
const settings = params.has('settings');
const actions = params.has('actions');
const viewer = params.has('viewer');
let renderSuggestedActions, renderViewerSnapshot, editorSettings;
const root = document.querySelector('#app');
let snapshot = {sites: []}, dragging = null, panelDragging = false;
let nativeVisible = true, renderedSnapshot = false, deferredSnapshot = null;
function updateMonitoringVisibility() {
  const visible = nativeVisible && !document.hidden;
  document.body.dataset.monitorVisible = String(visible);
  if (!visible) {
    // A brief attention pulse must not resume minutes later after unhiding.
    document.querySelectorAll('.card-waiting-glow').forEach(node => node.remove());
    document.querySelectorAll('.waiting-attention').forEach(node => node.classList.toggle('waiting-attention', false));
  } else if (deferredSnapshot) {
    const state = deferredSnapshot; deferredSnapshot = null;
    render(state);
  }
}
Events.On('devjay:visibility', event => {
  nativeVisible = event.data === true;
  updateMonitoringVisibility();
});
document.addEventListener('visibilitychange', updateMonitoringVisibility);
const el = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ''; if (text !== undefined) n.textContent = text; return n; };
const button = (text, action, cls = '') => { const n = el('button', cls, text); n.type = 'button'; n.onclick = action; return n; };
const notice = el('footer', 'notice copyable'); notice.setAttribute('role', 'status');
let currentNotice;
function report(source, value) {
  currentNotice = nextNotice(currentNotice, source, value);
  if (notice.textContent !== currentNotice.text) notice.textContent = currentNotice.text;
  notice.classList.toggle('error', currentNotice.error);
}
const bridge = createBridge((name, data) => Events.Emit(name, data), (text, error) => report('bridge', {text, error}));
const emit = bridge.emit;
Events.On('devjay:request-result', event => bridge.reply(event.data));
const senderName = siteID ? `site-${siteID}` : 'control-panel';
Events.On('devjay:pr-open-result', event => {
  if (!settings && !viewer && !actions && event.data.sender === senderName) {
    report('bridge', {text:event.data.error || event.data.message || '', error:!!event.data.error});
  }
});
if (!settings && !viewer && !actions) installCardMenu(root, data => emit('card-menu', data));
function pathLabel(site) { const n = el('div', 'path'); n.title = site.path; n.append(el('span', 'prefix', site.prefix), el('strong', '', site.name)); return n; }
const waitingAttentionDuration = 4000;
const waitingAttention = new Map();
let waitingAttentionSerial = 0;
function confirmedWaiting(app) {
  return (app.agents || []).filter(agent => (agent === 'Claude Code' || agent === 'Codex') && app.waitingAgents?.includes(agent));
}
function confirmedWorking(app) {
  return (app.agents || []).filter(agent => (agent === 'Claude Code' || agent === 'Codex') && app.workingAgents?.includes(agent) && !app.waitingAgents?.includes(agent));
}
function waitingAttentionKey(site, app, agent) { return JSON.stringify([site.id, site.path, app.bundleID || app.name, agent]); }
function updateWaitingAttention(state) {
  if (settings || viewer || actions) return;
  const current = new Set(), now = performance.now();
  if (state.preferences?.showIDEStatus !== false) {
    for (const site of state.sites) {
      if ((siteID && site.id !== siteID) || !site.developmentAppsKnown) continue;
      for (const app of site.developmentApps || []) {
        for (const agent of confirmedWaiting(app)) {
          const key = waitingAttentionKey(site, app, agent);
          current.add(key);
          if (!waitingAttention.has(key)) waitingAttention.set(key, {started:now, serial:++waitingAttentionSerial});
        }
      }
    }
  }
  for (const key of waitingAttention.keys()) if (!current.has(key)) waitingAttention.delete(key);
}
function appWaitingAttention(site, app) {
  return confirmedWaiting(app).map(agent => waitingAttention.get(waitingAttentionKey(site, app, agent))).filter(Boolean);
}
function waitingAttentionElapsed(entries) {
  if (!entries.length) return null;
  const elapsed = performance.now() - Math.max(...entries.map(entry => entry.started));
  return elapsed < waitingAttentionDuration ? elapsed : null;
}
function developmentAppIcon(name, source, size) {
  const icon = el('span', `development-app-icon${size === 16 ? ' development-agent-icon' : ''}`);
  icon.setAttribute('aria-hidden', 'true');
  const initials = (name.match(/\b[\p{L}\p{N}]|\p{Lu}/gu) || []).slice(0, 2).join('').toUpperCase() || '?';
  const fallback = el('span', 'development-app-initials', initials);
  icon.append(fallback);
  if (source) {
    const image = el('img'); image.alt = ''; image.width = size; image.height = size; image.draggable = false; image.hidden = true;
    image.setAttribute('aria-hidden', 'true');
    image.onload = () => { fallback.hidden = true; image.hidden = false; };
    image.onerror = () => { image.remove(); fallback.hidden = false; };
    icon.append(image); image.src = source;
  }
  return icon;
}
function developmentAppIndicator(site) {
  const observedApps = site.developmentAppsKnown ? (site.developmentApps || []) : [];
  const pairedApps = observedApps.filter(app => app.agents?.some(agent => agent === 'Claude Code' || agent === 'Codex'));
  const apps = pairedApps.length ? pairedApps : observedApps;
  const indicator = el('div', 'development-app');
  if (!apps.length) {
    indicator.textContent = site.developmentAppsKnown ? 'No app detected' : 'App unknown';
    return indicator;
  }
  for (const app of apps) {
    const agents = app.agents || [];
    const agentList = agents.length > 1 ? `${agents.slice(0, -1).join(', ')} and ${agents.at(-1)}` : agents[0];
    const waiting = confirmedWaiting(app);
    const waitingList = waiting.length > 1 ? `${waiting.slice(0, -1).join(', ')} and ${waiting.at(-1)}` : waiting[0];
    const working = confirmedWorking(app);
    const workingList = working.length > 1 ? `${working.slice(0, -1).join(', ')} and ${working.at(-1)}` : working[0];
    const description = (agents.length ? `${app.name} with ${agentList}` : app.name)
      + (waiting.length ? ` · ${waitingList} ${waiting.length === 1 ? 'needs' : 'need'} input` : '')
      + (working.length ? ` · ${workingList} ${working.length === 1 ? 'is' : 'are'} working` : '');
    const group = el('span', `development-app-group${agents.length ? ' has-agents' : ''}${waiting.length ? ' needs-input' : working.length ? ' is-working' : ''}`);
    const elapsed = waitingAttentionElapsed(appWaitingAttention(site, app));
    if (elapsed !== null) {
      group.classList.add('waiting-attention');
      // Recreated DOM resumes the original finite pulse; ordinary snapshots
      // never extend its deadline. CSS finishes without a timer or render.
      group.style.animationDelay = `${-elapsed}ms`;
    }
    group.dataset.appTooltip = description;
    group.setAttribute('aria-label', description);
    group.setAttribute('role', 'img');
    const source = typeof app.icon === 'string' && /^data:image\/png;base64,[A-Za-z0-9+/]+={0,2}$/.test(app.icon) ? app.icon : null;
    group.append(developmentAppIcon(app.name, source, 22));
    if (agents.length) {
      const agentIcons = el('span', 'development-app-agents'); agentIcons.setAttribute('aria-hidden', 'true');
      for (const agent of agents) {
        const source = agent === 'Claude Code' ? '/icons/claude.png' : agent === 'Codex' ? '/icons/codex.png' : null;
        const icon = developmentAppIcon(agent, source, 16);
        if (waiting.includes(agent)) {
          const marker = el('span', 'development-agent-input', '?'); marker.setAttribute('aria-hidden', 'true');
          icon.append(marker);
        } else if (working.includes(agent)) {
          const marker = el('span', 'development-agent-working', '▶'); marker.setAttribute('aria-hidden', 'true');
          icon.append(marker);
        }
        agentIcons.append(icon);
      }
      group.append(agentIcons);
    }
    if (working.length && !waiting.length) {
      const dots = el('span', 'development-app-working-dots'); dots.setAttribute('aria-hidden', 'true');
      const phase = performance.now();
      for (let i = 0; i < 3; i++) {
        const dot = el('span');
        // Retain the staggered CSS phase across rebuilds without a timer.
        dot.style.animationDelay = `${-((phase + i * 180) % 1800)}ms`;
        dots.append(dot);
      }
      group.append(dots);
    }
    indicator.append(group);
  }
  return indicator;
}
function details(site, parent, showIDEStatus) {
  const {checked, ...displayGit} = site.git;
  const attention = (site.developmentApps || []).map(app => appWaitingAttention(site, app).map(entry => entry.serial));
  const signature = JSON.stringify([site.path, site.prefix, site.name, displayGit, site.developmentApps, site.developmentAppsKnown, showIDEStatus, site.position?.label, attention]);
  if (parent.dataset.details === signature) return;
  parent.dataset.details = signature;
  parent.replaceChildren();
  const entries = (site.developmentApps || []).flatMap(app => appWaitingAttention(site, app));
  const elapsed = waitingAttentionElapsed(entries);
  if (showIDEStatus && elapsed !== null) {
    const glow = el('span', 'card-waiting-glow');
    glow.setAttribute('aria-hidden', 'true');
    glow.style.animationDelay = `${-elapsed}ms`;
    parent.append(glow);
  }
  const heading = el('div', 'card-heading');
  const title = siteID ? el('div', 'card-title') : button('', () => emit('activate', site.id), 'card-title');
  const branch = el('div', 'branch', site.git.branch || 'Folder');
  title.append(pathLabel(site), branch);
  title.title = siteID ? 'Drag to move this Site' : 'Go to this Site · Drag to move the panel';
  if (!siteID) title.setAttribute('aria-label', `Activate ${site.path}, ${site.git.branch || 'folder'}`);
  heading.append(title);
  const pullRequests = site.git.pullRequests || [];
  const openPR = number => emit('pr-open', {siteID:site.id, number});
  if (pullRequests.length) {
    const tags = el('div', 'card-prs');
    for (const pr of pullRequests) {
      const ci = prCheckIndicator(pr);
      const tag = button(`${ci.symbol ? `${ci.symbol} ` : ''}#${pr.number}`, () => openPR(pr.number), `pr-tag status-link ${prTagTone(pr)}`.trim());
      tag.dataset.prNumber = String(pr.number);
      const description = [`PR #${pr.number}`, pr.isDraft ? 'Draft' : pr.state, ci.label].filter(Boolean).join(' · ');
      tag.title = `Open ${description}`;
      tag.dataset.appTooltip = description;
      tag.setAttribute('aria-label', tag.title); tags.append(tag);
    }
    heading.append(tags);
  }
  parent.append(heading);
  const status = cardStatus(site.git);
  const body = el('div', 'card-status');
  function statusButton(view, cls, prNumber) {
    const node = button('', () => {
      const selection = window.getSelection();
      if (selection && !selection.isCollapsed && (node.contains(selection.anchorNode) || node.contains(selection.focusNode))) return;
      if (prNumber) openPR(prNumber);
      else emit('viewer-open', {siteID:site.id,kind:view || 'edits'});
    }, `${cls} status-link copyable`);
    if (prNumber) node.dataset.prNumber = String(prNumber);
    else node.dataset.viewKind = view || 'edits';
    return node;
  }
  function line(text, tone, symbol, title = text, view = 'edits', prNumber) {
    const row = statusButton(view, `status-line ${tone}`, prNumber);
    const icon = el('span', 'status-symbol', symbol); icon.setAttribute('aria-hidden', 'true');
    row.append(icon, el('span', '', text)); row.title = title; body.append(row);
  }
  function metrics(label, values, title, prNumber) {
    if (!values.length) return;
    const row = el('div', 'metric-row copyable'); row.append(el('span', 'metric-label', label));
    if (title) { row.classList.add('review-row'); row.title = title; row.setAttribute('aria-label', `${label}: ${title}`); }
    if (prNumber) row.dataset.prNumber = String(prNumber);
    for (const item of values) {
      const badge = statusButton(item.view, `metric ${item.tone}`, prNumber); badge.title = item.title; badge.setAttribute('aria-label', `View ${item.title}`);
      const symbol = el('span', 'metric-symbol', item.symbol); symbol.setAttribute('aria-hidden', 'true');
      if (item.symbol) badge.append(symbol);
      if (item.value !== '') badge.append(el('strong', '', item.value));
      badge.append(el('span', 'metric-unit', item.label)); row.append(badge);
    }
    body.append(row);
  }
  metrics('Local', status.local); metrics('Remote', status.remote);
  if (status.alert) line(status.alert.text, status.alert.tone, status.alert.symbol, status.alert.title || site.git.localNote || site.git.summary, status.alert.view);
  if (status.quiet) line(status.quiet, 'good quiet-status', '✓');
  if (status.base) line(status.base, 'warning', '↳', status.baseTitle);
  const singlePR = pullRequests.length === 1 ? pullRequests[0].number : null;
  if (status.pr) {
    for (const label of status.pr.split('; ')) {
      const number = Number(/^PR #(\d+)/.exec(label)?.[1]);
      const prNumber = pullRequests.some(pr => pr.number === number) ? number : null;
      line(singlePR ? label.replace(/^PR #\d+ · /, '') : label, `pr-status ${status.prTone}`, status.prTone === 'good' ? '✓' : '⑂', status.prTitle, 'edits', prNumber);
    }
  }
  if (status.prNote) line(status.prNote, 'freshness muted', '·', status.prTitle, 'edits', singlePR);
  for (const review of status.reviews) {
    const number = Number(/^PR #(\d+)/.exec(review.label)?.[1]);
    const prNumber = pullRequests.some(pr => pr.number === number) ? number : null;
    metrics(singlePR ? 'Review' : review.label, review.values, review.title, prNumber);
    if (review.note) line(review.note, 'freshness warning', '◷', review.title, 'edits', prNumber);
  }
  if (status.freshness) line(status.freshness, 'freshness muted', '◷', [site.git.remoteNote, site.git.remoteChecked ? `Last checked ${new Date(site.git.remoteChecked).toLocaleString()}` : ''].filter(Boolean).join('\n'), 'remote');
  parent.append(body);
  if (showIDEStatus) parent.append(developmentAppIndicator(site));
}

let list, decoration, layout, siteError, siteErrorTimer;
if (viewer) {
  document.body.className = 'git-viewer';
  let currentView = null, currentSnapshot = {sites:[]};
  const suggestions = el('section', 'git-view-suggestions'); suggestions.hidden = true;
  const updateSuggestions = mountViewerSuggestions(suggestions, request => emit('action-prepare', request));
  const updateOverview = () => updateSuggestions(currentSnapshot.sites.find(site => site.id === currentView?.siteID), currentSnapshot.action);
  renderViewerSnapshot = state => { currentSnapshot = state; updateOverview(); };
  const renderViewer = mountGitViewer(root, data => emit('viewer-request', data), suggestions, siteID => emit('branches-open', {siteID}));
  Events.On(`devjay:viewer-state:${params.get('viewer')}`, event => {
    currentView = event.data; renderViewer(currentView); updateOverview(); reportUIReady();
  });
  document.addEventListener('keydown', e => { if (e.key === 'Escape') emit('viewer-close'); });
} else if (actions) {
  document.body.className = 'suggestions';
  renderSuggestedActions = mountSuggestedActions(root, () => emit('suggestions-close'), (name, data) => emit(name, data));
  document.addEventListener('keydown', e => { if (e.key === 'Escape') emit('suggestions-close'); });
} else if (settings) {
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
  editorSettings = mountEditorSettings(root, data => emit('editor-settings-request', data));
  Events.On('devjay:editor-settings-state', event => editorSettings.renderEditor(event.data));
  root.append(el('h2', '', 'View settings'));
  for (const [key, title] of [['allDesktops', 'Control panel always visible'], ['onTop', 'Show all windows on top'], ['showIDEStatus', 'Show development app status']]) {
    const label = el('label', 'preference'); const box = el('input'); box.type = 'checkbox'; box.dataset.pref = key; box.onchange = () => emit(key, box.checked);
    label.append(box, document.createTextNode(title)); root.append(label);
  }
  root.append(el('p', 'help', 'Show IDEs, terminals and AI apps on Site cards. Turning this off stops app detection.'));
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
  const header = el('header');
  const grip = el('span', 'grip'); grip.title = 'Drag to move this Site';
  const minimise = button('−', () => Window.Minimise(), 'minimise-window'); minimise.setAttribute('aria-label', 'Minimize Site'); minimise.title = 'Minimize Site';
  header.append(grip, minimise);
  list = el('div', 'site-detail'); root.append(header, list, notice);
  list.dataset.id = siteID;
} else {
  document.body.className = 'strip'; document.body.dataset.decoration = 'none';
  const header = el('header');
  const grip = el('span', 'grip'); grip.title = 'DevJay · Drag to move';
  const close = button('×', () => Window.Close(), 'close-window'); close.setAttribute('aria-label', 'Quit DevJay'); close.title = 'Quit DevJay';
  header.append(grip, close); list = el('nav', 'cards'); list.setAttribute('aria-label', 'Sites'); root.append(header, list, notice);
  installPanelDrag(list, {
    enabled: () => snapshot.settingsOpen,
    layout: () => snapshot.layout || 'horizontal',
    busy: value => { panelDragging = value; if (!value) { render(snapshot); fitCards(); } },
    commit: ids => emit('order', ids),
    placements: () => cardPlacements(snapshot),
    place: positions => emit('placements', positions),
  });
  installCardActivation(list, id => emit('activate', id));
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
if (viewer || actions) root.append(notice);
const fitCards = !settings && !actions && !viewer ? installCardSizing(root, list, size => emit('fit-cards', size), () => panelDragging) : () => {};

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
  preview.style.gridTemplateColumns = ['.path', '.row-status', '.remove-site'].map(selector => `${row.querySelector(selector).getBoundingClientRect().width}px`).join(' ');
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
let appTooltipSignature;
function refreshAppTooltip(state) {
  if (settings || viewer || actions) return;
  const sites = siteID ? state.sites.filter(site => site.id === siteID) : state.sites;
  const signature = JSON.stringify([
    state.preferences?.showIDEStatus !== false,
    state.layout || 'horizontal', state.decoration || 'none',
    sites.map(site => [site.id, site.path, site.position?.label, state.placements?.[site.id], site.developmentAppsKnown,
      (site.developmentApps || []).map(app => [app.bundleID, app.name, app.agents, app.waitingAgents, app.workingAgents, app.windowCount]),
      (site.git.pullRequests || []).map(pr => [pr.number, pr.state, pr.isDraft, pr.ciState])]),
  ]);
  if (signature === appTooltipSignature) return;
  appTooltipSignature = signature;
  emit('app-tooltip-refresh');
}
function render(state) {
  report('snapshot', state);
  if (viewer) { renderViewerSnapshot(state); return; }
  snapshot = state;
  const showIDEStatus = state.preferences?.showIDEStatus !== false;
  if (actions) { renderSuggestedActions(state); return; }
  updateWaitingAttention(state);
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
  if (settings) {
    document.querySelectorAll('[data-pref]').forEach(n => { n.checked = n.dataset.pref === 'showIDEStatus' ? showIDEStatus : state.preferences[n.dataset.pref]; });
    document.querySelectorAll('[name=sorting]').forEach(n => { n.checked = n.value === (state.manual ? 'manual' : 'name'); });
    if (dragging) return;
    const signature = state.sites.map(s => s.id).join(',');
    if (list.dataset.signature !== signature) {
      const focused = document.activeElement?.closest('.site-row');
      const focusedID = focused?.dataset.id;
      const focusRemove = document.activeElement?.classList.contains('remove-site');
      list.dataset.signature = signature; list.replaceChildren();
      state.sites.forEach((site, index) => {
        const row = el('div', 'site-row'); row.dataset.id = site.id;
        const description = el('div', 'row-description'); description.append(pathLabel(site), el('div', 'row-status help')); row.append(description);
        const remove = button('×', () => emit('remove', site.id), 'remove-site');
        remove.setAttribute('aria-label', `Remove ${site.name}`); remove.title = 'Remove Site'; row.append(remove);
        row.tabIndex = 0; row.setAttribute('aria-label', `Reorder ${site.name}`); row.title = 'Drag to reorder, or use Up and Down arrow keys';
        row.onkeydown = e => { if (e.target === row && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) { e.preventDefault(); reorder(site.id, index + (e.key === 'ArrowUp' ? -1 : 1)); } };
        row.onpointerdown = e => { if (!e.target.closest('button')) { row.focus(); startSiteDrag(e, row, row); } };
        list.append(row);
      });
      const restored = [...list.children].find(row => row.dataset.id === focusedID);
      if (restored) (focusRemove ? restored.querySelector('.remove-site') : restored).focus();
    }
    for (const site of state.sites) { const row = [...list.children].find(n => n.dataset.id === site.id); const status = row.querySelector('.row-status'); status.textContent = [site.git.branch, site.position.label || 'Desktop unknown'].filter(Boolean).join(' · '); status.title = status.textContent; }
  } else if (siteID) {
    const site = state.sites.find(s => s.id === siteID); if (!site) { list.replaceChildren(); list.dataset.details = ''; return; }
    details(site, list, showIDEStatus); root.title = `${site.path}\n${site.position.label || 'Desktop unknown'}\nDrag to move this Site`;
  } else {
    if (!state.sites.length) {
      if (!list.firstElementChild?.classList.contains('empty')) list.replaceChildren(button('Add your first Site in Settings', () => emit('settings-open'), 'empty'));
      refreshAppTooltip(state);
      return;
    }
    for (const child of [...list.children]) if (!state.sites.some(s => s.id === child.dataset.id)) child.remove();
    state.sites.forEach((site, index) => {
      let card = [...list.children].find(n => n.dataset.id === site.id);
      if (!card) { card = el('div', 'card'); card.dataset.id = site.id; }
      const position = cardPlacements(state)[site.id];
      card.style.left = free ? `${position.x}px` : '';
      card.style.top = free ? `${position.y}px` : '';
      card.style.zIndex = free ? position.z : '';
      card.classList.toggle('visible', site.window.onDesktop);
      card.setAttribute('aria-label', `Activate ${site.path}, ${site.git.branch || 'folder'}, ${site.git.localNote || site.git.summary}${site.window.onDesktop ? ', visible on current desktop' : ''}`);
      details(site, card, showIDEStatus);
      card.title = [site.window.onDesktop ? 'Visible on current desktop' : '', site.path, site.git.branch, site.git.summary, site.git.localNote, site.git.github, site.git.remoteNote, site.position.label, site.git.tracking ? `Tracking ${site.git.tracking}` : '', site.git.remoteChecked ? `Remote checked ${new Date(site.git.remoteChecked).toLocaleString()}` : ''].filter(Boolean).join('\n');
      if (list.children[index] !== card) list.insertBefore(card, list.children[index] || null);
    });
  }
  refreshAppTooltip(state);
}
if (settings) Events.On('devjay:site-error', event => {
  clearTimeout(siteErrorTimer);
  siteError.textContent = event.data;
  siteError.hidden = !event.data;
  siteErrorTimer = setTimeout(() => { siteError.hidden = true; siteError.textContent = ''; }, 5000);
});
Events.On('devjay:snapshot', event => {
  // Initial documents still render and acknowledge recovery even if hidden.
  // Later hidden updates retain only the newest state until the window returns.
  if (renderedSnapshot && (!nativeVisible || document.hidden)) {
    deferredSnapshot = event.data;
    updateWaitingAttention(event.data);
    return;
  }
  deferredSnapshot = null;
  render(event.data);
  renderedSnapshot = true;
  updateMonitoringVisibility();
  if (!viewer) reportUIReady();
});
emit('hello');

if (viewer) emit('viewer-hello');
if (settings) emit('editor-settings-request', {operation: 'refresh'});
