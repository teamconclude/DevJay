// Keep local work, incoming commits, and review state independently readable.
export function cardStatus(git) {
  const result = {local: [], remote: [], alert: null, quiet: '', pr: '', prTone: 'muted', freshness: '', reviews: []};
  const metric = (symbol, value, label, tone, title) => ({symbol, value, label, tone, title});
  if (git.ahead > 0) result.local.push(metric('↑', git.ahead, 'commit' + (git.ahead === 1 ? '' : 's'), 'info', `${git.ahead} local commits ahead of upstream`));
  if (git.conflicts > 0) result.local.push(metric('!', git.conflicts, 'conflict' + (git.conflicts === 1 ? '' : 's'), 'error', `${git.conflicts} conflicted files; ${git.changes} changed files in total`));
  else if (git.changes > 0) result.local.push(metric('✎', git.changes, 'edit' + (git.changes === 1 ? '' : 's'), 'warning', `${git.changes} changed files, including staged, unstaged, and untracked files`));
  if (git.behind > 0) result.remote.push(metric('↓', git.behind, 'commit' + (git.behind === 1 ? '' : 's'), 'warning', `${git.behind} incoming commits from upstream`));
  if (git.rewritten) result.remote.push(metric('↻', '', 'Rewritten', 'warning', 'Remote history was rewritten'));
  const ordinary = /^(Up to date|Matches last known remote|Diverged ·|\d+ local commits ahead|\d+ commits behind)/.test(git.summary || '');
  if (!git.conflicts && git.summary && !ordinary) result.alert = {text: git.summary, tone: git.tone || 'muted', symbol: git.tone === 'error' ? '!' : '·'};
  if (git.summary === 'Up to date' && !git.localNote && !git.remoteNote && !result.local.length && !result.remote.length && !result.alert) result.quiet = 'In sync';
  if (git.remoteNote) {
    result.freshness = git.remoteNote.includes('not checked') ? 'Remote not checked' : git.remoteNote.includes('no longer exists') ? 'Remote branch removed' : git.remoteNote.includes('stale') ? 'Remote stale · cached' : 'Remote unavailable · cached';
  }
  if (result.alert?.text === 'Remote branch removed') result.freshness = '';
  if (git.localNote) result.alert = {text: git.localNote, tone: 'warning', symbol: '◷'};
  result.pr = git.github || '';
  if (result.pr === 'No open pull request') result.pr = git.isMainBranch ? '' : 'No open PR';
  if (result.pr.includes('Changes requested')) result.prTone = 'warning';
  else if (result.pr.includes('Approved')) result.prTone = 'good';
  else if (result.pr.startsWith('PR #')) result.prTone = 'info';
  const labels = result.pr.split('; ');
  for (const review of git.reviews || []) {
    const prefix = `PR #${review.number}`;
    const index = labels.findIndex(label => label.startsWith(prefix + ' ·'));
    if (index < 0) continue;
    const title = `${labels[index]}\n${review.detail}`;
    const approved = labels[index] === `${prefix} · Approved`;
    if (review.unavailable) {
      labels[index] = `${prefix} · ${approved ? 'Approved · ' : ''}Review unavailable`;
      result.prTone = 'warning';
      continue;
    }
    const values = [];
    if (review.blockers > 0) values.push(metric('', review.blockers, review.blockers === 1 ? 'blocker' : 'blockers', 'error', title));
    if (review.warnings > 0) values.push(metric('', review.warnings, review.warnings === 1 ? 'warning' : 'warnings', 'warning', title));
    const hasFindings = values.length > 0;
    if (!hasFindings) {
      if (approved) values.push(metric('✓', '', 'Approved', 'good', title));
      else values.push(metric(review.earlier ? '' : '✓', '', review.earlier ? 'No findings reported' : 'No findings', review.earlier ? 'muted' : 'good', title));
    }
    result.reviews.push({label: prefix, values, title, note: review.earlier ? 'Changes pushed since review' : ''});
    // Keep approval above any reported findings; a clean report cannot hide it.
    if (!approved || !hasFindings) labels.splice(index, 1);
  }
  result.pr = labels.join('; ');
  result.prTitle = [git.github, ...(git.reviews || []).map(review => review.detail)].filter(Boolean).join('\n');
  return result;
}
