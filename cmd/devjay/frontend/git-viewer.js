import { mountFileEditor } from './file-editor.js';
import { syntaxLines } from './syntax-highlight.js';
import { filterSearchRows, appendSearchSyntax } from './diff-search.js';
import { defaultReviewIgnorePatterns, reviewIgnoreMatchers, reviewFileIgnored, ignorePatternsStorageKey, storedIgnorePatterns, saveIgnorePatterns } from './file-ignore.js';
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
  const unified=diffRows(text), rows=[], budget={cells:inlineDiffBudget};
  let changes=0, block=[], last=null;
  const flush=()=>{
    if(!block.length)return;
    const change=changes++, before=block.filter(row=>row.kind==='delete'), after=block.filter(row=>row.kind==='add');
    for(const row of block)row.change=change;
    // Both layouts render these same row objects, so they share the ranges.
    const inline=before.length&&after.length?inlineChanges(before.map(row=>row.line.slice(1)),after.map(row=>row.line.slice(1)),budget):null;
    if(inline){before.forEach((row,i)=>{row.inline=inline.before[i];});after.forEach((row,i)=>{row.inline=inline.after[i];});}
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

// Word-level changes within a block of deleted and added lines. Each side is
// compared as one text, so a statement reflowed across lines (gofmt/prettier)
// only marks its real edits; line breaks and indentation are not changes.
// Returns per-line [start, end) UTF-16 ranges, or null when the sides are too
// different for marks to help, or the comparison exceeds its cell budget.
const inlineDiffBudget=2000000, inlineBlockCells=250000, inlineSimilarity=0.4;
const inlineTokenPattern=/\n|[ \t]+|[\p{L}\p{N}_]+|[^\s\p{L}\p{N}_]/gu;
export function inlineChanges(before, after, budget={cells:inlineDiffBudget}) {
  const tokenize=lines=>{
    const tokens=[];let line=0,column=0;
    for(const [text] of lines.join('\n').matchAll(inlineTokenPattern)){
      tokens.push({text,line,column,space:/^\s+$/.test(text)});
      if(text==='\n'){line++;column=0;}else column+=text.length;
    }
    return tokens;
  };
  const a=tokenize(before),b=tokenize(after);
  let prefix=0,suffix=0;
  while(prefix<a.length&&prefix<b.length&&a[prefix].text===b[prefix].text)prefix++;
  while(suffix<a.length-prefix&&suffix<b.length-prefix&&a[a.length-1-suffix].text===b[b.length-1-suffix].text)suffix++;
  const n=a.length-prefix-suffix,m=b.length-prefix-suffix,cells=(n+1)*(m+1);
  if(cells>inlineBlockCells||cells>budget.cells)return null;
  budget.cells-=cells;
  // Suffix LCS table, then a forward walk that marks tokens outside the LCS.
  const width=m+1,table=new Uint32Array(cells);
  for(let i=n-1;i>=0;i--)for(let j=m-1;j>=0;j--){
    table[i*width+j]=a[prefix+i].text===b[prefix+j].text?table[(i+1)*width+j+1]+1:Math.max(table[(i+1)*width+j],table[i*width+j+1]);
  }
  for(let i=0,j=0;i<n||j<m;){
    if(i<n&&j<m&&a[prefix+i].text===b[prefix+j].text){i++;j++;}
    else if(j>=m||(i<n&&table[(i+1)*width+j]>=table[i*width+j+1]))a[prefix+i++].changed=true;
    else b[prefix+j++].changed=true;
  }
  const visible=tokens=>tokens.reduce((sum,token)=>sum+(token.space||token.text==='\n'?0:token.text.length),0);
  const common=visible(a.filter(token=>!token.changed));
  if(2*common<inlineSimilarity*(visible(a)+visible(b)))return null;
  const ranges=(tokens,lines)=>{
    const result=lines.map(()=>[]);
    for(const token of tokens){
      if(!token.changed||token.text==='\n')continue;
      const line=result[token.line],range=[token.column,token.column+token.text.length];
      // Join neighbouring marks separated only by whitespace into one span.
      const previous=line.at(-1);
      if(previous&&/^\s*$/.test(lines[token.line].slice(previous[1],range[0])))previous[1]=range[1];
      else line.push(range);
    }
    // Whitespace is layout here (reflowed calls, indentation); trim it from
    // marks and drop whitespace-only marks. Ignore whitespace hides the rest.
    return result.map((line,index)=>line.map(([from,to])=>{
      const text=lines[index].slice(from,to);
      return text.trim()?[from+text.length-text.trimStart().length,to-(text.length-text.trimEnd().length)]:null;
    }).filter(Boolean));
  };
  return {before:ranges(a,before),after:ranges(b,after)};
}

// A patch does not include the whole file. Keep before/after lexer states
// independent inside each hunk, and reset at omitted sections rather than
// inferring their contents or carrying a comment through an unknown gap.
export function patchSyntaxRows(rows, path, oldPath = path) {
  const result = new Map();
  let before = [], after = [], remaining = 30000;
  const flush = () => {
    for (const [side, source, name] of [['before', before, oldPath], ['after', after, path]]) {
      if (!source.length || remaining <= 0) continue;
      const lines = syntaxLines(name, source.map(row => row.line.slice(1)).join('\n'));
      for (let index = 0; index < source.length; index++) {
        const tokens = lines[index];
        remaining -= tokens?.length || 0;
        if (remaining < 0) break;
        const values = result.get(source[index]) || {};
        values[side] = tokens; result.set(source[index], values);
      }
    }
    before = []; after = [];
  };
  for (const row of rows) {
    if (row.kind === 'hunk') flush();
    else if (row.kind === 'delete') before.push(row);
    else if (row.kind === 'add') after.push(row);
    else if (row.kind === 'context') { before.push(row); after.push(row); }
    else if (row.line !== '\\ No newline at end of file') flush();
  }
  flush();
  return result;
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
  function summaryHeading(node, files, label, limited=false, hidden=0){
    const counts=summarizeFiles(files,limited);
    const title=`${counts.count}${hidden?` / ${counts.count+hidden}`:''}${limited?'+':''} ${label|| (counts.count+hidden===1?'FILE':'FILES')}`;
    const totals=el('span','git-line-totals');
    totals.append(el('span','git-added',`+${counts.added.toLocaleString()}`),el('span','git-removed',`−${counts.removed.toLocaleString()}`));
    if(counts.partial)totals.append(el('span','muted','partial'));
    node.replaceChildren(el('span','',title),totals);
    node.classList.add('git-summary');
    node.title=`${counts.added} lines added, ${counts.removed} lines removed${counts.binary?`; ${counts.binary} binary file changes excluded from line counts`:''}${counts.partial?'; totals are incomplete because some files could not be counted or the list is limited':''}${hidden?`; ${hidden} ignored ${hidden===1?'file is':'files are'} hidden and not counted`:''}. Staged and unstaged changes are counted separately.`;
  }
  let state=null, renderedVersion=null, renderedState=null, initialFocus=true, activeList=null;
  let commitRowsKey=null, fileRowsKey=null, overviewKey=null;
  let requestedListItem=null, queuedListItem=null, pendingEdge=null, ignoredRedirect=null;
  const send=(operation,extra={})=>{if(state&&!state.loading)request({version:state.version,operation,...extra});};
  function selectListItem(list,item,operation,extra){
    activeList=list;list.focus({preventScroll:true});
    if(state?.loading)return;
    if(operation==='commit'&&extra.commit===state?.commit&&!state?.allChanges)return;
    requestedListItem={list,item};send(operation,extra);
  }
  // Keep focus on the list itself: replacing rows or loading another diff must
  // not lose the next arrow key. Only the selected option enters the details.
  function navigableList(label) {
    const list=el('div','git-scroll-list');list.tabIndex=0;
    list.setAttribute('role','listbox');list.setAttribute('aria-label',label);
    list.title='↑/↓ to select · Home/End for first/last';
    const selected=()=>list.querySelector('.selected');
    list.onfocus=()=>{activeList=list;selected()?.scrollIntoView({block:'nearest',inline:'nearest'});};
    list.onkeydown=event=>{
      if(event.altKey||event.ctrlKey||event.metaKey||event.shiftKey||event.isComposing||event.defaultPrevented)return;
      if(!['ArrowUp','ArrowDown','Home','End'].includes(event.key))return;
      event.preventDefault();
      if(!state)return;
      const items=[...list.querySelectorAll('button')];
      if(!items.length)return;
      const pending=queuedListItem||requestedListItem;
      const current=items.indexOf(pending?.list===list&&items.includes(pending.item)?pending.item:selected());
      const index=event.key==='Home'?0:event.key==='End'?items.length-1:
        current<0?0:Math.max(0,Math.min(items.length-1,current+(event.key==='ArrowDown'?1:-1)));
      if(index!==current){
        if(state.loading)queuedListItem={list,item:items[index]};
        else items[index].click();
      }
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
  title.append(name);
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
  branchRow.append(branchName);
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
  const toolbarIcon=path=>{
    const icon=document.createElementNS('http://www.w3.org/2000/svg','svg');
    for(const [name,value] of Object.entries({viewBox:'0 0 16 16',width:16,height:16,fill:'none',stroke:'currentColor','stroke-width':1.5,'stroke-linecap':'round','stroke-linejoin':'round','aria-hidden':'true',focusable:'false'}))icon.setAttribute(name,value);
    const shape=document.createElementNS('http://www.w3.org/2000/svg','path');shape.setAttribute('d',path);icon.append(shape);return icon;
  };
  const refresh=button('',()=>send('refresh'),'git-refresh');
  refresh.append(toolbarIcon('M13.2 5.5A5.5 5.5 0 1 0 13.5 9M13.2 1.5v4h-4'));
  refresh.setAttribute('aria-label','Refresh');
  refresh.title='Reread files and the last fetched history. Push and Update check the remote before confirmation.';
  const reviewBranch=button('',()=>send('review-open'),'git-review-branch');
  reviewBranch.append(toolbarIcon('M1.5 8C4.5 2 11.5 2 14.5 8C11.5 14 4.5 14 1.5 8ZM10 8A2 2 0 1 1 6 8A2 2 0 0 1 10 8Z'),el('span','','Review'));
  reviewBranch.setAttribute('aria-label','Review');
  reviewBranch.title='Review branch changes from the fork point, including uncommitted edits';
  branchName.append(refresh);
  const tools=el('div','git-view-tools');tools.append(reviewBranch);
  header.append(title,branchRow,...(suggestions?[suggestions]:[]),tools);
  const context=el('div','git-view-context');
  const historyOrder=el('select','git-history-order');
  historyOrder.setAttribute('aria-label','Commit order');historyOrder.title='Sort branch commits';
  for(const [value,label] of [['newest','Newest first'],['oldest','Oldest first']]){
    const option=el('option','',label);option.value=value;historyOrder.append(option);
  }
  historyOrder.onchange=()=>{
    if(!historyOrder.disabled&&state?.kind==='branch'&&state.branchOnly)send('history-sort',{order:historyOrder.value});
  };
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
  const branchSummary=el('div','git-branch-summary');
  const branchDiffButton=button('All branch changes',()=>{setLayout('split');send('branch-diff');},'git-branch-diff');
  branchSummary.append(branchDiffButton,historyOrder);
  const commitList=navigableList('Branch commits');
  const pagination=el('div','git-view-pagination');
  const previous=button('Previous',()=>send('page',{page:state.page-1}));
  const pageLabel=el('span','muted');const next=button('Next',()=>send('page',{page:state.page+1}));
  pagination.append(previous,pageLabel,next);commits.append(branchSummary,commitList,pagination);
  const files=el('aside','git-view-files');files.setAttribute('aria-label','Changed files');
  const fileHeading=el('h2','','Files'),fileList=navigableList('Changed files');
  // Shared with Review: patterns are saved per repository, while showing
  // ignored files is a temporary choice for this window.
  const ignoreOptions=el('details','git-ignore-options');
  const ignoreSummary=el('summary','','Ignored files');
  const showIgnoredLabel=el('label','git-show-ignored'),showIgnored=el('input','');
  showIgnored.type='checkbox';showIgnored.setAttribute('aria-label','Show ignored files');
  showIgnoredLabel.append(showIgnored,el('span','','Show ignored files'));
  const patternsLabel=el('label','git-ignore-label','Ignore patterns');
  const ignorePatterns=el('textarea','git-ignore-patterns');ignorePatterns.rows=3;ignorePatterns.maxLength=4096;
  ignorePatterns.spellcheck=false;ignorePatterns.setAttribute('aria-label','Ignore file patterns');
  ignorePatterns.value=defaultReviewIgnorePatterns;patternsLabel.append(ignorePatterns);
  const ignoreHelp=el('p','git-ignore-help','Separate patterns with commas or new lines. Names match in any folder; use folder/ for a folder, or ** to match across folders. Shared with Review for this repository.');
  ignoreOptions.append(ignoreSummary,showIgnoredLabel,patternsLabel,ignoreHelp);
  files.append(fileHeading,ignoreOptions,fileList);
  let ignoreKey='',ignoreMatchers=reviewIgnoreMatchers(ignorePatterns.value);
  const fileHidden=file=>!showIgnored.checked&&reviewFileIgnored(file,ignoreMatchers);
  function loadIgnorePatterns(v){
    const key=ignorePatternsStorageKey(v.path||v.siteID),stored=storedIgnorePatterns(key);
    // Follow patterns saved by Review, without replacing text being edited here.
    const value=key!==ignoreKey?stored??defaultReviewIgnorePatterns:document.activeElement===ignorePatterns?null:stored;
    ignoreKey=key;
    if(value!==null&&value!==ignorePatterns.value){ignorePatterns.value=value;ignoreMatchers=reviewIgnoreMatchers(value);}
  }
  function applyFileFilters(){
    if(!state||state.loading)return; // The next completed read applies them.
    renderedVersion=null;render(state);
  }
  ignorePatterns.oninput=()=>{
    ignoreMatchers=reviewIgnoreMatchers(ignorePatterns.value);
    if(ignoreKey)saveIgnorePatterns(ignoreKey,ignorePatterns.value);
    applyFileFilters();
  };
  showIgnored.onchange=applyFileFilters;
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
  layoutControls.append(unifiedButton,splitButton);
  const whitespaceButton=button('Ignore whitespace',()=>{if(state&&!state.loading)send('whitespace',{ignoreWhitespace:!state.ignoreWhitespace});},'git-diff-whitespace');
  whitespaceButton.title='Hide changes that only add, remove, or reindent whitespace';
  diffToolbar.append(differenceControls,fileControls,layoutControls,whitespaceButton);
  const findBar=el('div','diff-find');findBar.hidden=true;
  const findInput=el('input','diff-find-input');findInput.type='search';findInput.maxLength=512;findInput.placeholder='Find in diff…';findInput.setAttribute('aria-label','Find text in diff');
  const findStatus=el('span','diff-find-status');findStatus.setAttribute('role','status');
  const findClose=button('×',closeFind,'diff-find-close');findClose.title='Close find (Escape)';findClose.setAttribute('aria-label',findClose.title);
  findBar.append(findInput,findStatus,findClose);
  let findScroll=null;
  function refreshFind(){
    if(!model||!(renderedState||state))return;
    if(findInput.value&&!findScroll)findScroll={top:diff.scrollTop,left:diff.scrollLeft};
    paintDiff(renderedState||state);
    if(findInput.value)diff.scrollTop=0;
    else if(findScroll){diff.scrollTop=findScroll.top;diff.scrollLeft=findScroll.left;findScroll=null;}
  }
  function closeFind(){findBar.hidden=true;findInput.value='';refreshFind();diff.focus({preventScroll:true});}
  findInput.oninput=refreshFind;
  const visibleDifferences=()=>changeNodes.flatMap((node,index)=>node?[index]:[]);
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
    const visible=visibleDifferences();
    // At either end of a file, difference navigation continues into the
    // adjacent displayed file, matching Review.
    previousDiff.disabled=loading||(!visible.some(index=>index<difference)&&index<=0);
    nextDiff.disabled=loading||(!visible.some(index=>index>difference)&&(index<0||index>=fileRows.length-1));
    previousFile.disabled=loading||index<=0;
    nextFile.disabled=loading||index<0||index>=fileRows.length-1;
    differenceStatus.textContent=count&&difference>=0?`${difference+1} / ${count} ${count===1?'difference':'differences'}`:findInput.value?'No visible differences':'No text differences';
    fileStatus.textContent=`${index<0?0:index+1} / ${fileRows.length}${state?.filesLimited?'+':''} files`;
    unifiedButton.disabled=loading;splitButton.disabled=loading||model?.supported===false;
    whitespaceButton.disabled=loading;
    whitespaceButton.classList.toggle('selected',!!state?.ignoreWhitespace);whitespaceButton.setAttribute('aria-pressed',String(!!state?.ignoreWhitespace));
    splitButton.title=model?.supported===false?'Combined conflict diffs are shown in unified form.':'Show aligned before and after versions';
    for(const [node,value] of [[unifiedButton,'unified'],[splitButton,'split']]){
      const selected=(layout==='split'&&model?.supported!==false?'split':'unified')===value;
      node.classList.toggle('selected',selected);node.setAttribute('aria-pressed',String(selected));
    }
  }
  function moveDifference(direction){
    if(!state||state.loading)return;
    const visible=visibleDifferences(),next=(direction<0?visible.reverse():visible).find(index=>direction<0?index<difference:index>difference);
    if(next!==undefined){
      difference=next;markDifference();updateDiffControls();
      changeNodes[difference]?.scrollIntoView({block:'center',inline:'nearest'});
      return;
    }
    // Open the adjacent file at its nearest difference. Files without text
    // differences are still shown; pressing again continues past them.
    const index=fileRows.findIndex(row=>row.selected),row=fileRows[index+direction];
    if(index<0||!row)return;
    pendingEdge={file:row.index,edge:direction<0?'last':'first'};
    send('file',{file:row.index});
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
  function appendInline(code,text,tokens,query,ranges,budget){
    let cursor=0;
    for(const [start,end] of ranges||[]){
      if(start>cursor)appendSearchSyntax(code,text,tokens,query,cursor,start,budget);
      const mark=el('mark','git-inline-change');appendSearchSyntax(mark,text,tokens,query,start,end,budget);code.append(mark);cursor=end;
    }
    if(cursor<text.length)appendSearchSyntax(code,text,tokens,query,cursor,text.length,budget);
  }
  function paintDiff(v){
    const split=layout==='split'&&model?.supported!==false;
    const file=v.files?.[v.file];
    const syntax=model?.supported===false?new Map():patchSyntaxRows(model?.unified||[],file?.path||'',file?.oldPath||file?.path||'');
    diff.classList.toggle('side-by-side',split);sides.hidden=!split||!v.files?.[v.file]||fileHidden(v.files[v.file]);
    updateDiffLabels(v);
    const fragment=document.createDocumentFragment(),highlightBudget={remaining:2048};changeNodes=[];
    const register=(line,row)=>{
      if(row.change===undefined)return;
      line.classList.add('git-difference');line.dataset.change=String(row.change);
      if(!changeNodes[row.change])changeNodes[row.change]=line;
    };
    const query=findInput.value||'';
    const search=filterSearchRows((split?model?.rows:model?.unified)||[],query,row=>{
      if(split&&(row.kind==='change'||row.kind==='context'))return [row.before,row.after].filter(Boolean).map(side=>side.line.slice(1));
      return ['context','add','delete'].includes(row.kind)?[row.line.slice(1)]:model?.supported===false?[row.line||'']:[];
    });
    findStatus.textContent=query?(search.matchingRows?`${search.matchingRows} matching ${search.matchingRows===1?'line':'lines'}`:'No matches'):'Search current diff';
    for(const row of search.rows){
      if(row.kind==='search-gap'){fragment.append(el('div','diff-search-gap',`… ${row.count} lines hidden …`));continue;}
      // File paths and revision labels are already visible above the panes.
      // Keep meaningful metadata (renames, modes, binary notices) in the diff.
      if(split&&row.kind==='meta'&&/^(diff --git |index |--- |\+\+\+ )/.test(row.line))continue;
      if(split&&(row.kind==='change'||row.kind==='context')){
        const line=el('div','git-split-line');register(line,row);
        for(const [side,source] of [['before',row.before],['after',row.after]]){
          const cell=el('div',`git-split-cell ${source?.kind||'blank'}`);
          const number=el('span','git-line-number',source?(side==='before'?source.old:source.current):'');number.setAttribute('aria-hidden','true');
          const code=el('span','git-code');
          if(source){
            const text=source.line.slice(1),tokens=syntax.get(source)?.[side];
            appendInline(code,text,tokens,query,row.kind==='change'?source.inline:null,highlightBudget);
            if(source.noNewline)code.append(el('span','git-no-newline',' ⏎ No newline at end of file'));
          }
          cell.append(number,code);line.append(cell);
        }
        fragment.append(line);
      }else{
        const line=el('div',`git-diff-line ${row.kind}`);register(line,row);
        const left=el('span','git-line-number',row.old),right=el('span','git-line-number',row.current);
        left.setAttribute('aria-hidden','true');right.setAttribute('aria-hidden','true');
        const code=el('span','git-code');
        if(['context','add','delete'].includes(row.kind)){
          code.append(el('span','git-diff-prefix',row.line.slice(0,1)));
          appendInline(code,row.line.slice(1),syntax.get(row)?.[row.kind==='delete'?'before':'after'],query,row.inline,highlightBudget);
        }else if(model?.supported===false)appendSearchSyntax(code,row.line,null,query,0,row.line.length,highlightBudget);
        else code.textContent=row.line;
        line.append(left,right,code);fragment.append(line);
      }
    }
    if(query&&!search.matchingRows)fragment.append(el('p','diff-search-empty','No matches in this diff.'));
    diff.replaceChildren(fragment);
    if(!changeNodes[difference])difference=visibleDifferences()[0]??-1;
    markDifference();updateDiffControls();
  }
  function updateDiffLabels(v){
    const file=v.files?.[v.file];
    beforeLabel.textContent=v.kind==='edits'?(file?.group==='Staged'?'HEAD':file?.group==='Untracked'?'New file':'Index'):
      v.parent?`Before · ${v.parent.slice(0,8)}`:'Empty tree';
    afterLabel.textContent=v.kind==='edits'?(file?.group==='Staged'?'Index':'Working tree'):`After · ${((v.allChanges?v.head:v.commit)||'').slice(0,8)}`;
  }
  const handleKeydown=event=>{
    if(event.isComposing||event.defaultPrevented)return;
    if(event.metaKey&&!event.altKey&&!event.ctrlKey&&!event.shiftKey&&event.key?.toLowerCase()==='f'){
      event.preventDefault();findBar.hidden=false;findInput.focus();findInput.select();return;
    }
    if(event.key==='Escape'&&!findBar.hidden){event.preventDefault();closeFind();return;}
    if(!event.altKey&&!event.ctrlKey&&!event.metaKey&&!event.shiftKey&&activeList&&
      ['ArrowUp','ArrowDown','Home','End'].includes(event.key)&&
      [document.body,document.documentElement,root].includes(event.target)){
      activeList.focus({preventScroll:true});activeList.onkeydown(event);return;
    }
    if(!event.altKey||event.ctrlKey||event.metaKey||event.shiftKey)return;
    if(['INPUT','TEXTAREA','SELECT'].includes(event.target?.tagName)||event.target?.isContentEditable)return;
    const control={ArrowUp:previousDiff,ArrowDown:nextDiff,ArrowLeft:previousFile,ArrowRight:nextFile}[event.key];
    if(control){event.preventDefault();if(!control.disabled)control.click();}
  };
  // BODY can retain keyboard focus after a native window opens or diff nodes
  // are replaced, so shortcuts must also work outside the root's descendants.
  document.addEventListener('keydown',handleKeydown);
  const rememberList=event=>{activeList=[commitList,fileList].find(list=>list.contains(event.target))||null;queuedListItem=null;};
  document.addEventListener('pointerdown',rememberList);
  const emptyDetail=el('div','git-detail-empty');
  content.append(diffHeader,diffToolbar,findBar,partTabs,note,sides,diff,emptyDetail);
  sidebar.append(commits);detailPanes.append(files,content);inspection.append(commitDetail,detailPanes);
  panes.append(sidebar,inspection);root.append(header,error,panes,statusbar);
  const pendingControls=new Map();
  function holdReadControls(loading){
    if(loading){
      if(pendingControls.size)return;
      for(const control of [reviewBranch,branchDiffButton,branchHistoryButton,fullHistoryButton,historyOrder,previousDiff,nextDiff,previousFile,nextFile,unifiedButton,splitButton,whitespaceButton]){
        pendingControls.set(control,control.disabled);
        // Keep the last frame's contrast while still blocking stale requests.
        control.classList.toggle('git-read-pending',!control.disabled);
        control.disabled=true;
      }
    }else{
      for(const [control,disabled] of pendingControls){control.disabled=disabled;control.classList.toggle('git-read-pending',false);}
      pendingControls.clear();
    }
  }
  const render=v=>{
    const changedContext=state?.siteID!==v.siteID||state?.kind!==v.kind;
    if(changedContext){requestedListItem=null;queuedListItem=null;}
    state=v;
    loadIgnorePatterns(v);
    root.setAttribute('aria-busy',String(v.loading));
    commitList.setAttribute('aria-busy',String(v.loading));fileList.setAttribute('aria-busy',String(v.loading));
    renderEditor(v);
    // A row selection reads asynchronously. Keep the complete current frame,
    // including toolbar contrast, until its replacement is ready.
    const holdFrame=v.loading&&renderedState?.siteID===v.siteID&&renderedState?.kind===v.kind;
    holdReadControls(holdFrame);
    if(holdFrame)return;
    historyOrder.value=v.oldestFirst?'oldest':'newest';
    historyOrder.hidden=v.kind!=='branch'||!v.branchOnly;
    historyOrder.disabled=!!v.loading||v.kind!=='branch'||!v.branchOnly;
    reviewBranch.disabled=!!v.loading||!v.head;
    updateDiffControls();
    branchSummary.hidden=v.kind!=='branch';
    branchSummary.title=branchHistoryContext(v);
    branchSummary.classList.toggle('selected',!!v.allChanges);
    branchDiffButton.textContent=Number.isInteger(v.branchCommits)?`All ${v.branchCommits.toLocaleString()} ${v.branchCommits===1?'commit':'commits'}`:'All branch changes';
    branchDiffButton.disabled=v.loading||!v.branchDiff?.base;
    branchDiffButton.classList.toggle('selected',!!v.allChanges);branchDiffButton.setAttribute('aria-pressed',String(!!v.allChanges));
    branchDiffButton.title=v.branchDiff?.note||`Compare committed branch changes from the common ancestor with ${v.branchDiff?.target||'the base branch'}`;
    historyScope.hidden=v.kind!=='branch';
    for(const [button,selected] of [[branchHistoryButton,!!v.branchOnly],[fullHistoryButton,!v.branchOnly]]) {
      button.disabled=!!v.loading;
      button.classList.toggle('selected',selected);button.setAttribute('aria-pressed',String(selected));
    }
    commitList.setAttribute('aria-label',v.branchOnly?'Branch commits':'Full history');
    // Editor discovery can emit this version again without changing the view.
    if(!v.loading&&renderedVersion===v.version)return;
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
    updateBranchButton();
    refresh.disabled=v.loading;
    const nextOverviewKey=JSON.stringify([v.siteID,v.kind,v.name,v.path,v.branch,v.branchOnly,v.oldestFirst,v.allChanges,v.branchCommits,v.branchDiff,v.counts,v.incomingCount,v.upstream,v.tracking,v.comparisonOnly,v.branchWork,v.rebased,v.captured,v.remoteNote,v.historyNote,v.error,!!v.loading,!!v.ignoreWhitespace]);
    if(overviewKey!==nextOverviewKey){
      overviewKey=nextOverviewKey;
      name.textContent=v.name||'Changes';name.title=v.path;
      branch.textContent=v.branch||'';branch.title=v.branch||'';
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
      error.hidden=!v.error;error.textContent=v.error||'';
      const capture=v.captured?new Date(v.captured).toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit'}):'';
      context.replaceChildren();
      context.hidden=edits||!!v.branchOnly;
      if(!context.hidden){
        if(v.loading)context.append(el('span','muted','Loading history…'));
        else {
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
        !v.loading&&capture?`Read at ${capture}`:'',v.ignoreWhitespace?'Whitespace ignored':'',v.comparisonOnly?'No upstream configured':'',v.remoteNote,v.historyNote,
        v.rebased?'Rebased history: equivalent changes can have different commit IDs.':v.counts?.local>0&&v.incomingCount>0?'History differs: remote-only commits may be older versions after a rebase.':'',
        v.branchWork?.inherited?`${v.branchWork.inherited} additional commits already on ${v.branchWork.base}`:''].filter(Boolean).join(' · ');
    }
    // Keep scroll and text selection intact for duplicate/loading-only emissions.
    if(v.loading)return;
    const allRows=edits?changeFileRows(v.files||[],v.file):(v.files||[]).map((file,index)=>({path:file.path,file,index,selected:index===v.file,parts:[{file,index}]}));
    const rows=allRows.filter(row=>!fileHidden(row.file));
    // Reads select the first file in Git order. When that is ignored, keep the
    // current frame and select the first displayed file instead of painting it.
    // Request once per read; repeated emissions of the same version wait for it.
    if(v.files?.[v.file]&&fileHidden(v.files[v.file])&&rows.length&&!v.error){
      if(ignoredRedirect!==v.version){ignoredRedirect=v.version;send('file',{file:rows[0].index});}
      return;
    }
    renderedVersion=v.version;
    const previousState=renderedState;
    const sameList=previousState?.siteID===v.siteID&&previousState?.kind===v.kind&&!!previousState?.branchOnly===!!v.branchOnly&&previousState?.page===v.page;
    const commitScroll=sameList?commitList.scrollTop:0;
    const fileScroll=sameList&&renderedState?.commit===v.commit?fileList.scrollTop:0;
    const sameDiff=sameDiffSelection(previousState,v);
    if(!sameDiff)findScroll=null;
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
        const item=button('',()=>selectListItem(commitList,item,'commit',{commit:commit.id}),'git-commit');
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
    const hiddenRows=allRows.length-rows.length;
    summaryHeading(fileHeading,(v.files||[]).filter(file=>!fileHidden(file)),null,v.filesLimited,hiddenRows);
    ignoreSummary.textContent=`Ignored files · ${hiddenRows} hidden`;
    fileRows=rows;
    const nextFileRowsKey=JSON.stringify([v.siteID,v.kind,v.commit,v.allChanges,v.files,rows.map(row=>[row.path,row.file.status]),v.filesLimited,!!v.error]);
    if(fileRowsKey!==nextFileRowsKey){
      fileRowsKey=nextFileRowsKey;
      fileList.replaceChildren();
      for(const row of rows) {
        const {file,index}=row;
        const item=button('',()=>selectListItem(fileList,item,'file',{file:index}),'git-file');
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
      if(!rows.length&&hiddenRows)fileList.append(el('p','git-empty',`All ${hiddenRows===1?'changed file is':`${hiddenRows} changed files are`} hidden. Open Ignored files above to show them.`));
      if(!(v.files||[]).length)fileList.append(el('p','git-empty',v.error?'File list unavailable.':v.kind==='edits'?'No uncommitted changes.':v.allChanges?'No branch changes.':v.commit?'This commit has no changed files.':'Select a commit.'));
      if(v.filesLimited)fileList.append(el('p','git-empty','Showing the first 2,000 files.'));
    }
    fileList.removeAttribute('aria-activedescendant');
    fileList.querySelectorAll('button').forEach((item,index)=>{
      const row=rows[index];
      listOption(fileList,item,item.id,row.selected);
      item.onclick=()=>selectListItem(fileList,item,'file',{file:row.index});
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
    const selectedFile=v.files?.[v.file],ignoredSelection=!!selectedFile&&fileHidden(selectedFile);
    const file=ignoredSelection?null:selectedFile;
    emptyDetail.hidden=!!file;diffHeader.hidden=!file;diff.hidden=!file;
    if(ignoredSelection){
      emptyDetail.replaceChildren(el('strong','','All changed files are ignored'),el('p','','Open Ignored files above the file list and choose Show ignored files, or edit the patterns.'));
    }else if(!file){
      const clean=edits&&v.counts?.edits===0&&!v.error;
      emptyDetail.replaceChildren(el('strong','',v.error?'Changes unavailable':clean?'No uncommitted changes':edits?'No file selected':v.allChanges?'No branch changes':v.commit?'No file changes':'No commit selected'),
        el('p','',v.error?'Review the error above, then refresh to try again.':clean?'Your working tree is clean. Open History to review commits.':edits?'Select a file to inspect its changes.':v.allChanges?'The branch matches its common ancestor. Select a commit to inspect its changes.':v.commit?'This commit does not change any files.':'Select a commit from the history to inspect its changes.'));
    }
    diffTitle.textContent=file?(file.oldPath?`${file.oldPath} → ${file.path}`:file.path):'Select a file to view its diff';
    diffTitle.title=diffTitle.textContent;
    note.hidden=!v.diffNote||!file;note.textContent=v.diffNote||'';
    if(!sameDiff||previousState?.diff!==v.diff){
      model=splitDiff(v.diff||'');
      difference=model.changes?(sameDiff?Math.max(0,Math.min(difference,model.changes-1)):0):-1;
      paintDiff(v);
    }
    updateDiffControls();
    updateDiffLabels(v);
    diff.scrollTop=diffScroll.top;diff.scrollLeft=diffScroll.left;
    const edge=pendingEdge;pendingEdge=null;
    if(edge&&edge.file===v.file&&!sameDiff){
      const visible=visibleDifferences();
      difference=(edge.edge==='last'?visible.at(-1):visible[0])??-1;
      markDifference();updateDiffControls();
      changeNodes[difference]?.scrollIntoView({block:'center',inline:'nearest'});
    }
    const queued=queuedListItem;requestedListItem=null;queuedListItem=null;
    // Use the completed read's version; discard queued rows replaced by a
    // different history/page or file list, and stop on a failed read.
    if(queued&&!v.error&&root.contains(queued.item)&&
      (queued.list.contains(document.activeElement)||[document.body,document.documentElement,root].includes(document.activeElement)&&activeList===queued.list))queued.item.click();
  };
  render.destroy=()=>{document.removeEventListener('keydown',handleKeydown);document.removeEventListener('pointerdown',rememberList);};
  return render;
}
