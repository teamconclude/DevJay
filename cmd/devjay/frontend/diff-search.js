import { appendSyntax } from './syntax-highlight.js';

const searchHighlightLimit = 256;
const literalSearchPattern = query => new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'giu');

// Match source characters literally. RegExp keeps offsets in the original
// UTF-16 string even when Unicode case folding would change string length.
export function findTextMatches(text, query) {
  if (!query) return [];
  const pattern = literalSearchPattern(query);
  return [...text.matchAll(pattern)].map(match => ({start:match.index, end:match.index + match[0].length}));
}

export function filterSearchRows(rows, query, textsForRow, context = 3) {
  if (!query) return {rows, matchCount:0, matchingRows:0};
  const visible = new Set();
  let matchCount = 0, matchingRows = 0;
  rows.forEach((row, index) => {
    const count = textsForRow(row).reduce((sum, text) => sum + findTextMatches(text || '', query).length, 0);
    if (!count) return;
    matchCount += count; matchingRows++;
    for (let line = Math.max(0, index - context); line <= Math.min(rows.length - 1, index + context); line++) visible.add(line);
  });
  const filtered = [];
  if (matchingRows) for (let index = 0; index < rows.length;) {
    if (visible.has(index)) { filtered.push(rows[index++]); continue; }
    const from = index;
    while (index < rows.length && !visible.has(index)) index++;
    filtered.push({kind:'search-gap', count:index - from, from, to:index});
  }
  return {rows:filtered, matchCount, matchingRows};
}

// Range slicing preserves the caller's inline-diff wrappers. Syntax spans are
// rendered inside search marks rather than editing or reparsing source HTML.
export function appendSearchSyntax(parent, text, tokens, query, from = 0, to = text.length, budget = null) {
  from = Math.max(0, Math.min(text.length, from));
  to = Math.max(from, Math.min(text.length, to));
  const document = parent.ownerDocument || globalThis.document;
  let cursor = from, highlighted = 0;
  // Bound DOM expansion for long repeated lines, including callers rendering
  // separate inline-diff slices. A shared budget also caps an entire rendered
  // diff across its lines and sides. Filtering and counts examine all hits.
  for (const match of query ? text.matchAll(literalSearchPattern(query)) : []) {
    if (highlighted++ >= searchHighlightLimit || (budget && budget.remaining <= 0)) break;
    const start = Math.max(from, match.index), end = Math.min(to, match.index + match[0].length);
    if (end <= start) continue;
    appendSyntax(parent, text, tokens, cursor, start);
    const mark = document.createElement('mark'); mark.className = 'diff-search-hit';
    appendSyntax(mark, text, tokens, start, end); parent.append(mark); cursor = end;
    if (budget) budget.remaining--;
  }
  appendSyntax(parent, text, tokens, cursor, to);
  return parent;
}
