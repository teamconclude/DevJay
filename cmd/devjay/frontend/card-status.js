// Keep local work, incoming commits, and review state independently readable.
export function cardStatus(git) {
  const result = {local: [], remote: [], alert: null, quiet: '', pr: '', prTone: 'muted', prNote: '', freshness: '', reviews: []};
  const metric = (symbol, value, label, tone, title, view, gitAction) => ({symbol, value, label, tone, title, ...(view ? {view} : {}), ...(gitAction ? {gitAction} : {})});
  const comparison = git.comparison;
  const ahead = comparison ? comparison.ahead : git.ahead, behind = comparison ? comparison.behind : git.behind;
  const diverged = ahead > 0 && behind > 0;
  const work = !comparison && diverged ? git.branchWork : null;
  const outgoing = work ? work.local : ahead;
  const comparedWith = comparison ? `compared with cached ${comparison.tracking} history; no upstream configured` : 'ahead of upstream';
  const historyDetail = `${outgoing} local-only commit IDs${comparison ? ` ${comparedWith}` : ''}. ${work?.inherited ? `${work.inherited} additional commits already on ${work.base} are excluded. ` : ''}Local and remote history differ; remote-only commits may be older versions after a rebase. Review the histories manually with Claude or Astra before publishing.`;
  if (!git.rebased && outgoing > 0) result.local.push(metric('↑', outgoing, comparison || diverged ? 'local only' : 'not pushed', 'info', diverged ? historyDetail : `${ahead} local commits ${comparedWith}`, 'branch', 'push'));
  if (git.conflicts > 0) result.local.push(metric('!', git.conflicts, 'conflict' + (git.conflicts === 1 ? '' : 's'), 'error', `${git.conflicts} conflicted files; ${git.changes} changed files in total`, 'edits'));
  else if (git.changes > 0) result.local.push(metric('✎', git.changes, 'edit' + (git.changes === 1 ? '' : 's'), 'warning', `${git.changes} changed files, including staged, unstaged, and untracked files`, 'edits'));
  if (!git.rebased && behind > 0) result.remote.push(metric('↓', behind, diverged ? 'remote only' : 'incoming', 'warning', diverged ? `${behind} remote-only commit IDs. ${historyDetail}` : comparison ? `${behind} commits in cached ${comparison.tracking} history not present locally; no upstream configured` : `${behind} incoming commits from upstream`, 'branch', 'update'));
  if (!git.rebased && git.rewritten) result.remote.push(metric('↻', '', 'Rewritten', 'warning', 'Remote history was rewritten', 'branch'));
  if (git.base?.behind > 0 && git.base.tracking !== comparison?.tracking && !git.mergedPR?.sameHead) {
    result.base = `${git.base.branch} · ${git.base.behind} newer ${git.base.behind === 1 ? 'commit' : 'commits'}`;
    result.baseTitle = [`Commits on ${git.base.tracking} not included in this branch`, git.base.note].filter(Boolean).join('\n');
    if (git.base.note) result.base += ' · cached';
  }
  const ordinary = /^(Up to date|Matches last known remote|Diverged ·|\d+ local commits ahead|\d+ commits behind)/.test(git.summary || '');
  if (!git.conflicts && git.summary && !ordinary) result.alert = {text: git.summary, tone: git.tone || 'muted', symbol: git.tone === 'error' ? '!' : '·'};
  if (!result.base && git.summary === 'Up to date' && !git.localNote && !git.remoteNote && !result.local.length && !result.remote.length && !result.alert) result.quiet = 'In sync';
  if (git.remoteNote) {
    result.freshness = git.remoteNote.includes('not checked') ? 'Remote not checked' : git.remoteNote.includes('no longer exists') ? 'Remote branch removed' : git.remoteNote.includes('stale') ? 'Last checked earlier' : git.remoteChecked ? 'Remote check failed · showing last check' : 'Remote check failed';
  }
  if (result.alert?.text === 'Remote branch removed') result.freshness = '';
  if (diverged && !git.rebased && !git.conflicts && !result.alert) {
    result.alert = {text:'History differs', tone:'warning', symbol:'↻', view:'branch', title:historyDetail};
  }
  if (git.rebased && !git.conflicts) {
    result.alert = {text: git.localNote ? 'Rebase status cached' : git.remoteNote ? 'Rebased · remote not verified' : 'Rebased · ready to publish', tone: 'info', symbol: '↻', view: 'branch', publishRebased: true,
      title: `${git.ahead} local and ${git.behind} remote commit IDs differ. Remote patches appear locally on a newer base. Right-click to check and push the rebased branch; conflicts or unverified history require manual work with Claude or Astra.`};
  }
  if (git.localNote) result.alert = {text: git.localNote, tone: 'warning', symbol: '◷'};
  result.pr = git.github || '';
  if (result.pr === 'No open pull request') result.pr = '';
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
    const freshnessUnknown = review.freshnessUnavailable;
    if (review.blockers > 0) values.push(metric('⊗', review.blockers, review.blockers === 1 ? 'blocker' : 'blockers', 'error', title));
    if (review.warnings > 0) values.push(metric('⚠︎', review.warnings, review.warnings === 1 ? 'warning' : 'warnings', 'warning', title));
    const hasFindings = values.length > 0;
    if (!hasFindings) {
      if (approved) values.push(metric('✓', '', 'Approved', 'good', title));
      else values.push(metric(review.earlier || freshnessUnknown ? '' : '✓', '', review.earlier || freshnessUnknown ? 'No findings reported' : 'No findings', review.earlier || freshnessUnknown ? 'muted' : 'good', title));
    }
    result.reviews.push({label: prefix, values, title, note: review.earlier ? (review.changeNote || 'Changes since review') : freshnessUnknown ? 'Review freshness unavailable' : ''});
    // Keep approval above any reported findings; a clean report cannot hide it.
    if (!approved || !hasFindings) labels.splice(index, 1);
  }
  result.pr = labels.join('; ');
  result.prTitle = [git.github, ...(git.reviews || []).map(review => review.detail)].filter(Boolean).join('\n');
  if (git.mergedPR?.sameHead) {
    const merged = git.mergedPR;
    result.pr = `PR #${merged.number} · Merged`;
    result.prTone = 'merged';
    result.prNote = 'Branch left over after merge';
    result.prTitle = [result.pr, result.prNote, `Merged ${new Date(merged.mergedAt).toLocaleString()}`, merged.url].filter(Boolean).join('\n');
    result.reviews = [];
    result.quiet = '';
  }
  return result;
}
