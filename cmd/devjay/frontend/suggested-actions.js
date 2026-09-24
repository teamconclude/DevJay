// Delivery failures and missing acknowledgements must leave a retry available.
async function recoverRequest(send, restore) {
  try { if (await send() !== false) return; } catch { /* The bridge displays the error. */ }
  restore();
}

function localWarning(git) {
  if (git.localNote) return 'Local status is unavailable. Check the checkout before continuing.';
  if (git.conflicts) return 'Resolve the local conflicts before continuing.';
  if (git.changes) return `${git.changes} changed ${git.changes === 1 ? 'file' : 'files'}. Commit or stash local work before continuing.`;
  return '';
}

export function postMergeSuggestion(site) {
  if (!site?.git.mergedPR) return null;
  const git = site.git;
  // A reused branch name is history, not a completed development context.
  if (!git.mergedPR.sameHead || !git.defaultBranch || git.mergedPR.base !== git.defaultBranch || git.isMainBranch || localWarning(git)) return null;
  return {kind: 'merged', branch: git.branch, target: git.defaultBranch || '', number: git.mergedPR.number,
    title: `Return to the default branch: ${git.defaultBranch || 'unknown'}`,
    canDelete: true, blocked: false, warning: ''};
}

export function siteSuggestions(site) {
  if (!site?.git) return [];
  const merged = postMergeSuggestion(site);
  if (merged) return [merged];
  if (site.git.canReplaceRemote && site.git.tracking) {
    const warning = localWarning(site.git);
    return [{kind:'replace',branch:site.git.branch,target:site.git.tracking,
      title:`Replace ${site.git.branch} with ${site.git.tracking}`,
      status:'Remote history was rewritten; this local tip was previously published.',
      warning,blocked:!!warning}];
  }
  if (site.git.rebased) return [];
  const git = site.git, suggestions = [], warning = localWarning(git);
  if (git.ahead > 0 && git.behind === 0 && git.pushTarget && !git.rewritten) {
    suggestions.push({kind: 'push', branch: git.branch, target: git.pushTarget,
      title: `Push ${git.ahead} ${git.ahead === 1 ? 'commit' : 'commits'} to ${git.pushTarget}`,
      status: `${git.ahead} local ${git.ahead === 1 ? 'commit' : 'commits'} ready to publish`,
      warning, blocked: !!warning});
  }
  if (git.behind > 0 && git.tracking) {
    const diverged = git.ahead > 0 || git.rewritten;
    suggestions.push({kind: 'update', branch: git.branch, target: git.tracking,
      title: `Update ${git.branch} from ${git.tracking}`,
      status: `${git.behind} incoming ${git.behind === 1 ? 'commit' : 'commits'}`,
      warning: warning || (diverged ? 'The branch has diverged or remote history was rewritten. Update manually; a fast-forward cannot be safely suggested.' : git.remoteNote || ''),
      blocked: !!warning || diverged || !!git.remoteNote});
  }
  if (git.base?.behind > 0 && !git.isMainBranch) {
    const needsUpdate = git.behind > 0 && git.tracking !== git.base.tracking;
    suggestions.push({kind: 'rebase', branch: git.branch, target: git.base.branch, tracking: git.base.tracking,
      title: `Rebase onto the latest ${git.base.branch}`,
      status: `${git.base.branch} · ${git.base.behind} newer ${git.base.behind === 1 ? 'commit' : 'commits'}`,
      warning: warning || (needsUpdate ? 'Update this branch from its own remote first, then check the rebase.' : git.base.note || ''),
      blocked: !!warning || needsUpdate});
  }
  return suggestions;
}

export function suggestionSteps(suggestion, deleteBranch) {
  if (suggestion.kind === 'replace') return [
    `Fetch ${suggestion.target} and verify that the exact local tip was previously recorded on that upstream.`,
    'Stop if there are local edits, untracked files, unverified local commits, or changes since this confirmation.',
    `Preserve the old commits in a recovery reference, then replace ${suggestion.branch} with the confirmed remote history.`,
    'Keep the branch name and tracking settings. Nothing is pushed.',
  ];
  if (suggestion.kind === 'push') return [
    `Fetch ${suggestion.target} and verify it has no incoming commits.`,
    `Push the confirmed commits from ${suggestion.branch} to ${suggestion.target}. Stop if the remote has changed.`,
    'Keep local files and branch unchanged. Never force-push or publish other branches or tags.',
  ];
  if (suggestion.kind === 'update') return [
    'Check that local work is saved and fetch the branch from its remote.',
    `Fast-forward ${suggestion.branch} to ${suggestion.target}. Stop if the branches have diverged.`,
    'Keep all local work. Do not reset, stash, or push automatically.',
  ];
  if (suggestion.kind === 'rebase') return [
    `Fetch ${suggestion.tracking} and check the complete rebase in a temporary checkout.`,
    'Repeat the conflict check against the confirmed commits. Stop and recommend a manual rebase if conflicts are found.',
    `Rebase ${suggestion.branch} onto the checked default-branch commit, preserving merges. Stop if the checkout or remote changes.`,
    'Do not push automatically. Previously pushed commits may need a separately confirmed force-with-lease push.',
  ];
  return [
    'Check that local work is saved and refresh the remote status.',
    `Switch to ${suggestion.target} and update it only if it can be fast-forwarded.`,
    deleteBranch ? `Delete local branch ${suggestion.branch} after the switch succeeds.` : `Keep local branch ${suggestion.branch}.`,
  ];
}

export function actionLabels(plan) {
  if (plan.kind === 'replace') return {title:'Confirm branch replacement',button:'Replace branch'};
  if (plan.kind === 'push') return {title:'Confirm push', button:`Push ${plan.commits} ${plan.commits === 1 ? 'commit' : 'commits'}`};
  if (plan.kind === 'update') return {title:'Confirm branch update', button:'Update branch'};
  if (plan.kind === 'rebase') return {title:'Confirm rebase', button:'Rebase branch'};
  return {title:'Confirm return to default branch', button:`Return to ${plan.target}`};
}

// Best available action goes on the right; prerequisites outrank publishing.
export function recommendedActions(suggestions) {
  const priority = {merged:0, replace:1, update:2, rebase:3, push:4};
  return [...suggestions].sort((a,b) => Number(!!a.blocked)-Number(!!b.blocked) ||
    (priority[a.kind] ?? 99)-(priority[b.kind] ?? 99)).slice(0,3).reverse();
}

export function suggestionButtonLabel(suggestion, git) {
  if (suggestion.kind === 'replace') return 'Replace from remote';
  if (suggestion.kind === 'push') return `Push ${git.ahead} ${git.ahead === 1 ? 'commit' : 'commits'}`;
  if (suggestion.kind === 'update') return 'Update branch';
  if (suggestion.kind === 'rebase') return `Rebase onto ${suggestion.target}`;
  return `Return to ${suggestion.target}`;
}

// The originating viewer selects an action and starts preparation. This window
// only confirms the resulting plan; it never selects or runs an action on load.
export function mountSuggestedActions(root, close, actionEvent) {
  const el = (tag, cls, text) => {
    const node = document.createElement(tag); node.className = cls;
    if (text !== undefined) node.textContent = text;
    return node;
  };
  const chrome = el('header', 'dialog-chrome');
  const dismiss = el('button', 'dialog-close', '×');
  dismiss.type = 'button'; dismiss.title = 'Cancel';
  dismiss.setAttribute('aria-label', 'Cancel action'); dismiss.onclick = close;
  chrome.append(dismiss);
  const heading = el('h2', 'confirmation-title');
  const context = el('p', 'help copyable');
  const plan = el('section', 'suggestion-plan');
  const deletion = el('label', 'suggestion-delete');
  const checkbox = el('input', ''); checkbox.type = 'checkbox';
  const deleteText = el('span', ''); deletion.append(checkbox, deleteText);
  const preview = el('p', 'suggestion-preview');
  const footer = el('div', 'suggestion-footer');
  const cancel = el('button', '', 'Cancel'); cancel.type = 'button'; cancel.onclick = close;
  const next = el('button', 'suggestion-primary'); next.type = 'button';
  footer.append(cancel, next); root.append(chrome, heading, context, plan, deletion, preview, footer);
  let current = null, identity = '';
  next.onclick = () => {
    if (next.disabled || current?.status !== 'ready' || !current.plan) return;
    next.disabled = true;
    const pending = current;
    return recoverRequest(() => actionEvent('action-confirm', pending.plan.id), () => { if (current === pending) next.disabled = false; });
  };
  checkbox.onchange = () => {
    if (checkbox.disabled || current?.status !== 'ready' || current.plan?.kind !== 'merged') return;
    checkbox.disabled = next.disabled = true;
    const pending = current;
    return recoverRequest(() => actionEvent('action-prepare', {siteID:pending.siteID,kind:'merged',branch:pending.plan.branch,
      head:pending.plan.head,deleteBranch:checkbox.checked}), () => {
        if (current === pending) { checkbox.disabled = next.disabled = false; checkbox.checked = !!pending.plan.deleteBranch; }
      });
  };
  return state => {
    const action = state.action?.siteID === state.suggestedSite ? state.action : null;
    const site = state.sites.find(s => s.id === state.suggestedSite);
    const key = JSON.stringify([action, site?.path]);
    if (key === identity) return;
    identity = key; current = action;
    const p = action?.plan;
    const kind = p?.kind || action?.kind;
    const labels = actionLabels(p || {kind, target:site?.git.defaultBranch});
    heading.textContent = action ? labels.title : 'Confirm action';
    context.textContent = site ? `${site.name || site.path || ''} · ${p?.branch || site.git.branch}${p ? ` → ${p.target}` : ''}` : '';
    context.title = site?.path || '';
    next.hidden = action?.status !== 'ready'; next.disabled = false;
    next.textContent = labels.button;
    cancel.disabled = dismiss.disabled = action?.status === 'running';
    cancel.textContent = action?.status === 'complete' ? 'Done' : 'Cancel';
    deletion.hidden = action?.status !== 'ready' || kind !== 'merged';
    checkbox.disabled = deletion.hidden; checkbox.checked = !!p?.deleteBranch;
    deleteText.textContent = `Delete local branch ${p?.branch || ''} afterward`;
    preview.textContent = action?.status === 'running' ? 'Finishing the operation before this window can close.' :
      kind === 'push' ? 'Only the confirmed commits are published. Your local checkout stays unchanged.' :
      'Nothing is pushed automatically. Original commits are kept in a recovery reference.';
    if (action?.status === 'ready') {
      const list = el('ol', '');
      for (const step of suggestionSteps({...p,tracking:site?.git.base?.tracking || p.target}, p.deleteBranch)) list.append(el('li', '', step));
      const revisions = p.kind === 'push' ? `${p.targetHead.slice(0,8)} → ${p.head.slice(0,8)}` : `${p.head.slice(0,8)} → ${p.targetHead.slice(0,8)}`;
      plan.replaceChildren(el('p', 'help copyable', revisions), list);
      if (p.deleteBranch) plan.append(el('p', 'warning', `This also deletes local branch ${p.branch}. A recovery reference will retain its commits.`));
      if (p.kind === 'replace') plan.append(el('p', 'warning', 'The local branch will use the rewritten remote history. Its old commits will be kept in a recovery reference.'));
      if (p.kind === 'rebase') plan.append(el('p', 'warning', 'Rebasing rewrites commit IDs. Review the result before pushing.'));
    } else {
      const titles = {preparing:'Checking action…',running:'Applying action…',complete:'✓ Completed',failed:'Action stopped'};
      plan.replaceChildren(el('strong', action?.status === 'complete' ? 'good' : '', titles[action?.status] || 'Choose an action in the Site’s changes window.'),
        el('p', 'copyable', action?.message || ''));
      if (action?.files?.length) { const files=el('ul','copyable'); for(const file of action.files) files.append(el('li','',file)); plan.append(files); }
      if (action?.currentHead) plan.append(el('p','help copyable',`Current checkout: ${action.currentBranch} · ${action.currentHead.slice(0,8)}`));
      if (action?.recovery) plan.append(el('p','help copyable',`Recovery: ${action.recovery}`));
    }
  };
}

// Live actions follow the current checkout, independently of the pinned diff.
// Clicking starts safety checks only; the popup requires separate confirmation.
export function mountViewerSuggestions(root, prepareAction) {
  let identity = '';
  const el = (tag, cls, text) => {const n=document.createElement(tag);n.className=cls;if(text!==undefined)n.textContent=text;return n;};
  return (site, action) => {
    const suggestions = recommendedActions(siteSuggestions(site));
    const busy = action?.status === 'preparing' || action?.status === 'running';
    const key = JSON.stringify([site?.id,site?.git.branch,site?.git.head,suggestions,busy]);
    if(key===identity)return;
    identity=key;root.replaceChildren();root.hidden=!suggestions.length;
    if(!suggestions.length)return;
    const header=el('div','git-suggestion-header');
    header.append(el('strong','','Suggested actions'),el('span','muted',`Current checkout · ${site.git.branch}`));
    const buttons=el('div','git-suggestion-buttons');
    suggestions.forEach((suggestion,index)=>{
      const primary=index===suggestions.length-1;
      const button=el('button',primary?'suggestion-primary':'suggestion-secondary',suggestionButtonLabel(suggestion,site.git));
      button.type='button';button.disabled=busy || !!suggestion.blocked;
      button.title=[suggestion.title,suggestion.warning || 'Check and confirm before applying this action'].join('\n');
      button.onclick=()=>{
        if(button.disabled)return;
        button.disabled=true;
        return recoverRequest(() => prepareAction({siteID:site.id,kind:suggestion.kind,branch:site.git.branch,head:site.git.head,deleteBranch:false}), () => { button.disabled=false; });
      };
      buttons.append(button);
    });
    header.append(buttons);root.append(header);
    const warnings=[...new Set(suggestions.map(s=>s.warning).filter(Boolean))];
    for(const warning of warnings)root.append(el('p','git-suggestion-warning warning',warning));
  };
}
