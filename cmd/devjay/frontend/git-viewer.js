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

// Pair each contiguous deletion/addition block without inventing matching
// lines. Missing counterparts remain blank, and context keeps both numbers.
export function splitDiff(text) {
  const unified=diffRows(text), rows=[];
  let changes=0, block=[], last=null;
  const flush=()=>{
    if(!block.length)return;
    const change=changes++, before=block.filter(row=>row.kind==='delete'), after=block.filter(row=>row.kind==='add');
    for(const row of block)row.change=change;
    for(let i=0;i<Math.max(before.length,after.length);i++)rows.push({kind:'change',change,before:before[i],after:after[i]});
    block=[];
  };
  for(const row of unified){
    if(row.kind==='add'||row.kind==='delete'){block.push(row);last=row;continue;}
    if(row.line==='\\ No newline at end of file'&&last){last.noNewline=true;continue;}
    flush();last=null;
    rows.push(row.kind==='context'?{kind:'context',before:row,after:row}:row);
  }
  flush();
  return {unified,rows,changes,supported:!text.split('\n').some(line=>line.startsWith('@@@'))};
}

export function changedText(before, after) {
  const left=Array.from(before),right=Array.from(after);
  let start=0,end=0;
  while(start<left.length&&start<right.length&&left[start]===right[start])start++;
  while(end<left.length-start&&end<right.length-start&&left[left.length-1-end]===right[right.length-1-end])end++;
  const parts=chars=>[chars.slice(0,start).join(''),chars.slice(start,chars.length-end).join(''),end?chars.slice(-end).join(''):''];
  return [parts(left),parts(right)];
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
export function commitPresenceLabel(presence, v = {}) {
  if (presence === 'inherited') return `✓ On ${v.branchWork?.base || 'default branch'}`;
  if (v.counts?.local > 0 && v.incomingCount > 0) {
    if (presence === 'outgoing') return '↑ Local only';
    if (presence === 'incoming') return '↓ Remote only';
  }
  return {incoming:'↓ Incoming',outgoing:'↑ Not pushed',shared:'✓ On remote',unlinked:'Local',local:'✓ Already local',unknown:'Local status unknown'}[presence] || '';
}

export function branchHistoryContext(v) {
  if(v.branchOnly){
    const count=Number.isInteger(v.branchCommits)?`${v.branchCommits} ${v.branchCommits===1?'commit':'commits'} added`:'Commits added';
    return `${count} since the common ancestor with ${v.branchDiff?.target||'the base branch'}. Includes published commits; excludes shared ancestors and remote-only commits.`;
  }
  if(!v.upstream)return 'Branch history · No remote comparison available';
  const diverged=v.counts?.local>0&&v.incomingCount>0;
  const outgoing=Number.isInteger(v.counts?.local)?`${v.branchWork?.local ?? v.counts.local} ${v.comparisonOnly||diverged?'local-only':'not pushed'}`:'Outgoing comparison unavailable';
  const incoming=v.incomingCount>=0?`${v.incomingCount} ${diverged?'remote-only':'incoming'}`:'Incoming comparison unavailable';
  const inherited=v.branchWork?.inherited?` · ${v.branchWork.inherited} additional commits already on ${v.branchWork.base}`:'';
  return `${v.branch || 'Branch'} · ${outgoing} · ${incoming} · ${v.tracking}${inherited}${v.comparisonOnly?' · No upstream configured; cached comparison':''}`;
}

export function sameDiffSelection(previous, next) {
  const before = previous?.files?.[previous.file], after = next.files?.[next.file];
  return !!before && !!after && previous.siteID === next.siteID && previous.kind === next.kind &&
    !!previous.allChanges === !!next.allChanges &&
    previous.commit === next.commit && before.path === after.path && before.group === after.group && before.oldPath === after.oldPath;
}

export function viewCategory(v, kind) {
  const branchOnly=kind==='branch'&&v.branchOnly;
  const count = branchOnly?v.branchCommits:v.counts?.[kind];
  const known = Number.isInteger(count) && count >= 0;
  const label = {edits:'Changes',branch:'History',local:'Local commits',remote:'Remote commits'}[kind];
  const unit = kind === 'edits' ? `changed ${count === 1 ? 'file' : 'files'}` : kind === 'local' ? `local-only ${count === 1 ? 'commit' : 'commits'}` : branchOnly?`${count===1?'commit':'commits'} added since the branch base`:`${count === 1 ? 'commit' : 'commits'} in ${kind==='branch'?'full':'remote'} history`;
  return {
    label, count:known ? count.toLocaleString() : v.loading ? '…' : '—', empty:known && count === 0,
    description:known ? `${count.toLocaleString()} ${unit}${kind === 'local' && v.tracking ? ` compared with ${v.tracking}` : ''} at the last refresh${kind === 'remote' ? ', including commits already present locally' : ''}.` :
      v.loading ? 'Loading count…' : branchOnly ? 'Branch commit count unavailable. Refresh or open Full history.' : kind !== 'edits' && !v.upstream ? 'No upstream history available for comparison.' : 'Count unavailable. Refresh to retry.',
  };
}

// Keep one row per path, while retaining the backend's separate diff selections.
export function changeFileRows(files, selected) {
  const paths=new Map();
  files.forEach((file,index)=>{
    if(!paths.has(file.path))paths.set(file.path,{path:file.path,parts:[]});
    paths.get(file.path).parts.push({file,index});
  });
  return [...paths.values()].sort((a,b)=>a.path.localeCompare(b.path)).map(row=>{
    const current=row.parts.find(part=>part.index===selected);
    const preferred=current||row.parts.find(part=>part.file.group==='Unstaged')||row.parts[0];
    return {...row,index:preferred.index,file:preferred.file,selected:!!current};
  });
}

function changePartLabel(group) {
  return {Staged:'Staged',Unstaged:'Working tree',Untracked:'Untracked'}[group]||group;
}

function historyDay(date) {
  const value=new Date(date);
  return Number.isNaN(value.getTime())?'Date unavailable':value.toLocaleDateString(undefined,{month:'short',day:'numeric',year:'numeric'});
}

export function mountGitViewer(root, request, suggestions, openBranches) {
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
  let state=null, renderedVersion=null, renderedState=null, initialFocus=true;
  let commitRowsKey=null, fileRowsKey=null;
  const send=(operation,extra={})=>{if(state&&!state.loading)request({version:state.version,operation,...extra});};
  // Keep focus on the list itself: replacing rows or loading another diff must
  // not lose the next arrow key. Only the selected option enters the details.
  function navigableList(label) {
    const list=el('div','git-scroll-list');list.tabIndex=0;
    list.setAttribute('role','listbox');list.setAttribute('aria-label',label);
    list.title='↑/↓ to select · Home/End for first/last';
    const selected=()=>list.querySelector('.selected');
    list.onfocus=()=>selected()?.scrollIntoView({block:'nearest',inline:'nearest'});
    list.onkeydown=event=>{
      if(event.altKey||event.ctrlKey||event.metaKey||event.shiftKey||event.isComposing||event.defaultPrevented)return;
      if(!['ArrowUp','ArrowDown','Home','End'].includes(event.key))return;
      event.preventDefault();
      if(!state||state.loading)return;
      const items=[...list.querySelectorAll('button')];
      if(!items.length)return;
      const current=items.indexOf(selected());
      const index=event.key==='Home'?0:event.key==='End'?items.length-1:
        current<0?0:Math.max(0,Math.min(items.length-1,current+(event.key==='ArrowDown'?1:-1)));
      if(index!==current)items[index].click();
    };
    return list;
  }
  function listOption(list,item,id,selected) {
    item.id=id;item.tabIndex=-1;item.setAttribute('role','option');
    item.classList.toggle('selected',selected);item.setAttribute('aria-selected',String(selected));
    if(selected)list.setAttribute('aria-activedescendant',id);
  }
  const header=el('header','git-view-header');
  const title=el('div','git-view-title');
  const name=el('strong',''),branch=el('span','git-current-branch');
  title.append(el('span','git-toolbar-label','Repository'),name);
  let openingBranches=false;
  const canSwitch=()=>!!openBranches && !!state?.head && !!state?.branch && !['Detached HEAD','(detached)'].includes(state.branch);
  const updateBranchButton=()=>{switchBranch.hidden=!canSwitch();switchBranch.disabled=openingBranches||!!state?.loading;};
  const switchBranch=button('Switch...',async()=>{
    if(switchBranch.disabled||state?.loading||!canSwitch())return;
    openingBranches=true;updateBranchButton();
    const rect=switchBranch.getBoundingClientRect();
    try { await openBranches(state.siteID,{x:rect.left,y:rect.top,width:rect.width,height:rect.height}); }
    finally { openingBranches=false;updateBranchButton(); }
  },'git-switch-branch');
  switchBranch.setAttribute('aria-haspopup','dialog');switchBranch.setAttribute('aria-label','Switch branch');switchBranch.title='Switch branch';switchBranch.hidden=true;
  const branchRow=el('div','git-view-branch');
  const branchName=el('div','git-branch-name');branchName.append(branch,switchBranch);
  branchRow.append(el('span','git-toolbar-label','Current branch'),branchName);
  const tabs=el('nav','git-view-tabs');tabs.setAttribute('aria-label','Changes to inspect');
  for(const [kind,label] of [['edits','Changes'],['branch','History']]) {
    const tab=button('',()=>{tab.focus({preventScroll:true});send('mode',{kind});});tab.dataset.kind=kind;
    tab.onkeydown=event=>{
      if(event.key==='ArrowDown'&&!event.altKey&&!event.ctrlKey&&!event.metaKey&&!event.shiftKey&&!state?.loading){
        event.preventDefault();(state?.kind==='edits'?fileList:commitList).focus();
      }
    };
    tab.append(el('span','',label),el('span','git-tab-count','…'));tabs.append(tab);
  }
  const refresh=button('Refresh',()=>send('refresh'));
  refresh.title='Reread files and the last fetched history. Push and Update check the remote before confirmation.';
  header.append(title,branchRow,...(suggestions?[suggestions]:[]),refresh);
  const context=el('div','git-view-context');
  const error=el('div','git-view-error');error.setAttribute('role','status');error.hidden=true;
  const panes=el('div','git-view-panes');
  const sidebar=el('aside','git-view-sidebar');sidebar.setAttribute('aria-label','Changes and history');
  const historyScope=el('nav','git-history-scope');historyScope.setAttribute('aria-label','History scope');
  const branchHistoryButton=button('Branch history',()=>send('history-scope',{scope:'branch'}),'git-branch-history');
  branchHistoryButton.title='Commits added since the branch base, including published commits';
  const fullHistoryButton=button('Full history',()=>send('history-scope',{scope:'full'}),'git-full-history');
  fullHistoryButton.title='All commits in the branch and its last fetched upstream';
  historyScope.append(branchHistoryButton,fullHistoryButton);
  sidebar.append(tabs,historyScope,context);
  const inspection=el('section','git-view-inspection');
  const detailPanes=el('div','git-detail-panes');
  const statusbar=el('footer','git-view-status');
  const commits=el('aside','git-view-commits');commits.setAttribute('aria-label','Commits');
  const branchDiffButton=button('All branch changes',()=>{setLayout('split');send('branch-diff');},'git-branch-diff');
  const commitList=navigableList('Branch commits');
  const pagination=el('div','git-view-pagination');
  const previous=button('Previous',()=>send('page',{page:state.page-1}));
  const pageLabel=el('span','muted');const next=button('Next',()=>send('page',{page:state.page+1}));
  pagination.append(previous,pageLabel,next);commits.append(branchDiffButton,commitList,pagination);
  const files=el('aside','git-view-files');files.setAttribute('aria-label','Changed files');
  const fileHeading=el('h2','','Files'),fileList=navigableList('Changed files');files.append(fileHeading,fileList);
  const content=el('section','git-view-content');
  const commitDetail=el('div','git-commit-detail');
  const diffHeader=el('div','git-diff-header');
  const diffTitle=el('span','git-diff-title'),editorControls=el('div','file-editor-controls');
  const partTabs=el('nav','git-change-parts');partTabs.setAttribute('aria-label','Changes in selected file');
  diffHeader.append(diffTitle,editorControls);
  const renderEditor=mountFileEditor(editorControls,request);
  const note=el('p','git-diff-note');
  const diff=el('div','git-diff');diff.tabIndex=0;diff.setAttribute('aria-label','File diff');
  let layout='unified',difference=-1,model=null,changeNodes=[],fileRows=[];
  const diffToolbar=el('div','git-diff-toolbar');diffToolbar.setAttribute('aria-label','Diff navigation');
  const differenceStatus=el('span','git-diff-position'),fileStatus=el('span','git-file-position');
  differenceStatus.setAttribute('aria-live','polite');
  function navButton(symbol,label,run){const node=button(symbol,run);node.title=label;node.setAttribute('aria-label',label);return node;}
  const previousDiff=navButton('↑','Previous difference (Option+Up)',()=>moveDifference(-1));
  const nextDiff=navButton('↓','Next difference (Option+Down)',()=>moveDifference(1));
  const previousFile=navButton('←','Previous file (Option+Left)',()=>moveFile(-1));
  const nextFile=navButton('→','Next file (Option+Right)',()=>moveFile(1));
  const differenceControls=el('div','git-diff-navigation');differenceControls.append(previousDiff,nextDiff,differenceStatus);
  const fileControls=el('div','git-diff-navigation');fileControls.append(previousFile,fileStatus,nextFile);
  const layoutControls=el('div','git-diff-layout');layoutControls.setAttribute('aria-label','Diff layout');
  const unifiedButton=button('Unified',()=>setLayout('unified'));
  const splitButton=button('Side by side',()=>setLayout('split'));
  layoutControls.append(unifiedButton,splitButton);diffToolbar.append(differenceControls,fileControls,layoutControls);
  const sides=el('div','git-diff-sides'),beforeLabel=el('span',''),afterLabel=el('span','');sides.append(beforeLabel,afterLabel);
  function placeFiles(){
    const inSidebar=state?.kind==='edits'||layout==='split';
    const parent=inSidebar?sidebar:detailPanes;
    if(files.parentNode!==parent){if(inSidebar)sidebar.append(files);else detailPanes.insertBefore(files,content);}
    panes.classList.toggle('split-view',layout==='split');
  }
  function updateDiffControls(){
    const loading=!state||state.loading, count=model?.changes||0;
    const index=fileRows.findIndex(row=>row.selected);
    previousDiff.disabled=loading||difference<=0;
    nextDiff.disabled=loading||!count||difference>=count-1;
    previousFile.disabled=loading||index<=0;
    nextFile.disabled=loading||index<0||index>=fileRows.length-1;
    differenceStatus.textContent=count?`${difference+1} / ${count} ${count===1?'difference':'differences'}`:'No text differences';
    fileStatus.textContent=`${index<0?0:index+1} / ${fileRows.length}${state?.filesLimited?'+':''} files`;
    unifiedButton.disabled=loading;splitButton.disabled=loading||model?.supported===false;
    splitButton.title=model?.supported===false?'Combined conflict diffs are shown in unified form.':'Show aligned before and after versions';
    for(const [node,value] of [[unifiedButton,'unified'],[splitButton,'split']]){
      const selected=(layout==='split'&&model?.supported!==false?'split':'unified')===value;
      node.classList.toggle('selected',selected);node.setAttribute('aria-pressed',String(selected));
    }
  }
  function moveDifference(direction){
    if(!state||state.loading||!model?.changes)return;
    const next=Math.max(0,Math.min(model.changes-1,difference+direction));
    difference=next;markDifference();updateDiffControls();
    changeNodes[difference]?.scrollIntoView({block:'center',inline:'nearest'});
  }
  function markDifference(){
    diff.querySelectorAll('.git-difference').forEach(node=>node.classList.toggle('current-difference',Number(node.dataset.change)===difference));
  }
  function moveFile(direction){
    const index=fileRows.findIndex(row=>row.selected),row=fileRows[index+direction];
    if(index>=0&&row)send('file',{file:row.index});
  }
  function setLayout(next){
    if(!state||state.loading||next===layout)return;
    layout=next;placeFiles();paintDiff(state);
    changeNodes[difference]?.scrollIntoView({block:'center',inline:'nearest'});
  }
  function paintDiff(v){
    const split=layout==='split'&&model?.supported!==false;
    diff.classList.toggle('side-by-side',split);sides.hidden=!split||!v.files?.[v.file];
    updateDiffLabels(v);
    const fragment=document.createDocumentFragment();changeNodes=[];
    const register=(line,row)=>{
      if(row.change===undefined)return;
      line.classList.add('git-difference');line.dataset.change=String(row.change);
      if(!changeNodes[row.change])changeNodes[row.change]=line;
    };
    for(const row of (split?model?.rows:model?.unified)||[]){
      // File paths and revision labels are already visible above the panes.
      // Keep meaningful metadata (renames, modes, binary notices) in the diff.
      if(split&&row.kind==='meta'&&/^(diff --git |index |--- |\+\+\+ )/.test(row.line))continue;
      if(split&&(row.kind==='change'||row.kind==='context')){
        const line=el('div','git-split-line');register(line,row);
        const parts=row.before&&row.after&&row.kind==='change'?changedText(row.before.line.slice(1),row.after.line.slice(1)):null;
        for(const [side,source,index] of [['before',row.before,0],['after',row.after,1]]){
          const cell=el('div',`git-split-cell ${source?.kind||'blank'}`);
          const number=el('span','git-line-number',source?(side==='before'?source.old:source.current):'');number.setAttribute('aria-hidden','true');
          const code=el('span','git-code');
          if(source){
            if(parts){code.append(el('span','',parts[index][0]),el('mark','git-inline-change',parts[index][1]),el('span','',parts[index][2]));}
            else code.textContent=source.line.slice(1);
            if(source.noNewline)code.append(el('span','git-no-newline',' ⏎ No newline at end of file'));
          }
          cell.append(number,code);line.append(cell);
        }
        fragment.append(line);
      }else{
        const line=el('div',`git-diff-line ${row.kind}`);register(line,row);
        const left=el('span','git-line-number',row.old),right=el('span','git-line-number',row.current);
        left.setAttribute('aria-hidden','true');right.setAttribute('aria-hidden','true');
        line.append(left,right,el('span','git-code',row.line));fragment.append(line);
      }
    }
    diff.replaceChildren(fragment);markDifference();updateDiffControls();
  }
  function updateDiffLabels(v){
    const file=v.files?.[v.file];
    beforeLabel.textContent=v.kind==='edits'?(file?.group==='Staged'?'HEAD':file?.group==='Untracked'?'New file':'Index'):
      v.parent?`Before · ${v.parent.slice(0,8)}`:'Empty tree';
    afterLabel.textContent=v.kind==='edits'?(file?.group==='Staged'?'Index':'Working tree'):`After · ${((v.allChanges?v.head:v.commit)||'').slice(0,8)}`;
  }
  root.addEventListener('keydown',event=>{
    if(!event.altKey||event.ctrlKey||event.metaKey||event.shiftKey||event.isComposing||event.defaultPrevented)return;
    if(['INPUT','TEXTAREA','SELECT'].includes(event.target?.tagName)||event.target?.isContentEditable)return;
    const control={ArrowUp:previousDiff,ArrowDown:nextDiff,ArrowLeft:previousFile,ArrowRight:nextFile}[event.key];
    if(control){event.preventDefault();if(!control.disabled)control.click();}
  });
  const emptyDetail=el('div','git-detail-empty');
  content.append(diffHeader,diffToolbar,partTabs,note,sides,diff,emptyDetail);
  sidebar.append(commits);detailPanes.append(files,content);inspection.append(commitDetail,detailPanes);
  panes.append(sidebar,inspection);root.append(header,error,panes,statusbar);
  return v=>{
    const changedContext=state?.siteID!==v.siteID||state?.kind!==v.kind;
    state=v;
    updateDiffControls();
    branchDiffButton.hidden=v.kind!=='branch';
    branchDiffButton.disabled=v.loading||!v.branchDiff?.base;
    branchDiffButton.classList.toggle('selected',!!v.allChanges);branchDiffButton.setAttribute('aria-pressed',String(!!v.allChanges));
    branchDiffButton.title=v.branchDiff?.note||`Compare committed branch changes from the common ancestor with ${v.branchDiff?.target||'the base branch'}`;
    historyScope.hidden=v.kind!=='branch';
    for(const [button,selected] of [[branchHistoryButton,!!v.branchOnly],[fullHistoryButton,!v.branchOnly]]) {
      button.disabled=!!v.loading;
      button.classList.toggle('selected',selected);button.setAttribute('aria-pressed',String(selected));
    }
    commitList.setAttribute('aria-label',v.branchOnly?'Branch commits':'Full history');
    renderEditor(v);
    root.setAttribute('aria-busy',String(v.loading));
    commitList.setAttribute('aria-busy',String(v.loading));fileList.setAttribute('aria-busy',String(v.loading));
    // Selecting a row reads its diff asynchronously. Keep the current frame
    // intact until it arrives, instead of flashing loading text and controls.
    if(v.loading&&renderedState?.siteID===v.siteID&&renderedState?.kind===v.kind)return;
    const edits=v.kind==='edits';
    commits.hidden=edits;panes.classList.toggle('edits-only',edits);
    // Move the same file list between the sidebar and selected-commit details.
    // Retaining its node also retains scrolling and focus on ordinary refreshes.
    placeFiles();
    if(changedContext&&v.loading){
      commitList.replaceChildren();fileList.replaceChildren();commitDetail.replaceChildren();diff.replaceChildren();
      commitList.removeAttribute('aria-activedescendant');fileList.removeAttribute('aria-activedescendant');
      renderedState=null;renderedVersion=null;commitRowsKey=null;fileRowsKey=null;
      model=null;fileRows=[];changeNodes=[];difference=-1;sides.hidden=true;updateDiffControls();
      diffTitle.textContent='';note.hidden=true;fileHeading.textContent='Files';
      previous.disabled=true;next.disabled=true;pageLabel.textContent='';
    }
    name.textContent=v.name||'Changes';name.title=v.path;
    branch.textContent=v.branch||'';branch.title=v.branch||'';
    updateBranchButton();
    tabs.querySelectorAll('button').forEach(tab=>{
      const category=viewCategory(v,tab.dataset.kind);
      tab.classList.toggle('selected',tab.dataset.kind===v.kind);
      tab.classList.toggle('empty-category',category.empty);
      tab.setAttribute('aria-pressed',String(tab.dataset.kind===v.kind));
      tab.setAttribute('aria-label',`${category.label}, ${category.count}`);
      tab.querySelector('.git-tab-count').textContent=category.count;
      tab.querySelector('.git-tab-count').hidden=tab.dataset.kind!=='edits';
      tab.title=category.description;
    });
    // Inert would discard keyboard focus on every read. Keep the list focused
    // while loading; send() guards read requests and the editor disables itself.
    refresh.disabled=v.loading;
    error.hidden=!v.error;error.textContent=v.error||'';
    const capture=v.captured?new Date(v.captured).toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit'}):'';
    context.replaceChildren();
    context.hidden=edits;
    if(!edits){
      if(v.loading)context.append(el('span','muted','Loading history…'));
      else if(v.branchOnly){
        const count=v.branchCommits;
        context.append(el('strong','git-history-count',Number.isInteger(count)?`${count.toLocaleString()} branch ${count===1?'commit':'commits'}`:'Branch commits'),
          el('span','muted',`Since the common ancestor with ${v.branchDiff?.target||'the base branch'}`));
      }else {
        const outgoing=v.branchWork?.local ?? (Number.isInteger(v.counts?.local)?v.counts.local:null);
        const diverged=v.counts?.local>0&&v.incomingCount>0;
        if(diverged)context.append(el('span','git-history-count',v.rebased?'Rebased':'History differs'));
        if(outgoing>0)context.append(el('span','git-history-count outgoing',`↑ ${outgoing} ${v.comparisonOnly||diverged?'local only':'not pushed'}`));
        if(v.incomingCount>0)context.append(el('span','git-history-count incoming',`↓ ${v.incomingCount} ${diverged?'remote only':'incoming'}`));
        if(outgoing===0&&v.incomingCount===0)context.append(el('span','muted',v.comparisonOnly?'Matches comparison history':'Matches last fetched history'));
        else if(!context.children.length)context.append(el('span','muted',v.upstream?'Branch history':'Local history'));
      }
      context.title=branchHistoryContext(v);
    }
    statusbar.textContent=[v.loading?'Refreshing…':edits?'Uncommitted changes':v.branchOnly||v.allChanges?`Branch changes since the common ancestor with ${v.branchDiff?.target||'the base'}`:v.tracking?`Compared with ${v.tracking}`:'Local history',
      !v.loading&&capture?`Read at ${capture}`:'',v.comparisonOnly?'No upstream configured':'',v.remoteNote,v.historyNote,
      v.rebased?'Rebased history: equivalent changes can have different commit IDs.':v.counts?.local>0&&v.incomingCount>0?'History differs: remote-only commits may be older versions after a rebase.':'',
      v.branchWork?.inherited?`${v.branchWork.inherited} additional commits already on ${v.branchWork.base}`:''].filter(Boolean).join(' · ');
    // Keep scroll and text selection intact for duplicate/loading-only emissions.
    if(v.loading)return;
    if(renderedVersion===v.version)return;
    renderedVersion=v.version;
    const previousState=renderedState;
    const sameList=previousState?.siteID===v.siteID&&previousState?.kind===v.kind&&!!previousState?.branchOnly===!!v.branchOnly&&previousState?.page===v.page;
    const commitScroll=sameList?commitList.scrollTop:0;
    const fileScroll=sameList&&renderedState?.commit===v.commit?fileList.scrollTop:0;
    const sameDiff=sameDiffSelection(previousState,v);
    const diffScroll=sameDiff?{top:diff.scrollTop,left:diff.scrollLeft}:{top:0,left:0};
    const focusedPart=partTabs.contains(document.activeElement)?document.activeElement?.dataset.group:null;
    renderedState=v;
    const nextCommitRowsKey=JSON.stringify([v.siteID,v.kind,v.branchOnly,v.page,v.commits,!!v.error,v.comparisonOnly,v.branchWork?.base,v.counts?.local>0&&v.incomingCount>0,v.tracking]);
    if(commitRowsKey!==nextCommitRowsKey){
      commitRowsKey=nextCommitRowsKey;
      commitList.replaceChildren();
      let sharedStarted=false;
      for(const commit of v.commits||[]) {
        const day=historyDay(commit.date);
        const shared=commit.presence==='shared'||commit.presence==='local';
        if(!v.branchOnly&&shared&&!sharedStarted&&(v.commits||[]).some(c=>c.presence==='incoming'||c.presence==='outgoing')){
          commitList.append(el('div','git-history-divider','Shared history'));sharedStarted=true;
        }
        const item=button('',()=>{commitList.focus({preventScroll:true});send('commit',{commit:commit.id});},'git-commit');
        listOption(commitList,item,`git-commit-${commitList.children.length}`,commit.id===v.commit);
        item.classList.toggle('incoming',commit.presence==='incoming');item.classList.toggle('outgoing',commit.presence==='outgoing');
        const marker=el('span','git-history-dot');marker.setAttribute('aria-hidden','true');
        const copy=el('span','git-commit-copy');
        copy.append(el('strong','git-commit-subject',commit.subject||'(No subject)'));
        const metadata=el('span','git-commit-meta');
        metadata.append(el('span','git-commit-author',commit.author||'Unknown author'),el('span','git-commit-date',day),el('span','git-commit-sha',commit.id.slice(0,7)));
        copy.append(metadata);
        const label=v.comparisonOnly?{outgoing:'↑ Local only',shared:'Shared'}[commit.presence]:'';
        const presence=label||commitPresenceLabel(commit.presence,v);
        // Ordinary shared commits need no repeated badge; retain the full state in
        // the accessible name and tooltip so presence never depends on color.
        if(!shared&&presence)copy.append(el('span',`git-commit-presence ${commit.presence}`,presence));
        item.append(marker,copy);
        item.title=[commit.subject,commit.author,day,commit.id,presence].filter(Boolean).join('\n');
        item.setAttribute('aria-label',[commit.subject,commit.author,day,presence].filter(Boolean).join(', '));
        commitList.append(item);
      }
      if(!(v.commits||[]).length)commitList.append(el('p','git-empty',v.branchOnly?(v.error?'Branch commits unavailable. Open Full history to inspect individual commits.':'No commits added since the branch base.'):v.kind==='branch'?'No commits yet.':v.kind==='remote'?'No remote history available.':`No local-only commits compared with ${v.tracking||'the remote history'}.`));
    }
    commitList.removeAttribute('aria-activedescendant');
    commitList.querySelectorAll('button').forEach((item,index)=>listOption(commitList,item,item.id,v.commits[index].id===v.commit));
    previous.disabled=v.page===0;next.disabled=!v.more;pageLabel.textContent=`Page ${v.page+1}`;
    summaryHeading(fileHeading,v.files||[],null,v.filesLimited);
    const rows=edits?changeFileRows(v.files||[],v.file):(v.files||[]).map((file,index)=>({path:file.path,file,index,selected:index===v.file,parts:[{file,index}]}));
    fileRows=rows;
    const nextFileRowsKey=JSON.stringify([v.siteID,v.kind,v.commit,v.allChanges,v.files,rows.map(row=>row.file.status),v.filesLimited,!!v.error]);
    if(fileRowsKey!==nextFileRowsKey){
      fileRowsKey=nextFileRowsKey;
      fileList.replaceChildren();
      for(const row of rows) {
        const {file,index}=row;
        const item=button('',()=>{fileList.focus({preventScroll:true});send('file',{file:index});},'git-file');
        listOption(fileList,item,`git-file-${index}`,row.selected);
        const status=file.status.startsWith('R')?'R':file.status[0];
        const copy=el('span','git-file-copy');
        const slash=file.path.lastIndexOf('/');
        copy.append(el('span','git-file-name',file.path.slice(slash+1)));
        if(slash>=0)copy.append(el('span','git-file-directory',file.path.slice(0,slash)));
        if(edits){
          const labels=row.parts.map(part=>changePartLabel(part.file.group));
          copy.append(el('span','git-file-parts',labels.join(' · ')));
        }
        item.append(el('span',`git-file-status status-${status}`,status),copy);
        item.title=row.parts.map(part=>part.file.oldPath?`${part.file.oldPath} → ${part.file.path}`:part.file.path).filter((path,index,all)=>all.indexOf(path)===index).join('\n');
        item.setAttribute('aria-label',`${file.path}, ${row.parts.map(part=>changePartLabel(part.file.group)).join(' and ')} (${file.status})`);fileList.append(item);
      }
      if(!(v.files||[]).length)fileList.append(el('p','git-empty',v.error?'File list unavailable.':v.kind==='edits'?'No uncommitted changes.':v.allChanges?'No branch changes.':v.commit?'This commit has no changed files.':'Select a commit.'));
      if(v.filesLimited)fileList.append(el('p','git-empty','Showing the first 2,000 files.'));
    }
    fileList.removeAttribute('aria-activedescendant');
    fileList.querySelectorAll('button').forEach((item,index)=>{
      const row=rows[index];
      listOption(fileList,item,item.id,row.selected);
      item.onclick=()=>{fileList.focus({preventScroll:true});send('file',{file:row.index});};
    });
    partTabs.replaceChildren();
    const selectedRow=edits?rows.find(row=>row.selected):null;
    partTabs.hidden=!selectedRow;
    if(selectedRow){
      for(const part of selectedRow.parts){
        const tab=button(changePartLabel(part.file.group),()=>send('file',{file:part.index}));
        tab.dataset.group=part.file.group;
        tab.classList.toggle('selected',part.index===v.file);tab.setAttribute('aria-pressed',String(part.index===v.file));
        tab.disabled=selectedRow.parts.length===1;partTabs.append(tab);
        if(focusedPart===part.file.group&&!tab.disabled)tab.focus({preventScroll:true});
      }
      if(selectedRow.parts.length>1)partTabs.append(el('span','muted','This file has staged and working changes.'));
    }
    commitList.scrollTop=commitScroll;fileList.scrollTop=fileScroll;
    if(document.activeElement===commitList&&(!sameList||previousState?.commit!==v.commit))commitList.onfocus();
    if(document.activeElement===fileList&&(!sameList||!sameDiff))fileList.onfocus();
    if(initialFocus){
      initialFocus=false;
      if(document.activeElement===document.body||document.activeElement===root)(edits?fileList:commitList).focus({preventScroll:true});
    }
    commitDetail.hidden=v.kind==='edits'||(!v.commit&&!v.allChanges);
    const sameCommitDetail=previousState?.siteID===v.siteID&&previousState?.kind===v.kind&&previousState?.commit===v.commit&&
      previousState?.allChanges===v.allChanges&&(!v.allChanges||(previousState?.head===v.head&&previousState?.branchDiff?.target===v.branchDiff?.target))&&
      previousState?.message===v.message&&previousState?.parent===v.parent&&previousState?.merge===v.merge&&
      JSON.stringify(previousState?.commits?.find(c=>c.id===v.commit))===JSON.stringify(v.commits?.find(c=>c.id===v.commit));
    if(!sameCommitDetail){
      commitDetail.replaceChildren();
      if(v.allChanges){
        commitDetail.append(el('strong','','All branch changes'),el('p','muted',`${v.branchDiff?.target||'Base'} · common ancestor ${(v.parent||'').slice(0,8)} → ${v.branch||'branch'} ${(v.head||'').slice(0,8)} · Committed changes only`));
      }else if(!commitDetail.hidden){
        const message=el('details','');const subject=el('summary','',(v.message||'').split('\n')[0]);
        message.append(subject,el('pre','',v.message||''));commitDetail.append(message);
        const selectedCommit=v.commits?.find(c=>c.id===v.commit);
        if(selectedCommit){
          const meta=el('div','git-detail-meta');
          meta.append(el('span','',selectedCommit.author||'Unknown author'),el('span','',historyDay(selectedCommit.date)),el('code','',v.commit.slice(0,8)));
          const label=v.comparisonOnly?{outgoing:'↑ Local only',shared:'Shared'}[selectedCommit.presence]:'';
          meta.append(el('span',`git-commit-presence ${selectedCommit.presence}`,label||commitPresenceLabel(selectedCommit.presence,v)));
          commitDetail.append(meta);
        }
        if(v.merge)commitDetail.append(el('p','muted',`Merge commit · compared with first parent ${v.parent.slice(0,8)}`));
        else if(!v.parent)commitDetail.append(el('p','muted','Initial commit · compared with an empty tree'));
      }
    }
    const file=v.files?.[v.file];
    emptyDetail.hidden=!!file;diffHeader.hidden=!file;diff.hidden=!file;
    if(!file){
      const clean=edits&&v.counts?.edits===0&&!v.error;
      emptyDetail.replaceChildren(el('strong','',v.error?'Changes unavailable':clean?'No uncommitted changes':edits?'No file selected':v.allChanges?'No branch changes':v.commit?'No file changes':'No commit selected'),
        el('p','',v.error?'Review the error above, then refresh to try again.':clean?'Your working tree is clean. Open History to review commits.':edits?'Select a file to inspect its changes.':v.allChanges?'The branch matches its common ancestor. Select a commit to inspect its changes.':v.commit?'This commit does not change any files.':'Select a commit from the history to inspect its changes.'));
    }
    diffTitle.textContent=file?(file.oldPath?`${file.oldPath} → ${file.path}`:file.path):'Select a file to view its diff';
    diffTitle.title=diffTitle.textContent;
    note.hidden=!v.diffNote;note.textContent=v.diffNote||'';
    if(!sameDiff||previousState?.diff!==v.diff){
      model=splitDiff(v.diff||'');
      difference=model.changes?(sameDiff?Math.max(0,Math.min(difference,model.changes-1)):0):-1;
      paintDiff(v);
    }
    updateDiffControls();
    updateDiffLabels(v);
    diff.scrollTop=diffScroll.top;diff.scrollLeft=diffScroll.left;
  };
}
