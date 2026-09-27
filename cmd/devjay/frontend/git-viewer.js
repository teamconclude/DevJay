import { mountFileEditor } from './file-editor.js';
// Diff contents are always text nodes, never HTML. Line numbers remain aligned
// with unified hunks; combined conflict hunks are displayed verbatim.
export function diffRows(text) {
  let old = null, current = null;
  return text.split('\n').map(line => {
    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
    if (hunk) { old=Number(hunk[1]); current=Number(hunk[2]); return {kind:'hunk',line,old:'',current:''}; }
    if (line.startsWith('@@@') || line.startsWith('diff ') || (old === null && /^(index |--- |\+\+\+ |new file|deleted file|old mode|new mode|similarity|rename |Binary |Submodule )/.test(line))) {
      old=current=null; return {kind:'meta',line,old:'',current:''};
    }
    if (old !== null && line.startsWith('+')) return {kind:'add',line,old:'',current:current++};
    if (old !== null && line.startsWith('-')) return {kind:'delete',line,old:old++,current:''};
    if (old !== null && line.startsWith(' ')) return {kind:'context',line,old:old++,current:current++};
    return {kind:'meta',line,old:'',current:''};
  });
}

// A path can appear in both staged and unstaged sections. Count it once in
// the overall file total, while summing the changes shown in both sections.
export function summarizeFiles(files, limited=false) {
  const summary={count:new Set(files.map(file=>file.path)).size,added:0,removed:0,binary:0,partial:limited};
  for(const file of files){
    if(!file.statsKnown){summary.partial=true;continue;}
    if(file.binary){summary.binary++;continue;}
    summary.added+=file.added||0;summary.removed+=file.removed||0;
  }
  return summary;
}

export function remoteHistoryContext(v) {
  const incoming = v.incomingCount < 0 ? 'local comparison unavailable' : `${v.incomingCount} incoming ${v.incomingCount === 1 ? 'commit' : 'commits'}`;
  return `Remote history · ${v.tracking || 'upstream'} · ${incoming}${v.comparisonOnly ? ' · No upstream configured; cached comparison' : ''}`;
}
export function localHistoryContext(v) {
  return `Local-only commits compared with ${v.tracking || 'the upstream'}${v.comparisonOnly ? ' · No upstream configured; cached comparison' : ''}`;
}
export function commitPresenceLabel(presence) {
  return {incoming:'↓ Incoming',local:'✓ Already local',unknown:'Local status unknown'}[presence] || '';
}

export function sameDiffSelection(previous, next) {
  const before = previous?.files?.[previous.file], after = next.files?.[next.file];
  return !!before && !!after && previous.siteID === next.siteID && previous.kind === next.kind &&
    previous.commit === next.commit && before.path === after.path && before.group === after.group && before.oldPath === after.oldPath;
}

export function viewCategory(v, kind) {
  const count = v.counts?.[kind];
  const known = Number.isInteger(count) && count >= 0;
  const label = {edits:'Local edits',local:'Local commits',remote:'Remote commits'}[kind];
  const unit = kind === 'edits' ? `changed ${count === 1 ? 'file' : 'files'}` : kind === 'local' ? `local-only ${count === 1 ? 'commit' : 'commits'}` : `${count === 1 ? 'commit' : 'commits'} in remote history`;
  return {
    label, count:known ? count.toLocaleString() : v.loading ? '…' : '—', empty:known && count === 0,
    description:known ? `${count.toLocaleString()} ${unit}${kind === 'local' && v.tracking ? ` compared with ${v.tracking}` : ''} at the last refresh${kind === 'remote' ? ', including commits already present locally' : ''}.` :
      v.loading ? 'Loading count…' : kind !== 'edits' && !v.upstream ? 'No upstream history available for comparison.' : 'Count unavailable. Refresh to retry.',
  };
}

export function mountGitViewer(root, request, suggestions) {
  const el=(tag,cls,text)=>{const node=document.createElement(tag);node.className=cls;if(text!==undefined)node.textContent=text;return node;};
  const button=(text,run,cls='')=>{const node=el('button',cls,text);node.type='button';node.onclick=run;return node;};
  function summaryHeading(node, files, label, limited=false){
    const counts=summarizeFiles(files,limited);
    const title=`${counts.count}${limited?'+':''} ${label|| (counts.count===1?'FILE':'FILES')}`;
    const totals=el('span','git-line-totals');
    totals.append(el('span','git-added',`+${counts.added.toLocaleString()}`),el('span','git-removed',`−${counts.removed.toLocaleString()}`));
    if(counts.partial)totals.append(el('span','muted','partial'));
    node.replaceChildren(el('span','',title),totals);
    node.classList.add('git-summary');
    node.title=`${counts.added} lines added, ${counts.removed} lines removed${counts.binary?`; ${counts.binary} binary file changes excluded from line counts`:''}${counts.partial?'; totals are incomplete because some files could not be counted or the list is limited':''}. Staged and unstaged changes are counted separately.`;
  }
  let state=null, renderedVersion=null, renderedState=null;
  const send=(operation,extra={})=>{if(state)request({version:state.version,operation,...extra});};
  const header=el('header','git-view-header');
  const title=el('div','git-view-title');
  const name=el('strong',''),branch=el('span','muted');title.append(name,branch);
  const tabs=el('nav','git-view-tabs');tabs.setAttribute('aria-label','Changes to inspect');
  for(const [kind,label] of [['edits','Local edits'],['local','Local commits'],['remote','Remote commits']]) {
    const tab=button('',()=>send('mode',{kind}));tab.dataset.kind=kind;
    tab.append(el('span','',label),el('span','git-tab-count','…'));tabs.append(tab);
  }
  const refresh=button('Refresh',()=>send('refresh'));
  header.append(title,tabs,refresh);
  const context=el('div','git-view-context');
  const error=el('div','git-view-error');error.setAttribute('role','status');error.hidden=true;
  const panes=el('div','git-view-panes');
  const commits=el('aside','git-view-commits');commits.setAttribute('aria-label','Commits');
  const commitList=el('div','git-scroll-list');
  const pagination=el('div','git-view-pagination');
  const previous=button('Previous',()=>send('page',{page:state.page-1}));
  const pageLabel=el('span','muted');const next=button('Next',()=>send('page',{page:state.page+1}));
  pagination.append(previous,pageLabel,next);commits.append(el('h2','','Commits'),commitList,pagination);
  const files=el('aside','git-view-files');files.setAttribute('aria-label','Changed files');
  const fileHeading=el('h2','','Files'),fileList=el('div','git-scroll-list');files.append(fileHeading,fileList);
  const content=el('section','git-view-content');
  const commitDetail=el('div','git-commit-detail');
  const diffHeader=el('div','git-diff-header');
  const diffTitle=el('span','git-diff-title'),editorControls=el('div','file-editor-controls');
  diffHeader.append(diffTitle,editorControls);
  const renderEditor=mountFileEditor(editorControls,request);
  const note=el('p','git-diff-note');
  const diff=el('div','git-diff');diff.tabIndex=0;diff.setAttribute('aria-label','File diff');
  content.append(commitDetail,diffHeader,note,diff);panes.append(commits,files,content);root.append(header,context,...(suggestions?[suggestions]:[]),error,panes);
  return v=>{
    const changedContext=state?.siteID!==v.siteID||state?.kind!==v.kind;
    state=v;
    renderEditor(v);
    commits.hidden=v.kind==='edits';panes.classList.toggle('edits-only',v.kind==='edits');
    if(changedContext&&v.loading){
      commitList.replaceChildren();fileList.replaceChildren();commitDetail.replaceChildren();diff.replaceChildren();
      renderedState=null;renderedVersion=null;
      diffTitle.textContent='';note.hidden=true;fileHeading.textContent='Files';
      previous.disabled=true;next.disabled=true;pageLabel.textContent='';
    }
    name.textContent=v.name||'Changes';name.title=v.path;
    branch.textContent=v.branch||'';
    tabs.querySelectorAll('button').forEach(tab=>{
      const category=viewCategory(v,tab.dataset.kind);
      tab.classList.toggle('selected',tab.dataset.kind===v.kind);
      tab.classList.toggle('empty-category',category.empty);
      tab.setAttribute('aria-pressed',String(tab.dataset.kind===v.kind));
      tab.setAttribute('aria-label',`${category.label}, ${category.count}`);
      tab.querySelector('.git-tab-count').textContent=category.count;
      tab.title=category.description;
    });
    root.setAttribute('aria-busy',String(v.loading));panes.inert=v.loading;refresh.disabled=v.loading;
    error.hidden=!v.error;error.textContent=v.error||'';
    const capture=v.captured?new Date(v.captured).toLocaleTimeString():'';
    context.textContent=v.loading?'Loading…':v.kind==='edits'?
      `Working changes · file list captured ${capture}. Select a file to read its current diff.`:
      `${v.kind==='remote'?remoteHistoryContext(v):localHistoryContext(v)} · snapshot ${capture}${v.remoteNote?' · '+v.remoteNote:''}${v.historyNote?' · '+v.historyNote:''}${v.rebased?' · Rebased history: equivalent changes can have different commit IDs.':''}`;
    // Keep scroll and text selection intact for duplicate/loading-only emissions.
    if(v.loading)return;
    if(renderedVersion===v.version)return;
    renderedVersion=v.version;
    const previousState=renderedState;
    const sameList=previousState?.siteID===v.siteID&&previousState?.kind===v.kind&&previousState?.page===v.page;
    const commitScroll=sameList?commitList.scrollTop:0;
    const fileScroll=sameList&&renderedState?.commit===v.commit?fileList.scrollTop:0;
    const sameDiff=sameDiffSelection(previousState,v);
    const diffScroll=sameDiff?{top:diff.scrollTop,left:diff.scrollLeft}:{top:0,left:0};
    const focusFile=fileList.contains(document.activeElement),focusCommit=commitList.contains(document.activeElement);
    renderedState=v;
    commitList.replaceChildren();
    for(const commit of v.commits||[]) {
      const item=button('',()=>send('commit',{commit:commit.id}),'git-commit');
      item.classList.toggle('selected',commit.id===v.commit);item.setAttribute('aria-pressed',String(commit.id===v.commit));
      item.append(el('strong','',commit.subject||'(No subject)'),el('span','muted',`${commit.id.slice(0,8)} · ${commit.author}`),el('span','muted',new Date(commit.date).toLocaleString()));
      if(v.kind==='remote'){
        const presence=el('span',`git-commit-presence ${commit.presence}`,commitPresenceLabel(commit.presence));
        presence.title=commit.presence==='incoming'?'This commit ID is not in the local branch’s captured history.':commit.presence==='local'?'This commit is already in the local branch’s captured history.':'The local comparison could not be read.';
        item.append(presence);
      }
      item.title=commit.id;commitList.append(item);
      if(focusCommit&&commit.id===v.commit)item.focus({preventScroll:true});
    }
    if(!(v.commits||[]).length)commitList.append(el('p','git-empty',v.kind==='remote'?'No remote history available.':`No local-only commits compared with ${v.tracking||'the remote history'}.`));
    previous.disabled=v.page===0;next.disabled=!v.more;pageLabel.textContent=`Page ${v.page+1}`;
    summaryHeading(fileHeading,v.files||[],null,v.filesLimited);fileList.replaceChildren();
    let lastGroup='';
    for(const [index,file] of (v.files||[]).entries()) {
      if(file.group!==lastGroup&&v.kind==='edits'){
        const groupHeading=el('h3','git-file-group');
        summaryHeading(groupHeading,(v.files||[]).filter(item=>item.group===file.group),file.group.toUpperCase(),v.filesLimited);
        fileList.append(groupHeading);lastGroup=file.group;
      }
      const item=button('',()=>send('file',{file:index}),'git-file');
      item.classList.toggle('selected',index===v.file);item.setAttribute('aria-pressed',String(index===v.file));
      const status=file.status.startsWith('R')?'R':file.status[0];
      item.append(el('span',`git-file-status status-${status}`,status),el('span','git-file-path',file.path));
      item.title=file.oldPath?`${file.oldPath} → ${file.path}`:file.path;
      item.setAttribute('aria-label',`${file.group}: ${item.title} (${file.status})`);fileList.append(item);
      if(focusFile&&index===v.file)item.focus({preventScroll:true});
    }
    if(!(v.files||[]).length)fileList.append(el('p','git-empty',v.kind==='edits'?'No local edits.':v.commit?'This commit has no changed files.':'Select a commit.'));
    if(v.filesLimited)fileList.append(el('p','git-empty','Showing the first 2,000 files.'));
    commitList.scrollTop=commitScroll;fileList.scrollTop=fileScroll;
    commitDetail.hidden=v.kind==='edits'||!v.commit;
    const sameCommitDetail=previousState?.siteID===v.siteID&&previousState?.kind===v.kind&&previousState?.commit===v.commit&&
      previousState?.message===v.message&&previousState?.parent===v.parent&&previousState?.merge===v.merge;
    if(!sameCommitDetail){
      commitDetail.replaceChildren();
      if(!commitDetail.hidden){
        const message=el('details','');const subject=el('summary','',(v.message||'').split('\n')[0]);
        message.append(subject,el('pre','',v.message||''));commitDetail.append(message);
        if(v.merge)commitDetail.append(el('p','muted',`Merge commit · compared with first parent ${v.parent.slice(0,8)}`));
        else if(!v.parent)commitDetail.append(el('p','muted','Initial commit · compared with an empty tree'));
      }
    }
    const file=v.files?.[v.file];
    diffTitle.textContent=file?(file.oldPath?`${file.oldPath} → ${file.path}`:file.path):'Select a file to view its diff';
    diffTitle.title=diffTitle.textContent;
    note.hidden=!v.diffNote;note.textContent=v.diffNote||'';
    if(!sameDiff||previousState?.diff!==v.diff){
      const fragment=document.createDocumentFragment();
      for(const row of diffRows(v.diff||'')){
        const line=el('div',`git-diff-line ${row.kind}`);
        const left=el('span','git-line-number',row.old),right=el('span','git-line-number',row.current);
        left.setAttribute('aria-hidden','true');right.setAttribute('aria-hidden','true');
        line.append(left,right,el('span','git-code',row.line));fragment.append(line);
      }
      diff.replaceChildren(fragment);
    }
    diff.scrollTop=diffScroll.top;diff.scrollLeft=diffScroll.left;
  };
}
