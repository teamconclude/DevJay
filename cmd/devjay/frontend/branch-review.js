import { syntaxLines } from './syntax-highlight.js';
import { findTextMatches, filterSearchRows, appendSearchSyntax } from './diff-search.js';
import { defaultReviewIgnorePatterns, reviewIgnoreMatchers, reviewFileIgnored, ignorePatternsStorageKey, storedIgnorePatterns, saveIgnorePatterns } from './file-ignore.js';
export { defaultReviewIgnorePatterns, reviewIgnoreMatchers, reviewFileIgnored };
// Branch Review owns presentation and selection per window. Repository data is
// provided by the bounded, read-only Git reader; source text is never HTML.
const reviewRowLimit = 5000;
const reviewPreloadBatchSize = 8;

// Source payloads stay available for the whole comparison. Keep only a few
// expanded/tokenized models, which can be much larger than their source text.
function reviewModelCache() {
  const entries = new Map();
  return {
    get(key) { const value = entries.get(key); if (value) { entries.delete(key); entries.set(key, value); } return value; },
    set(key, value) { entries.delete(key); entries.set(key, value); if (entries.size > 4) entries.delete(entries.keys().next().value); },
  };
}

export function reviewTextLines(text = '') {
  if (!text) return [];
  const lines = text.split('\n');
  if (lines.at(-1) === '') lines.pop();
  return lines.map(line => line.endsWith('\r') ? line.slice(0, -1) : line);
}

export function reviewDiffModel(file = {}) {
  const before = reviewTextLines(file.oldText), after = reviewTextLines(file.newText);
  const hunks = file.hunks || [];
  const rows = [];
  let left = 0, right = 0;
  const line = (lines, index) => index < lines.length ? {number:index + 1, text:lines[index]} : null;
  const context = (oldEnd, newEnd) => {
    while (left < oldEnd || right < newEnd) {
      rows.push({kind:'context', before:left < oldEnd ? line(before, left++) : null, after:right < newEnd ? line(after, right++) : null});
    }
  };
  hunks.forEach((hunk, change) => {
    const oldStart = Math.max(left, Math.min(before.length, hunk.oldStart - (hunk.oldLines ? 1 : 0)));
    const newStart = Math.max(right, Math.min(after.length, hunk.newStart - (hunk.newLines ? 1 : 0)));
    context(oldStart, newStart);
    const oldEnd = Math.min(before.length, left + hunk.oldLines), newEnd = Math.min(after.length, right + hunk.newLines);
    while (left < oldEnd || right < newEnd) {
      rows.push({kind:'change', change, before:left < oldEnd ? line(before, left++) : null, after:right < newEnd ? line(after, right++) : null});
    }
  });
  context(before.length, after.length);
  return {rows, hunks};
}

export function reviewUnifiedRows(rows) {
  const result = [];
  for (let index = 0; index < rows.length;) {
    const row = rows[index];
    if (row.kind !== 'change') { result.push(row); index++; continue; }
    const block = [];
    while (index < rows.length && rows[index].kind === 'change' && rows[index].change === row.change) block.push(rows[index++]);
    for (const item of block) if (item.before) result.push({...item, after:null});
    for (const item of block) if (item.after) result.push({...item, before:null});
  }
  return result;
}

export function reviewVisibleRows(rows, expanded = new Set(), context = 3) {
  const visible = new Set(expanded);
  rows.forEach((row, index) => {
    if (row.kind !== 'change') return;
    for (let i = Math.max(0, index - context); i <= Math.min(rows.length - 1, index + context); i++) visible.add(i);
  });
  const result = [];
  for (let index = 0; index < rows.length;) {
    if (visible.has(index)) { result.push({...rows[index], rowIndex:index++}); continue; }
    const from = index;
    while (index < rows.length && !visible.has(index)) index++;
    result.push({kind:'gap', from, to:index, count:index - from});
  }
  return result;
}

export function reviewCommitSummary(hunk) {
  if (!hunk) return 'Select a changed block to see its commits.';
  const parts = [];
  if (hunk.uncommitted) parts.push('Uncommitted changes');
  for (const commit of hunk.commits || []) parts.push(`${commit.id?.slice(0, 8) || ''} ${commit.subject || 'Commit'}\n${commit.author || ''}${commit.date ? ' · ' + commit.date : ''}`.trim());
  if (hunk.note) parts.push(hunk.note);
  return parts.join('\n\n') || 'Commit attribution is unavailable for this block.';
}

export function reviewFileMatches(file, query) {
  const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  const path = `${file.path} ${file.oldPath || ''}`.toLocaleLowerCase();
  return terms.every(term => path.includes(term));
}

export function createBranchReview(root, request, options = {}) {
  const setText = (node, value) => { if (node.textContent !== value) node.textContent = value; };
  const setDisabled = (node, value) => { if (node.disabled !== !!value) node.disabled = !!value; };
  const el = (tag, cls = '', text) => { const node = document.createElement(tag); node.className = cls; if (text !== undefined) node.textContent = text; return node; };
  const button = (text, title, run, cls = '') => { const node = el('button', cls, text); node.type = 'button'; node.title = title; node.setAttribute('aria-label', title); node.onclick = run; return node; };
  let view = null, snapshotKey = '', selectedPath = '', activeChange = 0, layout = 'split', presentation = 'file', wrap = false;
  let highlighting = true;
  let revealPath = '', revealEdge = '';
  let scrollFrame = 0, selectionScrollTop = null;
  let prefetchFrame = 0, prefetchSequence = 0, prefetchBatch = null, visible = true, navigationFrame = 0;
  let loadingTimer = 0, loadingKey = '', displayedPath = '', displayedSnapshot = '';
  const syntaxCache = reviewModelCache();
  const searchCache = reviewModelCache();
  const modelCache = reviewModelCache();
  const visibleCards = new Set();
  let filesRenderKey = '';
  let cache = new Map(), cards = new Map(), expanded = new Map(), pages = new Map(), wanted = new Set(), failed = new Set(), pendingPath = '', renderKey = '';
  let editing = null, beginningEdit = '', beginningVersion = 0, finishingEdit = false, saving = false, saveVersion = 0, destroyed = false, observer = null, orphanVersion = -1;
  const draftKey = 'devjay-branch-review-draft';
  const frame = el('section', 'branch-review');
  const toolbar = el('header', 'br-toolbar'); toolbar.setAttribute('aria-label', 'Branch review controls');
  const identity = el('div', 'br-identity');
  const branchLabel = el('strong', 'br-branch', 'Branch review');
  const baseLabel = el('label', 'br-base'); baseLabel.append(el('span', '', 'from'));
  const baseSelect = el('select', 'br-base-select'); baseSelect.setAttribute('aria-label', 'Base branch for fork point'); baseLabel.append(baseSelect);
  identity.append(branchLabel, baseLabel);
  const nav = el('div', 'br-nav');
  const previousChange = button('↑', 'Previous change (Option+Up)', () => moveChange(-1));
  const nextChange = button('↓', 'Next change (Option+Down)', () => moveChange(1));
  const previousFile = button('←', 'Previous file (Option+Left)', () => moveFile(-1));
  const nextFile = button('→', 'Next file (Option+Right)', () => moveFile(1));
  const changePosition = el('span', 'br-position br-change-position', '0 / 0');
  changePosition.title = 'Change position in the current file';
  const position = el('span', 'br-position', '0 / 0');
  const changesNav = el('div', 'br-nav-group'); changesNav.setAttribute('role', 'group'); changesNav.setAttribute('aria-label', 'Changes in current file');
  const filesNav = el('div', 'br-nav-group'); filesNav.setAttribute('role', 'group'); filesNav.setAttribute('aria-label', 'Files in filtered list');
  changesNav.append(previousChange, changePosition, nextChange);
  filesNav.append(previousFile, position, nextFile);
  nav.append(changesNav, el('span', 'br-separator'), filesNav);
  const controls = el('div', 'br-controls');
  const modeSelect = el('select', 'br-presentation'); modeSelect.setAttribute('aria-label', 'Review presentation');
  for (const [value, label] of [['file', 'Full file'], ['continuous', 'Continuous']]) { const option = el('option', '', label); option.value = value; modeSelect.append(option); }
  const splitButton = button('◫', 'Side by side', () => setLayout('split'));
  const unifiedButton = button('☰', 'Unified', () => setLayout('unified'));
  const layouts = el('div', 'br-segmented'); layouts.append(splitButton, unifiedButton);
  const settings = el('details', 'br-options');
  const summary = el('summary', '', '⚙'); summary.title = 'Review options'; summary.setAttribute('aria-label', 'Review options');
  const optionsPanel = el('div', 'br-options-panel');
  const whitespaceLabel = el('label'); const whitespace = el('input'); whitespace.type = 'checkbox'; whitespaceLabel.append(whitespace, el('span', '', 'Ignore whitespace'));
  const wrapLabel = el('label'); const wrapInput = el('input'); wrapInput.type = 'checkbox'; wrapLabel.append(wrapInput, el('span', '', 'Wrap long lines'));
  const syntaxLabel = el('label'); const syntaxInput = el('input'); syntaxInput.type = 'checkbox'; syntaxInput.checked = true; syntaxLabel.append(syntaxInput, el('span', '', 'Syntax highlighting'));
  optionsPanel.append(el('strong', '', 'Review options'), whitespaceLabel, wrapLabel, syntaxLabel);
  settings.append(summary, optionsPanel);
  const editButton = button('Edit', 'Edit current working file', startEdit, 'br-edit-button');
  const refreshButton = button('↻', 'Refresh branch review', () => send('refresh'));
  const duplicateButton = button('＋', 'Open another branch review window', () => send('review-open'));
  controls.append(modeSelect, layouts, settings, editButton, refreshButton, duplicateButton);
  toolbar.append(identity, nav, controls);
  const notice = el('div', 'br-notice'); notice.setAttribute('role', 'status'); notice.hidden = true;
  const findBar = el('div', 'diff-find'); findBar.hidden = true;
  const findInput = el('input', 'diff-find-input'); findInput.type = 'search'; findInput.maxLength = 512;
  findInput.placeholder = 'Find in diff…'; findInput.setAttribute('aria-label', 'Find text in diff');
  const findStatus = el('span', 'diff-find-status'); findStatus.setAttribute('role', 'status');
  const findClose = button('×', 'Close find (Escape)', closeFind, 'diff-find-close');
  findBar.append(findInput, findStatus, findClose);
  const body = el('div', 'br-body');
  const sidebar = el('aside', 'br-sidebar');
  const filesHeader = el('div', 'br-files-header');
  const filesTitle = el('div', 'br-files-title', 'Files');
  const filter = el('input', 'br-filter'); filter.type = 'search'; filter.placeholder = 'Filter files…'; filter.setAttribute('aria-label', 'Filter changed files');
  const ignoreOptions = el('details', 'br-ignore-options');
  const ignoreSummary = el('summary', '', 'Ignored files');
  const patternsLabel = el('label', 'br-ignore-label', 'Ignore patterns');
  const ignorePatterns = el('textarea', 'br-ignore-patterns'); ignorePatterns.rows = 3; ignorePatterns.maxLength = 4096;
  ignorePatterns.setAttribute('aria-label', 'Ignore file patterns'); ignorePatterns.spellcheck = false;
  ignorePatterns.value = defaultReviewIgnorePatterns;
  patternsLabel.append(ignorePatterns);
  const showIgnoredLabel = el('label', 'br-show-ignored');
  const showIgnored = el('input'); showIgnored.type = 'checkbox'; showIgnored.setAttribute('aria-label', 'Show ignored files');
  showIgnoredLabel.append(showIgnored, el('span', '', 'Show ignored files'));
  const ignoreHelp = el('p', 'br-ignore-help', 'Separate patterns with commas or new lines. Names match in any folder; use folder/ for a folder, or ** to match across folders. Saved for this repository.');
  ignoreOptions.append(ignoreSummary, showIgnoredLabel, patternsLabel, ignoreHelp);
  filesHeader.append(filesTitle, filter, ignoreOptions);
  let ignoreMatchers = reviewIgnoreMatchers(ignorePatterns.value), ignoreStorageKey = '';
  const filesList = el('div', 'br-files'); filesList.setAttribute('role', 'listbox'); filesList.setAttribute('aria-label', 'Changed files'); filesList.tabIndex = 0;
  let listNavigation = false;
  filesList.onfocus = () => { listNavigation = true; };
  const commitPanel = el('section', 'br-commit-panel'); commitPanel.setAttribute('aria-label', 'Selected change commits');
  sidebar.append(filesHeader, filesList, commitPanel);
  const content = el('div', 'br-content'); content.tabIndex = 0; content.setAttribute('aria-label', 'Branch differences');
  const fileLoading = el('div', 'br-file-loading', 'Loading…'); fileLoading.hidden = true; fileLoading.setAttribute('role', 'status');
  body.append(sidebar, content, fileLoading);
  const footer = el('footer', 'br-footer');
  const comparisonLabel = el('span', 'br-comparison');
  const preloadStatus = el('span', 'br-preload-status');
  footer.append(comparisonLabel, preloadStatus);
  frame.append(toolbar, notice, findBar, body, footer); root.replaceChildren(frame);

  function send(operation, data = {}) {
    if (!view || destroyed) return;
    return request({operation, version:view.version, ...data});
  }
  function selectedFile() { return matchingFiles().find(file => file.path === selectedPath); }
  function selectedData() { return cache.get(selectedPath); }
  function matchingFiles() { return (view?.files || []).filter(file => (showIgnored.checked || !reviewFileIgnored(file, ignoreMatchers)) && reviewFileMatches(file, filter.value || '')); }
  function diffModel(data) {
    let model = modelCache.get(data);
    if (!model) { model = reviewDiffModel(data); modelCache.set(data, model); }
    return model;
  }
  function searchResult(data) {
    let result = searchCache.get(data);
    if (!result || result.query !== findInput.value) {
      result = {query:findInput.value, ...filterSearchRows(diffModel(data).rows, findInput.value, row => [row.before?.text || '', row.after?.text || ''])};
      searchCache.set(data, result);
    }
    return result;
  }
  function changeIndices(data) {
    if (!data) return [];
    if (!findInput.value) return (data.hunks || []).map((_, index) => index);
    return [...new Set(searchResult(data).rows.filter(row => row.kind === 'change').map(row => row.change))];
  }
  function displayedRows(data, path) {
    if (findInput.value) return searchResult(data).rows;
    const model = diffModel(data);
    return presentation === 'continuous' ? reviewVisibleRows(model.rows, expanded.get(path) || new Set()) : model.rows;
  }
  function updateFindStatus() {
    if (!findInput.value) { findStatus.textContent = 'Type to filter diff text'; return; }
    const files = presentation === 'continuous' ? matchingFiles() : (selectedFile() ? [selectedFile()] : []);
    const loaded = files.filter(file => cache.has(file.path));
    const count = loaded.reduce((total, file) => total + searchResult(cache.get(file.path)).matchingRows, 0);
    findStatus.textContent = `${count} matching ${count === 1 ? 'line' : 'lines'}${presentation === 'continuous' ? ` · ${loaded.length} / ${files.length} files loaded` : ''}`;
  }
  function closeFind() {
    findInput.value = ''; findBar.hidden = true; pages.clear(); renderContent(); renderCommits(); updateControls(); selectChange(activeChange, true); content.focus();
  }
  findInput.oninput = () => {
    pages.clear();
    const available = changeIndices(selectedData());
    if (available.length && !available.includes(activeChange)) activeChange = available[0];
    renderContent(); renderCommits(); updateControls(); content.scrollTop = 0; selectChange(activeChange, true);
  };
  function updateControls() {
    const files = matchingFiles(), index = files.findIndex(file => file.path === selectedPath), data = selectedData();
    const changes = changeIndices(data), count = changes.length, changeIndex = changes.indexOf(activeChange);
    const locked = !!editing || !!beginningEdit;
    setText(position, files.length ? `${Math.max(0, index + 1)} / ${files.length}` : '0 / 0');
    position.title = 'File position in the filtered list';
    position.setAttribute('aria-label', `File ${Math.max(0, index + 1)} of ${files.length}`);
    setDisabled(previousFile, locked || index <= 0);
    setDisabled(nextFile, locked || index < 0 || index >= files.length - 1);
    const changeNavigationLocked = locked || index < 0 || (!data && !failed.has(selectedPath));
    setDisabled(previousChange, changeNavigationLocked || (!changes.some(change => change < activeChange) && index <= 0));
    setDisabled(nextChange, changeNavigationLocked || (!changes.some(change => change > activeChange) && index >= files.length - 1));
    setText(changePosition, `${changeIndex + 1} / ${count}`);
    changePosition.setAttribute('aria-label', `Change ${changeIndex + 1} of ${count} in current file`);
    setDisabled(editButton, locked || !!view?.loading || !!pendingPath || !data?.editable);
    editButton.title = data?.editable ? 'Edit current working file' : 'Only complete, regular working text files can be edited';
    for (const control of [baseSelect, whitespace, refreshButton]) setDisabled(control, locked || !!view?.loading);
    for (const control of [modeSelect, splitButton, unifiedButton, syntaxInput, filter, ignorePatterns, showIgnored, findInput, findClose]) setDisabled(control, locked);
    splitButton.setAttribute('aria-pressed', String(layout === 'split'));
    unifiedButton.setAttribute('aria-pressed', String(layout === 'unified'));
    frame.classList.toggle('br-loading', !!view?.loading);
    frame.setAttribute('aria-busy', String(!!view?.loading));
    updateFileLoading();
  }
  function updateFileLoading() {
    const waiting = visible && !destroyed && !editing && !beginningEdit && selectedPath && !cache.has(selectedPath) && !failed.has(selectedPath);
    const key = waiting ? JSON.stringify([snapshotKey, selectedPath]) : '';
    if (key === loadingKey) return;
    if (loadingTimer) clearTimeout(loadingTimer);
    loadingTimer = 0; loadingKey = key; fileLoading.hidden = true;
    if (key) loadingTimer = setTimeout(() => {
      loadingTimer = 0;
      if (!destroyed && visible && loadingKey === key) fileLoading.hidden = false;
    }, 250);
  }
  function updateNotice(extra = '') {
    const messages = [view?.error, extra || view?.review?.note, view?.filesLimited ? 'The file list is limited. Narrow the comparison in your Git client for omitted files.' : ''].filter(Boolean);
    notice.textContent = messages.join(' '); notice.hidden = !messages.length;
    notice.classList.toggle('br-error', !!view?.error);
  }
  function renderFiles() {
    const hadFocus = filesList.contains(document.activeElement);
    const files = matchingFiles();
    const total = view?.files?.length || 0;
    const ignored = (view?.files || []).filter(file => reviewFileIgnored(file, ignoreMatchers)).length;
    ignoreSummary.textContent = `Ignored files · ${showIgnored.checked ? 0 : ignored} hidden`;
    filesTitle.textContent = `${files.length}${files.length !== total ? ' / ' + total : ''} ${files.length === 1 ? 'file' : 'files'}`;
    const emptyText = files.length ? '' : view?.loading ? 'Loading branch changes…' : view?.error ? 'Branch comparison unavailable.' : total ? 'No files match your filters. Adjust the search or show ignored files.' : 'No changes since the branch fork point.';
    const key = JSON.stringify([files.map(file => [file.path, file.oldPath, file.status, file.binary, file.statsKnown, file.added, file.removed]), emptyText]);
    if (key === filesRenderKey) {
      for (const row of filesList.querySelectorAll('.br-file')) row.setAttribute('aria-selected', String(row.dataset.path === selectedPath));
      updateControls(); return;
    }
    filesRenderKey = key;
    const fragment = document.createDocumentFragment();
    for (const file of files) {
      const row = button('', file.oldPath ? `${file.oldPath} → ${file.path}` : file.path, () => { if (editing || beginningEdit) return; listNavigation = true; selectFile(file.path); filesList.focus({preventScroll:true}); }, 'br-file');
      row.dataset.path = file.path; row.setAttribute('role', 'option'); row.setAttribute('aria-selected', String(file.path === selectedPath)); row.tabIndex = -1;
      const path = el('span', 'br-file-path');
      const slash = file.path.lastIndexOf('/');
      path.append(el('span', 'br-file-name', file.path.slice(slash + 1)));
      if (slash >= 0) path.append(el('span', 'br-file-directory', file.path.slice(0, slash)));
      const stats = el('span', 'br-file-stats');
      if (file.binary) stats.append(el('span', '', 'binary'));
      else if (file.statsKnown) stats.append(el('span', 'br-added', `+${file.added || 0}`), el('span', 'br-removed', `−${file.removed || 0}`));
      row.append(el('span', `br-file-status br-status-${file.status?.[0] || 'M'}`, file.status?.[0] || 'M'), path, stats);
      fragment.append(row);
    }
    if (!files.length) fragment.append(el('p', 'br-empty', emptyText));
    filesList.replaceChildren(fragment);
    if (hadFocus) filesList.focus({preventScroll:true});
    updateControls();
  }
  function renderCommits() {
    if (presentation === 'file' && !selectedData() && !failed.has(selectedPath) && displayedPath && displayedSnapshot === snapshotKey) return;
    const data = selectedData(), hunk = changeIndices(data).includes(activeChange) ? data?.hunks?.[activeChange] : null;
    const heading = el('div', 'br-commit-heading', 'Change details');
    const nodes = [heading];
    if (hunk) {
      const range = hunk.newLines ? `Current lines ${hunk.newStart}–${hunk.newStart + hunk.newLines - 1}` : `Removed lines ${hunk.oldStart}–${hunk.oldStart + hunk.oldLines - 1}`;
      nodes.push(el('p', 'br-commit-range', range));
      if (hunk.uncommitted) { const uncommitted = el('div', 'br-commit'); uncommitted.append(el('strong', '', 'Uncommitted changes'), el('p', 'br-muted', 'Present in your working files; not part of a commit.')); nodes.push(uncommitted); }
      for (const commit of hunk.commits || []) nodes.push(commitNode(commit));
      if (hunk.note) nodes.push(el('p', 'br-muted', hunk.note));
      if (!hunk.uncommitted && !hunk.commits?.length && !hunk.note) nodes.push(el('p', 'br-muted', 'Commit attribution is unavailable for this block.'));
    } else nodes.push(el('p', 'br-muted', 'Select a changed block to inspect its commits.'));
    if (data?.commits?.length) {
      const details = el('details', 'br-file-history'); details.append(el('summary', '', 'Commits touching this file'));
      for (const commit of data.commits) details.append(commitNode(commit));
      nodes.push(details);
    }
    commitPanel.replaceChildren(...nodes);
  }
  function commitNode(commit) {
    const node = el('article', 'br-commit');
    node.append(el('strong', 'br-commit-subject', commit.subject || 'Commit'), el('p', 'br-commit-author', commit.author || 'Unknown author'));
    const date = new Date(commit.date);
    const when = Number.isNaN(date.getTime()) ? commit.date || '' : date.toLocaleString(undefined, {dateStyle:'medium', timeStyle:'short'});
    const meta = el('p', 'br-commit-meta'); meta.append(el('code', '', commit.id?.slice(0, 8) || ''), el('span', '', when)); node.append(meta);
    return node;
  }
  function revealNode(node, block) {
    node?.scrollIntoView({block, inline:'nearest'});
    // A short file near the end cannot always align with the viewport top.
    // Keep an explicit selection until the reader moves vertically again.
    selectionScrollTop = content.scrollTop;
  }
  function syncVisibleFile() {
    scrollFrame = 0;
    if (destroyed || presentation !== 'continuous' || editing || beginningEdit || view?.loading || content.scrollTop === selectionScrollTop) return;
    selectionScrollTop = null;
    const area = content.getBoundingClientRect();
    if (area.height <= 0) return;
    const readingTop = area.top + Math.min(24, area.height / 4);
    let current;
    if (content.scrollTop > 0 && content.scrollTop + content.clientHeight >= content.scrollHeight - 1) current = [...cards.values()].at(-1);
    else for (const card of cards.values()) {
      const rect = card.getBoundingClientRect();
      if (rect.bottom > readingTop && rect.top < area.bottom) { current = card; break; }
    }
    const path = current?.dataset.path;
    if (!path || path === selectedPath) return;
    selectedPath = path; revealPath = ''; revealEdge = '';
    const visibleChange = [...current.querySelectorAll('.br-change')].find(node => {
      const rect = node.getBoundingClientRect();
      return rect.bottom > readingTop && rect.top < area.bottom;
    });
    activeChange = visibleChange ? Number(visibleChange.dataset.change) : changeIndices(selectedData())[0] || 0;
    // Update the existing sidebar nodes so scrolling never steals focus or
    // rebuilds the diff. Only the sidebar itself follows an offscreen row.
    for (const row of filesList.querySelectorAll('.br-file')) {
      row.setAttribute('aria-selected', String(row.dataset.path === path));
      if (row.dataset.path === path) {
        const rect = row.getBoundingClientRect(), list = filesList.getBoundingClientRect();
        if (rect.top < list.top) filesList.scrollTop += rect.top - list.top;
        else if (rect.bottom > list.bottom) filesList.scrollTop += rect.bottom - list.bottom;
      }
    }
    for (const node of content.querySelectorAll('.br-change')) node.classList.toggle('br-current-change', node.dataset.path === path && Number(node.dataset.change) === activeChange);
    renderCommits(); updateControls();
    if (!cache.has(path) && !failed.has(path)) { wanted.add(path); schedulePrefetch(); }
  }
  function scheduleVisibleFile() {
    if (destroyed || presentation !== 'continuous' || scrollFrame) return;
    scrollFrame = requestAnimationFrame(syncVisibleFile);
  }
  function trackScroll(event) { if (event.target === content) scheduleVisibleFile(); }
  content.addEventListener('scroll', trackScroll, {passive:true});
  function selectFile(path, scroll = true, edge = '', deferPaint = false) {
    if (editing || beginningEdit || !view?.files?.some(file => file.path === path)) return;
    const changed = selectedPath !== path;
    if (!changed) { if (failed.delete(path)) { updateFileLoading(); schedulePrefetch(); } return; }
    selectedPath = path;
    if (changed) {
      const available = changeIndices(cache.get(path));
      activeChange = (edge === 'last' ? available.at(-1) : available[0]) || 0;
      revealPath = path; revealEdge = edge;
    }
    // A held key moves the selection immediately but paints only its latest
    // target per frame. Skipped files never become foreground Git reads.
    wanted.clear(); failed.delete(path);
    renderFiles();
    const paint = () => {
      renderCommits();
      if (presentation === 'file') { if (renderContent() && changed) content.scrollTop = 0; }
      else if (scroll) revealNode(cards.get(selectedPath), 'start');
      if (cache.has(selectedPath) && changed) { revealPath = ''; revealEdge = ''; selectChange(activeChange, presentation === 'file' || !!edge); }
    };
    if (navigationFrame) cancelAnimationFrame(navigationFrame);
    navigationFrame = 0;
    if (deferPaint) navigationFrame = requestAnimationFrame(() => { navigationFrame = 0; paint(); });
    else paint();
    schedulePrefetch();
  }
  function moveFile(direction) {
    const files = matchingFiles(), index = files.findIndex(file => file.path === selectedPath), next = files[index + direction];
    if (next) selectFile(next.path);
  }
  function selectChange(index, scroll = false, path = selectedPath) {
    if (editing || beginningEdit) return;
    const data = cache.get(path);
    const available = changeIndices(data);
    if (!available.length) return;
    selectionScrollTop = content.scrollTop;
    revealPath = ''; revealEdge = '';
    if (path !== selectedPath) { selectedPath = path; renderFiles(); }
    activeChange = available.find(change => change >= index) ?? available.at(-1);
    renderCommits(); updateControls();
    for (const node of content.querySelectorAll('.br-change')) node.classList.toggle('br-current-change', node.dataset.path === path && Number(node.dataset.change) === activeChange);
    if (scroll) {
      let visible = displayedRows(data, path);
      if (layout === 'unified') visible = reviewUnifiedRows(visible);
      const rowIndex = visible.findIndex(row => row.change === activeChange);
      const page = Math.floor(Math.max(0, rowIndex) / reviewRowLimit);
      if ((pages.get(path) || 0) !== page) { pages.set(path, page); renderCard(path); }
      const row = [...(cards.get(path)?.querySelectorAll('.br-change') || [])].find(node => Number(node.dataset.change) === activeChange);
      revealNode(row, 'center');
    }
  }
  function moveChange(direction) {
    if ((direction < 0 ? previousChange : nextChange).disabled) return;
    const available = changeIndices(selectedData());
    const targetChange = direction < 0 ? [...available].reverse().find(change => change < activeChange) : available.find(change => change > activeChange);
    if (targetChange !== undefined) { selectChange(targetChange, true); return; }
    const files = matchingFiles(), index = files.findIndex(file => file.path === selectedPath), next = files[index + direction];
    if (next) selectFile(next.path, true, direction < 0 ? 'last' : 'first');
  }
  function setLayout(value) { if (editing) return; layout = value; renderContent(); updateControls(); selectChange(activeChange, true); }
  function remember(path, data) {
    cache.delete(path); cache.set(path, data);
    // Retain the complete comparison until its snapshot changes. Both the
    // file list and individual source payloads are bounded by the Git reader.
  }
  function snapshotIdentity(value) {
    const review = value?.review;
    return JSON.stringify([value?.path, value?.head, value?.captured, review?.worktreeFingerprint, review?.base, review?.ignoreWhitespace]);
  }
  function schedulePrefetch() {
    if (destroyed || prefetchFrame || !visible || !view?.review || !matchingFiles().some(file => !cache.has(file.path) && !failed.has(file.path))) return;
    prefetchFrame = requestAnimationFrame(() => {
      prefetchFrame = 0;
      if (!visible || !view?.review || view.loading || view.error || prefetchBatch || editing || beginningEdit || destroyed) return;
      const files = matchingFiles(), eligible = new Set(files.map(file => file.path));
      const paths = [...new Set([selectedPath, ...wanted, ...eligible])].filter(path => eligible.has(path) && !cache.has(path) && !failed.has(path)).slice(0, reviewPreloadBatchSize);
      updatePreloadStatus();
      if (!paths.length) return;
      const indices = paths.map(path => view.files.findIndex(file => file.path === path));
      const batch = {id:++prefetchSequence, version:view.version, paths:new Set(paths)};
      prefetchBatch = batch;
      const failedRequest = () => {
        if (prefetchBatch !== batch) return;
        for (const path of batch.paths) failed.add(path);
        prefetchBatch = null; updatePreloadStatus(); showSelectedReadFailure(); schedulePrefetch();
      };
      Promise.resolve(send('review-prefetch', {prefetchID:batch.id, prefetch:indices})).then(result => { if (result === false) failedRequest(); }).catch(failedRequest);
    });
  }
  function updatePreloadStatus() {
    const files = matchingFiles(), loaded = files.filter(file => cache.has(file.path)).length, unavailable = files.filter(file => failed.has(file.path)).length;
    const label = !files.length ? '' : loaded === files.length ? `${loaded} files ready` : `${loaded} / ${files.length} files ready${unavailable ? ` · ${unavailable} unavailable` : ' · Loading review…'}`;
    if (preloadStatus.textContent !== label) preloadStatus.textContent = label;
  }
  function showSelectedReadFailure() {
    if (failed.has(selectedPath) && !cache.has(selectedPath)) {
      if (presentation === 'file') renderContent(); else renderCard(selectedPath);
      renderCommits(); updateControls();
    }
  }
  function prefetch(next) {
    const data = next?.review?.selected;
    if (destroyed || !visible || editing || beginningEdit || !prefetchBatch || next?.prefetchID !== prefetchBatch.id || snapshotIdentity(next) !== snapshotKey) return;
    if (next.done) {
      // Missing results are failures, not an infinite automatic retry loop.
      for (const path of prefetchBatch.paths) if (!cache.has(path)) failed.add(path);
      prefetchBatch = null; updatePreloadStatus(); showSelectedReadFailure(); schedulePrefetch(); return;
    }
    if (!data?.path || !prefetchBatch.paths.has(data.path) || !view.files.some(file => file.path === data.path && (file.oldPath || '') === (data.oldPath || ''))) return;
    prefetchBatch.paths.delete(data.path);
    if (next.error) failed.add(data.path);
    else if (!cache.has(data.path)) remember(data.path, data);
    wanted.delete(data.path); updatePreloadStatus();
    // Cache every file, but do not paint off-screen cards as preload results
    // arrive. The viewport observer renders them when they come into view.
    if (data.path === selectedPath && presentation === 'file' && displayedPath !== selectedPath) renderContent();
    else if (data.path === selectedPath || (presentation === 'continuous' && (visibleCards.has(data.path) || typeof IntersectionObserver === 'undefined'))) renderCard(data.path);
    if (data.path === selectedPath) {
      renderCommits(); updateControls();
      if (revealPath === selectedPath && cache.has(selectedPath)) {
        const edge = revealEdge; revealPath = ''; revealEdge = '';
        selectChange(edge === 'last' ? Math.max(0, (data.hunks?.length || 0) - 1) : activeChange, presentation === 'file' || !!edge);
      }
    }
  }
  function observeCards() {
    observer?.disconnect();
    if (presentation !== 'continuous' || typeof IntersectionObserver === 'undefined') return;
    observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const path = entry.target.dataset.path;
        if (!entry.isIntersecting) { visibleCards.delete(path); continue; }
        const wasVisible = visibleCards.has(path); visibleCards.add(path);
        if (cache.has(path)) { if (!wasVisible) renderCard(path); }
        else if (!failed.has(path)) wanted.add(path);
      }
      schedulePrefetch();
    }, {root:content, rootMargin:'250px'});
    for (const card of cards.values()) observer.observe(card);
  }
  function contentKey() {
    const signature = JSON.stringify((view?.files || []).map(file => [file.path, file.oldPath, file.status, file.added, file.removed]));
    return JSON.stringify([signature, presentation, layout, presentation === 'file' ? selectedPath : '', snapshotKey]);
  }
  function renderContent() {
    if (editing) return false;
    options.beforeRender?.(content);
    const files = presentation === 'continuous' ? matchingFiles() : (selectedFile() ? [selectedFile()] : []);
    // Keep the actual displayed file/header, DOM and scroll position until the
    // requested file is ready. A quick read never flashes a placeholder.
    if (presentation === 'file' && files.length && !cache.has(selectedPath) && !failed.has(selectedPath) && displayedPath && displayedSnapshot === snapshotKey) return false;
    const oldScroll = content.scrollTop;
    visibleCards.clear();
    cards = new Map(); content.classList.toggle('br-continuous', presentation === 'continuous');
    content.classList.toggle('br-wrap', wrap);
    displayedPath = presentation === 'file' && cache.has(selectedPath) ? selectedPath : '';
    displayedSnapshot = snapshotKey;
    const fragment = document.createDocumentFragment();
    for (const file of files) {
      const card = el('article', 'br-file-card'); card.dataset.path = file.path; cards.set(file.path, card); fragment.append(card);
    }
    if (!files.length) fragment.append(el('div', 'br-empty-state', view?.loading ? 'Loading branch comparison…' : view?.error ? 'Refresh to retry the branch comparison.' : view?.files?.length ? 'No files match your filters. Adjust the search or show ignored files.' : 'There are no changes to review.'));
    content.replaceChildren(fragment);
    for (const file of files) renderCard(file.path);
    content.scrollTop = oldScroll;
    renderKey = contentKey();
    updateFindStatus();
    observeCards();
    return true;
  }
  function renderCard(path) {
    const card = cards.get(path); if (!card) return;
    options.beforeRender?.(card);
    const data = cache.get(path), file = view?.files?.find(file => file.path === path);
    updateFindStatus();
    const horizontal = [...card.querySelectorAll('.br-horizontal-scroll')].map(node => node.scrollLeft);
    const heading = el('header', 'br-file-heading');
    heading.append(el('strong', '', path));
    if (file?.oldPath) heading.append(el('span', 'br-muted', `← ${file.oldPath}`));
    const nodes = [heading];
    if (!data) {
      if (failed.has(path)) nodes.push(el('p', 'br-empty', 'Could not load this file.'), button('Retry', `Retry loading ${path}`, () => { failed.delete(path); wanted.add(path); updateFileLoading(); schedulePrefetch(); }, 'br-load-file'));
      card.replaceChildren(...nodes); return;
    }
    if (data.note) nodes.push(el('p', 'br-file-note', data.note));
    if (data.binary || data.limited) {
      nodes.push(el('p', 'br-empty', data.binary ? 'Binary file changed. A text comparison is unavailable.' : 'This file is too large for a complete text comparison. Open it in your editor.'));
      card.replaceChildren(...nodes); return;
    }
    const labels = el('div', `br-side-labels ${layout === 'split' ? 'br-split' : ''}`);
    if (layout === 'split') {
      for (const [side, text, source] of [['left', `Fork point · ${view.review.base?.slice(0, 8) || ''}`, data.oldText], ['right', 'Current working file', data.newText]]) {
        const label = el('div', 'br-column-label'); label.append(el('span', '', text));
        const scroll = el('div', 'br-horizontal-scroll'); scroll.tabIndex = 0; scroll.setAttribute('aria-label', `Scroll ${side === 'left' ? 'original' : 'current'} file horizontally`);
        const track = el('div', 'br-horizontal-track');
        const longest = reviewTextLines(source).reduce((length, line) => Math.max(length, Array.from(line.replace(/\t/g, '    ')).length), 0);
        track.style.width = `${longest * 12 + 80}px`; scroll.append(track);
        scroll.onscroll = () => card.style.setProperty(`--br-${side}-scroll`, `${-scroll.scrollLeft}px`);
        label.append(scroll); labels.append(label);
      }
    } else labels.append(el('span', '', `Fork point · ${view.review.base?.slice(0, 8) || ''}`), el('span', '', 'Current working file'));
    nodes.push(labels);
    const model = diffModel(data);
    let syntax = null;
    if (highlighting) {
      syntax = syntaxCache.get(data);
      if (!syntax) {
        // Each complete version has its own lexer state, including context
        // hidden by the continuous view or a previous page of a large file.
        syntax = {
          before:syntaxLines(data.oldPath || path, reviewTextLines(data.oldText).join('\n')),
          after:syntaxLines(path, reviewTextLines(data.newText).join('\n')),
        };
        syntaxCache.set(data, syntax);
      }
    }
    let rows = displayedRows(data, path);
    if (layout === 'unified') rows = reviewUnifiedRows(rows);
    const total = rows.length, page = Math.min(pages.get(path) || 0, Math.floor(Math.max(0, total - 1) / reviewRowLimit));
    if (total > reviewRowLimit) {
      const paging = el('div', 'br-row-paging');
      const previous = button('↑ Previous lines', 'Show previous lines', () => { pages.set(path, page - 1); renderCard(path); }); previous.disabled = !page;
      const next = button('Next lines ↓', 'Show next lines', () => { pages.set(path, page + 1); renderCard(path); }); next.disabled = (page + 1) * reviewRowLimit >= total;
      paging.append(previous, el('span', '', `Rows ${page * reviewRowLimit + 1}–${Math.min(total, (page + 1) * reviewRowLimit)} of ${total}`), next); nodes.push(paging);
      rows = rows.slice(page * reviewRowLimit, (page + 1) * reviewRowLimit);
    }
    const code = el('div', `br-code ${layout === 'split' ? 'br-split' : 'br-unified'}`);
    const fragment = document.createDocumentFragment(), highlightBudget = {remaining:2048};
    for (const row of rows) {
      if (row.kind === 'search-gap') { fragment.append(el('div', 'diff-search-gap', `⋯ ${row.count} lines hidden`)); continue; }
      if (row.kind === 'gap') { fragment.append(gapNode(path, row)); continue; }
      if (layout === 'split') {
        const node = el('div', `br-row ${row.kind === 'change' ? 'br-change' : ''}`);
        node.append(cellNode(row.before, row.kind === 'change' ? 'delete' : 'context', syntax?.before, highlightBudget, path, 'LEFT', data.oldText), cellNode(row.after, row.kind === 'change' ? 'add' : 'context', syntax?.after, highlightBudget, path, 'RIGHT', data.newText));
        registerChange(node, row, path, model); fragment.append(node);
      } else if (row.kind === 'change') {
        for (const [side, kind] of [[row.before, 'delete'], [row.after, 'add']]) if (side) {
          const node = el('div', `br-row br-change br-${kind}`);
          node.append(lineNumber(kind === 'delete' ? side : null, path, 'LEFT', data.oldText), lineNumber(kind === 'add' ? side : null, path, 'RIGHT', data.newText), el('span', 'br-sign', kind === 'delete' ? '−' : '+'), sourceNode(side, kind === 'delete' ? syntax?.before : syntax?.after, 'br-line-text', highlightBudget));
          registerChange(node, row, path, model); fragment.append(node);
        }
      } else {
        // Whitespace-ignored context can retain different text on each side.
        // Show the original when it is the only side matching the search.
        const originalMatch = findInput.value && row.before && !findTextMatches(row.after?.text || '', findInput.value).length && findTextMatches(row.before.text, findInput.value).length;
        const source = originalMatch ? row.before : row.after || row.before;
        const node = el('div', 'br-row'); node.append(lineNumber(row.before, path, 'LEFT', data.oldText), lineNumber(row.after, path, 'RIGHT', data.newText), el('span', 'br-sign', ''), sourceNode(source, originalMatch || !row.after ? syntax?.before : syntax?.after, 'br-line-text', highlightBudget)); fragment.append(node);
      }
    }
    code.append(fragment); nodes.push(code);
    if (findInput.value && !searchResult(data).matchingRows) nodes.push(el('p', 'diff-search-empty', 'No matching text in this file.'));
    if (!model.rows.length) nodes.push(el('p', 'br-empty', 'Empty file.'));
    else if (!model.hunks.length) nodes.push(el('p', 'br-file-note', view.review.ignoreWhitespace ? 'No text differences with whitespace ignored.' : 'Only file metadata changed.'));
    if ((data.oldText && !data.oldText.endsWith('\n')) || (data.newText && !data.newText.endsWith('\n'))) nodes.push(el('p', 'br-file-note', `No newline at end of ${data.oldText && !data.oldText.endsWith('\n') ? 'original' : ''}${data.oldText && !data.oldText.endsWith('\n') && data.newText && !data.newText.endsWith('\n') ? ' and ' : ''}${data.newText && !data.newText.endsWith('\n') ? 'current' : ''} file.`));
    card.replaceChildren(...nodes);
    options.afterRender?.(card);
    [...card.querySelectorAll('.br-horizontal-scroll')].forEach((node, index) => { node.scrollLeft = horizontal[index] || 0; node.onscroll(); });
  }
  function sourceNode(line, syntax, cls = 'br-line-text', highlightBudget = null) {
    const node = el('span', cls);
    const text = line?.text || '';
    appendSearchSyntax(node, text, syntax?.[(line?.number || 0) - 1], findInput.value, 0, text.length, highlightBudget);
    return node;
  }
  function lineNumber(line, path, side, text) {
    const node = el('span', 'br-line-number', line?.number || '');
    if (line && view?.reviewPRs?.length) options.commentLine?.(node, {path, side, line:line.number, text});
    return node;
  }
  function cellNode(line, kind, syntax, highlightBudget, path, side, source) {
    const node = el('div', `br-cell${line ? ' br-' + kind : ' br-blank'}`);
    const text = el('span', 'br-line-text'); text.append(sourceNode(line, syntax, 'br-source', highlightBudget));
    node.append(lineNumber(line, path, side, source), el('span', 'br-sign', line ? kind === 'add' ? '+' : kind === 'delete' ? '−' : '' : ''), text);
    return node;
  }
  function registerChange(node, row, path, model) {
    if (row.kind !== 'change') return;
    node.dataset.path = path; node.dataset.change = String(row.change);
    node.title = reviewCommitSummary(model.hunks[row.change]);
    node.classList.toggle('br-current-change', path === selectedPath && row.change === activeChange);
    node.onclick = () => selectChange(row.change, false, path);
    // Hover provides the commit tooltip without moving the navigation cursor.
    // Scrolling a new block under the pointer must not undo an arrow jump.
  }
  function gapNode(path, row) {
    const gap = el('div', 'br-gap');
    const expand = (edge, all = false) => {
      const values = expanded.get(path) || new Set();
      const from = all || edge === 'start' ? row.from : Math.max(row.from, row.to - 20), to = all || edge === 'end' ? row.to : Math.min(row.to, row.from + 20);
      for (let index = from; index < to; index++) values.add(index);
      expanded.set(path, values); renderCard(path);
    };
    gap.append(button('↓', 'Expand 20 lines below the preceding change', () => expand('start')), button(`${row.count} unchanged lines`, `Expand all ${row.count} unchanged lines`, () => expand('start', true)), button('↑', 'Expand 20 lines above the next change', () => expand('end')));
    return gap;
  }
  function startEdit() {
    const data = selectedData(); if (!data?.editable || view?.loading || pendingPath || editing || beginningEdit) return;
    // The displayed cached file need not be the backend selection. Capture a
    // fresh payload for this exact path before acquiring the native guard.
    beginningEdit = selectedPath; beginningVersion = view.version; wanted.clear(); pendingPath = selectedPath;
    const index = view.files.findIndex(file => file.path === selectedPath);
    Promise.resolve(send('review-file', {file:index})).then(result => { if (result === false) { beginningEdit = ''; pendingPath = ''; updateControls(); } });
    updateControls();
  }
  function persistDraft() {
    if (!editing) return;
    try { sessionStorage.setItem(draftKey, JSON.stringify({path:editing.path, token:editing.token, original:editing.original, rawOriginal:editing.rawOriginal, value:editing.textarea.value, crlf:editing.crlf})); } catch { /* Storage can be disabled; the live draft stays in memory. */ }
  }
  function storedDraft() { try { return JSON.parse(sessionStorage.getItem(draftKey) || 'null'); } catch { return null; } }
  function clearDraft() { try { sessionStorage.removeItem(draftKey); } catch { /* No persistent draft to remove. */ } }
  function openEditor(data) {
    beginningEdit = '';
    editing = {path:selectedPath, token:data.token, original:data.newText, rawOriginal:data.newText, crlf:/\r\n/.test(data.newText) && !/(^|[^\r])\n/.test(data.newText), textarea:el('textarea', 'br-edit-text'), discard:null, acknowledged:false, guardVersion:view.version};
    editing.textarea.value = data.newText; editing.original = editing.textarea.value; editing.textarea.readOnly = true; editing.textarea.spellcheck = false; editing.textarea.setAttribute('autocorrect', 'off'); editing.textarea.setAttribute('autocapitalize', 'off'); editing.textarea.setAttribute('aria-label', `Edit ${selectedPath}`);
    send('review-edit', {editing:true, file:view.file, token:data.token}); observer?.disconnect();
    const editor = el('section', 'br-editor'); const bar = el('header', 'br-editor-bar');
    const save = button('Save', 'Save changes to working file (Command+S)', saveEdit, 'br-save');
    const cancel = button('Cancel', 'Cancel editing', cancelEdit, 'br-cancel');
    bar.append(el('strong', '', selectedPath), el('span', 'br-muted', 'Working file only'), cancel, save);
    const warning = el('p', 'br-edit-warning', 'Preparing editor…'); save.disabled = true; cancel.disabled = true;
    editor.append(bar, warning, editing.textarea); content.replaceChildren(editor);
    editing.save = save; editing.cancel = cancel; editing.warning = warning;
    editing.textarea.oninput = () => { if (editing) { persistDraft(); editing.discard?.remove(); editing.discard = null; editing.warning.textContent = editing.textarea.value === editing.original ? 'No unsaved changes.' : 'Unsaved changes · Save changes the working file.'; } };
    editing.textarea.onkeydown = event => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') { event.preventDefault(); saveEdit(); }
      if (event.key === 'Tab' && !event.metaKey && !event.ctrlKey && editing?.acknowledged && !finishingEdit && !saving && !editing.textarea.readOnly) {
        event.preventDefault(); const input = editing.textarea, start = input.selectionStart, end = input.selectionEnd;
        input.setRangeText('\t', start, end, 'end'); input.oninput();
      }
    };
    persistDraft(); editing.textarea.focus(); updateControls();
  }
  function saveEdit() {
    if (!editing || !editing.acknowledged || finishingEdit || saving || view?.loading) return;
    saving = true; saveVersion = view.version; editing.save.disabled = true; editing.cancel.disabled = true; editing.textarea.readOnly = true;
    editing.warning.textContent = 'Saving…';
    const content = editing.textarea.value === editing.original ? editing.rawOriginal : editing.crlf ? editing.textarea.value.replace(/\r?\n/g, '\r\n') : editing.textarea.value;
    Promise.resolve(send('review-save', {token:editing.token, content})).then(result => {
      if (result === false) saveFailed('Save could not be sent. Your edits are still here.');
    }).catch(() => saveFailed('Save could not be sent. Your edits are still here.'));
  }
  function saveFailed(message) {
    if (!editing) return;
    saving = false; editing.save.disabled = editing.cancel.disabled = editing.textarea.readOnly = false;
    editing.warning.textContent = message; editing.warning.classList.add('br-error');
  }
  function finishEdit() {
    if (!editing) return;
    finishingEdit = true; saving = false; editing.save.disabled = editing.cancel.disabled = editing.textarea.readOnly = true;
    editing.guardVersion = view.version; send('review-edit', {editing:false});
  }
  function endEditor() {
    clearDraft(); editing = null; finishingEdit = false; renderKey = ''; applyFileFilters();
  }
  function cancelEdit() {
    if (!editing || !editing.acknowledged || finishingEdit || saving) return;
    if (editing.textarea.value === editing.original) { finishEdit(); return; }
    if (editing.discard) return;
    const discard = el('div', 'br-discard'); discard.setAttribute('role', 'alert');
    discard.append(el('span', '', 'Discard your unsaved edits?'), button('Keep editing', 'Keep editing', () => { discard.remove(); editing.discard = null; editing.textarea.focus(); }), button('Discard edits', 'Discard unsaved edits', finishEdit, 'br-discard-confirm'));
    editing.discard = discard; content.querySelector('.br-editor').append(discard);
  }
  function applyFileFilters() {
    if (editing || beginningEdit) return;
    const files = matchingFiles();
    if (!files.some(file => file.path === selectedPath)) { selectedPath = files[0]?.path || ''; activeChange = 0; revealPath = selectedPath; revealEdge = ''; }
    wanted.clear();
    renderFiles(); renderContent(); renderCommits(); updateControls();
    if (selectedPath && !cache.has(selectedPath)) wanted.add(selectedPath);
    updatePreloadStatus();
    schedulePrefetch();
  }
  filter.oninput = applyFileFilters;
  ignorePatterns.oninput = () => {
    if (editing || beginningEdit) return;
    ignoreMatchers = reviewIgnoreMatchers(ignorePatterns.value);
    if (ignoreStorageKey) saveIgnorePatterns(ignoreStorageKey, ignorePatterns.value);
    applyFileFilters();
  };
  showIgnored.onchange = applyFileFilters;
  filesList.onkeydown = event => {
    if (editing || beginningEdit || event.defaultPrevented || event.isComposing || event.shiftKey || event.metaKey || event.ctrlKey || event.altKey) return;
    const files = matchingFiles(), index = files.findIndex(file => file.path === selectedPath);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? files.length - 1 : event.key === 'ArrowDown' ? Math.min(files.length - 1, index + 1) : event.key === 'ArrowUp' ? Math.max(0, index - 1) : -1;
    if (next < 0 || !files[next]) return; event.preventDefault(); selectFile(files[next].path, true, '', !!event.repeat);
    [...filesList.querySelectorAll('.br-file')].find(node => node.dataset.path === selectedPath)?.scrollIntoView({block:'nearest'});
  };
  const handleKeydown = event => {
    if (destroyed || !visible || event.defaultPrevented || event.isComposing) return;
    if (event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'f') {
      event.preventDefault();
      if (!editing && !beginningEdit) { findBar.hidden = false; updateFindStatus(); findInput.focus(); findInput.select(); }
      return;
    }
    if (event.key === 'Escape' && !findBar.hidden && !editing && !beginningEdit) { event.preventDefault(); closeFind(); return; }
    if (event.key === 'Escape') { if (editing) { event.preventDefault(); cancelEdit(); } else settings.open = false; return; }
    if (!editing && !beginningEdit && listNavigation && !event.altKey && !event.metaKey && !event.ctrlKey && !event.shiftKey &&
      ['ArrowUp','ArrowDown','Home','End'].includes(event.key) && [document.body, document.documentElement, root].includes(event.target)) {
      filesList.focus({preventScroll:true}); filesList.onkeydown(event); return;
    }
    if (editing || beginningEdit || !event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
    if (['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target?.tagName) || event.target?.isContentEditable) return;
    const control = {ArrowUp:previousChange, ArrowDown:nextChange, ArrowLeft:previousFile, ArrowRight:nextFile}[event.key];
    if (control) { event.preventDefault(); if (!control.disabled) control.click(); }
  };
  // macOS WebKit can leave focus on BODY when a window opens or a clicked
  // control is replaced. Such key events never pass through the inner frame.
  document.addEventListener('keydown', handleKeydown);
  const rememberList = event => { listNavigation = filesList.contains(event.target); };
  document.addEventListener('pointerdown', rememberList);
  baseSelect.onchange = () => { if (!editing) send('review-options', {baseRef:baseSelect.value, ignoreWhitespace:whitespace.checked}); };
  whitespace.onchange = () => { if (!editing) send('review-options', {baseRef:view.review.baseRef, ignoreWhitespace:whitespace.checked}); };
  wrapInput.onchange = () => { wrap = wrapInput.checked; content.classList.toggle('br-wrap', wrap); };
  syntaxInput.onchange = () => {
    if (editing || beginningEdit) return;
    highlighting = syntaxInput.checked;
    const top = content.scrollTop, left = content.scrollLeft;
    const horizontal = new Map([...cards].map(([path, card]) => [path, [...card.querySelectorAll('.br-horizontal-scroll')].map(node => node.scrollLeft)]));
    renderContent(); content.scrollTop = top; content.scrollLeft = left;
    for (const [path, positions] of horizontal) [...(cards.get(path)?.querySelectorAll('.br-horizontal-scroll') || [])].forEach((node, index) => {
      node.scrollLeft = positions[index] || 0; node.onscroll();
    });
  };
  modeSelect.onchange = () => { if (editing) return; presentation = modeSelect.value; pages.clear(); renderContent(); updateControls(); selectChange(activeChange, true); };

  function render(next) {
    if (destroyed) return;
    if (prefetchBatch && (next.version !== prefetchBatch.version || next.loading)) prefetchBatch = null;
    view = next;
    const storageKey = ignorePatternsStorageKey(view.path || view.siteID);
    if (storageKey !== ignoreStorageKey && !editing && !beginningEdit) {
      ignoreStorageKey = storageKey;
      ignorePatterns.value = storedIgnorePatterns(storageKey) ?? defaultReviewIgnorePatterns;
      ignoreMatchers = reviewIgnoreMatchers(ignorePatterns.value);
    }
    const review = view.review;
    branchLabel.textContent = view.branch || view.name || 'Branch review'; branchLabel.title = `${view.path || ''}\n${view.branch || ''}`;
    updateNotice();
    if (!review) { updateControls(); renderFiles(); return; }
    const refsKey = JSON.stringify([review.baseRef, review.refs]);
    if (baseSelect.dataset.refs !== refsKey) {
      const refs = [...new Set([review.baseRef, ...(review.refs || [])].filter(Boolean))];
      baseSelect.replaceChildren(...refs.map(ref => { const option = el('option', '', ref); option.value = ref; return option; }));
      baseSelect.value = review.baseRef; baseSelect.dataset.refs = refsKey;
    }
    whitespace.checked = !!review.ignoreWhitespace;
    comparisonLabel.textContent = `Fork point ${review.base?.slice(0, 8) || 'unavailable'} → working tree${review.ignoreWhitespace ? ' · whitespace ignored' : ''}`;
    comparisonLabel.title = `${review.baseRef || 'Base branch'} · ${review.base || ''}\nIncludes committed, staged, unstaged and untracked changes.`;
    if (view.loading) { updateControls(); return; }
    const key = snapshotIdentity(view);
    const changedSnapshot = key !== snapshotKey;
    if (changedSnapshot) {
      // A refreshed file starts on page zero. Keep a selected change on a later
      // page reachable; otherwise preserve the reader's position in context.
      if ((pages.get(selectedPath) || 0) > 0) revealPath = selectedPath;
      cache.clear(); wanted.clear(); failed.clear(); pendingPath = ''; expanded.clear(); pages.clear(); snapshotKey = key; renderKey = ''; prefetchBatch = null;
    }
    const data = review.selected;
    if (view.error && pendingPath) { failed.add(pendingPath); pendingPath = ''; }
    if (data?.path) { remember(data.path, data); if (pendingPath === data.path) pendingPath = ''; }
    else if (view.error) pendingPath = '';
    const visibleFiles = matchingFiles();
    const currentExists = visibleFiles.some(file => file.path === selectedPath);
    if (view.reviewEditing && !editing && !beginningEdit && data?.path) selectedPath = data.path;
    if (!currentExists && !editing && !beginningEdit && !view.reviewEditing) { selectedPath = visibleFiles.find(file => file.path === data?.path)?.path || visibleFiles[0]?.path || ''; activeChange = 0; revealPath = selectedPath; revealEdge = ''; }
    if (data?.path === selectedPath) activeChange = Math.min(activeChange, Math.max(0, (data.hunks?.length || 0) - 1));
    if (data?.path === selectedPath && findInput.value) {
      const available = changeIndices(data);
      if (available.length && !available.includes(activeChange)) activeChange = available[0];
    }
    if (view.reviewEditing && !editing && !beginningEdit) {
      const draft = storedDraft();
      if (draft && draft.path === data?.path && typeof draft.value === 'string' && typeof draft.original === 'string' && typeof draft.token === 'string') {
        const original = typeof draft.rawOriginal === 'string' ? draft.rawOriginal : draft.crlf ? draft.original.replace(/\r?\n/g, '\r\n') : draft.original;
        openEditor({...data, token:draft.token, newText:original});
        editing.textarea.value = draft.value; editing.original = draft.original; editing.crlf = !!draft.crlf; persistDraft();
        updateNotice('Your unsaved edit was restored after the editor reloaded.');
      } else if (orphanVersion !== view.version) {
        orphanVersion = view.version; send('review-edit', {editing:false});
        updateNotice('The editor reloaded and its edit session was closed. Review the working file before editing again.');
      }
    }
    if (beginningEdit && !view.loading) {
      if (view.error) { beginningEdit = ''; pendingPath = ''; }
      else if (!view.files?.some(file => file.path === beginningEdit)) { beginningEdit = ''; pendingPath = ''; updateNotice('The selected file is no longer in this comparison.'); }
      else if (data?.path === beginningEdit && view.files?.[view.file]?.path === beginningEdit) openEditor(data);
      else if (view.version > beginningVersion) {
        beginningVersion = view.version; pendingPath = beginningEdit;
        send('review-file', {file:view.files.findIndex(file => file.path === beginningEdit)});
      }
    }
    if (editing) {
      if (finishingEdit) {
        if (!view.reviewEditing) { endEditor(); return; }
        if (editing.guardVersion !== view.version) { editing.guardVersion = view.version; send('review-edit', {editing:false}); }
      } else if (!editing.acknowledged) {
        if (view.reviewEditing) { editing.acknowledged = true; editing.save.disabled = editing.cancel.disabled = editing.textarea.readOnly = false; editing.warning.textContent = 'Save changes this working file. Staged contents stay unchanged.'; editing.textarea.focus(); }
        else if (editing.guardVersion !== view.version && data?.path === editing.path) {
          editing.guardVersion = view.version; editing.token = data.token; editing.rawOriginal = data.newText; editing.textarea.value = data.newText; editing.original = editing.textarea.value;
          editing.crlf = /\r\n/.test(data.newText) && !/(^|[^\r])\n/.test(data.newText);
          send('review-edit', {editing:true, file:view.file, token:data.token});
        }
      }
      if (saving && view.version > saveVersion) {
        if (view.error) saveFailed(view.error + ' Your edits are still here.');
        else { finishEdit(); updateNotice('Saved to the working file.'); }
      }
      updateControls(); return;
    }
    if (contentKey() !== renderKey) { renderContent(); renderFiles(); }
    else if (data?.path) {
      const top = content.scrollTop;
      renderCard(data.path); content.scrollTop = top;
    }
    renderCommits(); updateControls();
    if (data?.path === selectedPath && revealPath === selectedPath) {
      const edge = revealEdge;
      revealPath = ''; revealEdge = '';
      selectChange(edge === 'last' ? Math.max(0, (data.hunks?.length || 0) - 1) : activeChange, presentation === 'file' || !!edge);
    }
    if (selectedPath && !cache.has(selectedPath) && !failed.has(selectedPath) && !view.error) wanted.add(selectedPath);
    updatePreloadStatus(); schedulePrefetch();
    // A file read can finish after the reader has scrolled elsewhere while
    // loading. Reconcile against the final layout, never the returned path.
    scheduleVisibleFile();
  }
  function setVisible(value) {
    visible = !!value;
    updateFileLoading();
    if (!visible) { prefetchBatch = null; if (prefetchFrame) cancelAnimationFrame(prefetchFrame); prefetchFrame = 0; }
    else schedulePrefetch();
  }
  return {render, prefetch, setVisible, destroy() { destroyed = true; if (loadingTimer) clearTimeout(loadingTimer); document.removeEventListener('keydown', handleKeydown); document.removeEventListener('pointerdown', rememberList); content.removeEventListener('scroll', trackScroll); if (scrollFrame) cancelAnimationFrame(scrollFrame); if (prefetchFrame) cancelAnimationFrame(prefetchFrame); if (navigationFrame) cancelAnimationFrame(navigationFrame); observer?.disconnect(); frame.remove(); }};
}
