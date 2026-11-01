import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';

const editorSource=(await readFile(new URL('../cmd/devjay/frontend/file-editor.js',import.meta.url),'utf8')).replace(/^export /gm,'');
const syntaxSource=(await readFile(new URL('../cmd/devjay/frontend/syntax-highlight.js',import.meta.url),'utf8')).replace(/^export /gm,'');
const searchSource=(await readFile(new URL('../cmd/devjay/frontend/diff-search.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'').replace(/^export /gm,'');
const viewerSource=(await readFile(new URL('../cmd/devjay/frontend/git-viewer.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'').replace(/^export /gm,'');

// Mount the production renderer. Fragments, focus and scroll are represented
// here so assertions cover updates to the actual diff DOM, not just helpers.
function viewer(openBranches) {
  let document;
  class Element {
    constructor(tag) {
      this.tag=tag;this.children=[];this.dataset={};this.className='';this.attrs={};this.scrollTop=0;this.scrollLeft=0;
      this.classList={contains:c=>this.className.split(' ').includes(c),add:c=>this.classList.toggle(c,true),
        toggle:(c,on)=>{const classes=new Set(this.className.split(' ').filter(Boolean));on?classes.add(c):classes.delete(c);this.className=[...classes].join(' ');}};
    }
    append(...nodes) {
      for(const node of nodes) {
        if(node.tag==='fragment'){this.append(...node.children.slice());continue;}
        node.remove();node.parent=this;this.children.push(node);
      }
    }
    get parentNode(){return this.parent;}
    get tagName(){return this.tag.toUpperCase();}
    insertBefore(node,next){node.remove();node.parent=this;const index=this.children.indexOf(next);this.children.splice(index<0?this.children.length:index,0,node);}
    remove(){if(this.parent){if(this.contains(document.activeElement))document.activeElement=document.body;const nodes=this.parent.children;nodes.splice(nodes.indexOf(this),1);this.parent=null;}}
    replaceChildren(...nodes){if(this.children.some(node=>node.contains(document.activeElement)))document.activeElement=document.body;for(const node of this.children)node.parent=null;this.children=[];this._text='';this.scrollTop=0;this.scrollLeft=0;this.append(...nodes);}
    set textContent(text){this.replaceChildren();this._text=String(text);}
    get textContent(){return (this._text||'')+this.children.map(node=>node.textContent).join('');}
    contains(node){return node===this||this.children.some(child=>child.contains(node));}
    setAttribute(key,value){this.attrs[key]=value;}
    removeAttribute(key){delete this.attrs[key];}
    matches(selector){return selector.startsWith('.')?this.classList.contains(selector.slice(1)):this.tag===selector;}
    querySelectorAll(selector){return this.children.flatMap(child=>[...(child.matches(selector)?[child]:[]),...child.querySelectorAll(selector)]);}
    querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
    focus(){if(document.activeElement!==this){document.activeElement=this;this.onfocus?.();}}
    select(){this.selectionStart=0;this.selectionEnd=(this.value||'').length;}
    click(){if(!this.disabled)this.onclick?.();}
    addEventListener(type,listener){(this.listeners||={})[type]||=new Set();this.listeners[type].add(listener);}
    removeEventListener(type,listener){this.listeners?.[type]?.delete(listener);}
    dispatchEvent(event){event.target??=this;event.currentTarget=this;this['on'+event.type]?.(event);for(const listener of this.listeners?.[event.type]||[])listener(event);if(event.bubbles&&!event.cancelBubble){if(this.parent)this.parent.dispatchEvent(event);else if(this===document.documentElement)document.dispatchEvent(event);}return !event.defaultPrevented;}
    scrollIntoView(){this.revealed=(this.revealed||0)+1;}
  }
  const body=new Element('body'),root=new Element('main'),html=new Element('html'),listeners={};
  document={body,documentElement:html,activeElement:body,createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag),createDocumentFragment:()=>new Element('fragment'),
    addEventListener(type,listener){(listeners[type]||=new Set()).add(listener);},removeEventListener(type,listener){listeners[type]?.delete(listener);},
    dispatchEvent(event){event.target??=this;event.currentTarget=this;for(const listener of listeners[event.type]||[])listener(event);return !event.defaultPrevented;}};
  html.append(body);body.append(root);
  const context=vm.createContext({document});
  vm.runInContext(syntaxSource+'\n'+searchSource+'\n'+editorSource+'\n'+viewerSource,context);
  const requests=[];
  return {root,document,requests,render:context.mountGitViewer(root,request=>requests.push(request),null,openBranches)};
}

const file=(path='selected.go')=>({path,group:'Unstaged',status:'M',statsKnown:true,added:1,removed:1});
const state=overrides=>({siteID:'one',name:'one',kind:'edits',version:1,loading:false,branch:'feature',page:0,
  commit:'',commits:[],files:[file()],file:0,counts:{edits:1,local:1,remote:5},
  diff:'@@ -1 +1 @@\n-old\n+selected contents',...overrides});

test('View diffs opens a dedicated window from the current branch toolbar',()=>{
  const view=viewer();view.render(state({head:'abc123'}));
  const button=view.root.querySelector('.git-review-branch');
  assert.equal(button.disabled,false);button.click();
  assert.equal(view.requests.length,1);
  assert.equal(view.requests[0].operation,'review-open');
  assert.equal(view.requests[0].version,1);
  view.render(state({version:2,loading:true,head:'abc123'}));
  assert.equal(button.disabled,true);
  view.render(state({version:2,head:''}));
  assert.equal(button.disabled,true);
});

test('Git patch syntax preserves literal text, inline changes, and hunk-local state on each side',()=>{
  const view=viewer(),initial=state({diff:'@@ -1 +1 @@\n-/* original comment begins\n+const live = 7;\n@@ -100 +100 @@\n-return "old"\n+return "<script>new</script>"'});view.render(initial);
  assert.ok(view.root.querySelectorAll('.syntax-keyword').some(node=>node.textContent==='return'));
  assert.ok(view.root.querySelectorAll('.syntax-comment').some(node=>node.textContent.includes('original comment')));
  assert.equal(view.root.querySelectorAll('script').length,0);
  const layout=view.root.querySelectorAll('button').find(node=>node.textContent==='Side by side');layout.click();
  const cells=view.root.querySelectorAll('.git-split-cell');
  assert.equal(cells[1].querySelector('.syntax-keyword').textContent,'const');
  assert.equal(cells[2].querySelector('.syntax-keyword').textContent,'return');
  assert.equal(cells[3].querySelector('.git-code').textContent,'return "<script>new</script>"');
  assert.ok(view.root.querySelectorAll('.git-inline-change').some(node=>node.textContent.includes('script')));
  assert.ok(view.root.querySelectorAll('.syntax-string').length);
  assert.equal(view.root.querySelectorAll('script').length,0);
});

test('rapid category changes back to the last rendered category restore the cleared diff',()=>{
  const view=viewer(),initial=state({kind:'remote',commit:'a',commits:[{id:'a',subject:'Selected',date:'2026-01-01'}]});
  view.render(initial);
  const diff=view.root.querySelector('.git-diff');
  assert.match(diff.textContent,/selected contents/);
  view.render({...initial,version:2,kind:'local',loading:true});
  assert.equal(diff.children.length,0);
  view.render({...initial,version:3,loading:true});
  view.render({...initial,version:3});
  assert.match(diff.textContent,/selected contents/);
  assert.ok(diff.children.length>0);
});

test('an unrelated refresh preserves selected diff nodes and both scroll axes',()=>{
  const view=viewer(),initial=state();view.render(initial);
  const diff=view.root.querySelector('.git-diff'),line=diff.children[2];
  diff.scrollTop=160;diff.scrollLeft=45;
  view.render({...initial,version:2,files:[file('earlier.go'),file()],file:1,counts:{edits:2,local:1,remote:5}});
  assert.equal(diff.children[2],line);
  assert.equal(diff.scrollTop,160);assert.equal(diff.scrollLeft,45);
});

test('updated selected-file contents rebuild the diff while preserving scroll',()=>{
  const view=viewer(),initial=state();view.render(initial);
  const diff=view.root.querySelector('.git-diff'),line=diff.children[2];
  diff.scrollTop=160;diff.scrollLeft=45;
  view.render({...initial,version:2,diff:'@@ -1 +1 @@\n-old\n+new contents'});
  assert.notEqual(diff.children[2],line);assert.match(diff.textContent,/new contents/);
  assert.equal(diff.scrollTop,160);assert.equal(diff.scrollLeft,45);
});

test('changing the selected file resets diff scroll even when its contents match',()=>{
  const view=viewer(),initial=state();view.render(initial);
  const diff=view.root.querySelector('.git-diff'),line=diff.children[2];
  diff.scrollTop=160;diff.scrollLeft=45;
  view.render({...initial,version:2,files:[file('other.go')]});
  assert.notEqual(diff.children[2],line);
  assert.equal(diff.scrollTop,0);assert.equal(diff.scrollLeft,0);
});

test('background updates preserve expanded commit messages until another commit is selected',()=>{
  const view=viewer(),initial=state({kind:'remote',commit:'a',message:'Subject\n\nDetailed commit message',
    commits:[{id:'a',subject:'Subject',date:'2026-01-01'}]});
  view.render(initial);
  const details=view.root.querySelector('.git-commit-detail').querySelector('details');
  details.open=true;
  view.render({...initial,version:2,counts:{edits:2,local:1,remote:5},files:[file('another.go')]});
  assert.equal(view.root.querySelector('.git-commit-detail').querySelector('details'),details);
  assert.equal(details.open,true);assert.match(details.textContent,/Detailed commit message/);
  view.render({...initial,version:3,commit:'b',message:'Another commit',commits:[{id:'b',subject:'Another commit',date:'2026-01-02'}]});
  const next=view.root.querySelector('.git-commit-detail').querySelector('details');
  assert.notEqual(next,details);assert.equal(!!next.open,false);assert.match(next.textContent,/Another commit/);
});

test('Changes window opens branch selection for its Site and restores the button after a failed request',async()=>{
  let finish;const calls=[];
  const view=viewer((siteID,anchor)=>{calls.push([siteID,JSON.parse(JSON.stringify(anchor))]);return new Promise((resolve,reject)=>{finish=reject;});});
  view.render(state({head:'head'}));
  const button=view.root.querySelector('.git-switch-branch');
  button.getBoundingClientRect=()=>({left:235,top:28,width:72,height:26});
  const expected=[['one',{x:235,y:28,width:72,height:26}]];
  assert.equal(button.hidden,false);assert.equal(button.attrs['aria-haspopup'],'dialog');
  const opening=button.onclick();assert.equal(button.disabled,true);
  view.render(state({head:'head',version:2}));assert.equal(button.disabled,true);
  await button.onclick();assert.deepEqual(calls,expected);
  finish(new Error('Bridge unavailable'));await assert.rejects(opening,/Bridge unavailable/);
  assert.equal(button.disabled,false);
  view.render(state({head:'head',loading:true}));
  await button.onclick();assert.deepEqual(calls,expected); // Guard loading without flashing the toolbar.
  for(const change of [{head:''},{head:'head',branch:'Detached HEAD'},{head:'head',branch:'(detached)'},{head:'head',branch:''}]){
   view.render(state(change));assert.equal(button.hidden,true);
  }
});

test('branch view presents one history with explicit markers and subdued incoming commits',()=>{
 const view=viewer();
 view.render(state({kind:'branch',upstream:'remote',tracking:'origin/main',incomingCount:1,counts:{edits:1,local:1,branch:3},commits:[
  {id:'incoming',subject:'Other work',presence:'incoming'},
  {id:'outgoing',subject:'My work',presence:'outgoing'},
  {id:'shared',subject:'Shared base',presence:'shared'},
 ]}));
 const tabs=view.root.querySelector('.git-view-tabs');
 assert.equal(tabs.children.length,2);
 assert.match(tabs.textContent,/Changes1History3/);
 assert.doesNotMatch(tabs.textContent,/Local commits|Remote commits/);
 assert.match(view.root.querySelector('.git-view-context').textContent,/History differs.*1 local only.*1 remote only/);
 const commits=view.root.querySelectorAll('.git-commit');
 assert.ok(commits[0].classList.contains('incoming'));assert.match(commits[0].textContent,/↓ Remote only/);
 assert.match(commits[1].textContent,/↑ Local only/);assert.doesNotMatch(commits[2].textContent,/On remote/);assert.match(commits[2].attrs['aria-label'],/On remote/);
 view.render(state({version:2,kind:'branch',comparisonOnly:true,commits:[{id:'outgoing',presence:'outgoing'}]}));
 assert.match(view.root.querySelector('.git-commit').textContent,/Local only/);
 assert.doesNotMatch(view.root.querySelector('.git-commit').textContent,/Not pushed/);
});

test('Changes has one selectable row per path and keeps staged and working diffs distinct',()=>{
 const view=viewer();
 const staged={...file('src/shared.go'),group:'Staged'},working={...staged,group:'Unstaged'};
 const initial=state({files:[staged,file('z.go'),working,{...file('new.go'),group:'Untracked',status:'?'}],file:2,counts:{edits:3,branch:5}});
 view.render(initial);
 const sidebar=view.root.querySelector('.git-view-sidebar'),files=view.root.querySelector('.git-view-files');
 assert.equal(files.parentNode,sidebar);
 assert.equal(view.root.querySelectorAll('.git-file').length,3);
 const row=view.root.querySelectorAll('.git-file').find(n=>n.textContent.includes('shared.go'));
 assert.match(row.textContent,/Staged · Working tree/);assert.equal(row.attrs['aria-selected'],'true');
 const parts=view.root.querySelector('.git-change-parts').querySelectorAll('button');
 assert.deepEqual(parts.map(n=>n.textContent),['Staged','Working tree']);
 parts[0].onclick();
 assert.equal(view.requests[0].operation,'file');assert.equal(view.requests[0].file,0);
 view.render({...initial,version:2,file:0,diff:'@@ -1 +1 @@\n-old\n+staged content'});
 assert.equal(view.root.querySelectorAll('.git-file').length,3);
 assert.match(view.root.querySelector('.git-diff').textContent,/staged content/);
 assert.equal(view.root.querySelector('.git-change-parts').querySelectorAll('button')[0].attrs['aria-pressed'],'true');
 const history={...initial,version:3,kind:'branch',commits:[{id:'commit',subject:'Update',presence:'shared'}],commit:'commit',message:'Update',files:[{...file('src/shared.go'),group:'Commit'}],file:0};
 view.render(history);
 assert.equal(files.parentNode,view.root.querySelector('.git-detail-panes'));
 assert.equal(view.root.querySelector('.git-change-parts').hidden,true);
 view.render({...initial,version:4});
 assert.equal(files.parentNode,sidebar);assert.equal(view.root.querySelectorAll('.git-file').length,3);
});

test('Changes and History navigation sends only a mode request and clean changes show an empty list',()=>{
 const view=viewer();view.render(state({files:[],file:-1,counts:{edits:0,branch:4}}));
 const tabs=view.root.querySelector('.git-view-tabs').querySelectorAll('button');
 assert.equal(tabs[1].querySelector('.git-tab-count').hidden,true);
 tabs[1].onclick();
 assert.equal(view.requests[0].operation,'mode');assert.equal(view.requests[0].kind,'branch');
 assert.match(view.root.querySelector('.git-view-files').textContent,/No uncommitted changes/);
 assert.equal(view.root.querySelector('.git-change-parts').hidden,true);
 view.render(state({version:2,files:[],file:-1,counts:null,error:'Checkout unavailable'}));
 assert.match(view.root.querySelector('.git-detail-empty').textContent,/Changes unavailable/);
 assert.doesNotMatch(view.root.querySelector('.git-detail-empty').textContent,/working tree is clean/);
});

function key(target,key,modifiers={}) {
 const event={type:'keydown',key,bubbles:true,cancelable:true,preventDefault(){this.defaultPrevented=true;},stopPropagation(){this.cancelBubble=true;},...modifiers};
 target.dispatchEvent(event);return !!event.defaultPrevented;
}

const control=(view,label)=>view.root.querySelectorAll('button').find(node=>node.attrs['aria-label']?.startsWith(label));
const layoutButton=(view,label)=>view.root.querySelector('.git-diff-layout').querySelectorAll('button').find(node=>node.textContent===label);

test('side-by-side keeps commits left, aligns old/new rows and preserves navigation across refreshes',()=>{
 const view=viewer(),initial=state({kind:'branch',commit:'commit',parent:'parent',commits:[{id:'commit',subject:'Change'}],
  diff:'@@ -1,4 +1,4 @@\n-old\n+new\n same\n-deleted\n+<img src=x onerror=alert(1)>\n same'});
 view.render(initial);layoutButton(view,'Side by side').click();
 const diff=view.root.querySelector('.git-diff');
 assert.equal(diff.classList.contains('side-by-side'),true);
 assert.equal(view.root.querySelector('.git-view-files').parentNode,view.root.querySelector('.git-view-sidebar'));
 assert.equal(view.root.querySelector('.git-view-commits').parentNode,view.root.querySelector('.git-view-sidebar'));
 assert.match(diff.textContent,/<img src=x onerror=alert\(1\)>/);assert.equal(diff.querySelectorAll('img').length,0);
 const rows=diff.querySelectorAll('.git-split-line');
 assert.equal(rows[0].children.length,2);assert.equal(rows[0].children[0].querySelector('.git-code').textContent,'old');
 assert.equal(rows[0].children[1].querySelector('.git-code').textContent,'new');
 assert.equal(control(view,'Previous difference').disabled,true);
 control(view,'Next difference').click();
 assert.equal(rows[2].revealed,1);assert.equal(rows[2].classList.contains('current-difference'),true);
 assert.equal(control(view,'Next difference').disabled,true);
 diff.scrollTop=120;view.render({...initial,version:2});
 assert.equal(diff.querySelectorAll('.git-split-line')[2],rows[2]);assert.equal(diff.scrollTop,120);
 assert.match(view.root.querySelector('.git-diff-position').textContent,/2 \/ 2/);
 layoutButton(view,'Unified').click();
 assert.equal(diff.classList.contains('side-by-side'),false);
 assert.match(view.root.querySelector('.git-diff-position').textContent,/2 \/ 2/);
 assert.equal(view.root.querySelector('.git-view-files').parentNode,view.root.querySelector('.git-detail-panes'));
});

test('file toolbar follows displayed paths, guards loading and keyboard modifiers, and resets difference selection',()=>{
 const view=viewer(),initial=state({files:[file('z.go'),file('a.go'),{...file('a.go'),group:'Staged'}],file:1,
  diff:'@@ -1,3 +1,3 @@\n-old\n+new\n same\n-old\n+new'});
 view.render(initial);
 assert.equal(control(view,'Previous file').disabled,true);assert.equal(control(view,'Next file').disabled,false);
 assert.match(view.root.querySelector('.git-file-position').textContent,/1 \/ 2 files/);
 assert.equal(key(view.root,'ArrowDown',{altKey:true}),true);
 assert.match(view.root.querySelector('.git-diff-position').textContent,/2 \/ 2/);
 assert.equal(key(view.root,'ArrowRight',{altKey:true,metaKey:true}),false);
 assert.equal(key(view.root,'ArrowRight',{altKey:true,target:{tagName:'INPUT'}}),false);
 key(view.root,'ArrowRight',{altKey:true});
 assert.equal(view.requests.at(-1).file,0);assert.equal(view.requests.at(-1).version,1);
 view.render({...initial,version:2,loading:true});
 assert.equal(control(view,'Next file').disabled,true);control(view,'Next file').click();
 key(view.root,'ArrowLeft',{altKey:true});assert.equal(view.requests.length,1);
 view.render({...initial,version:2,file:0});
 assert.match(view.root.querySelector('.git-diff-position').textContent,/1 \/ 2/);
 assert.equal(control(view,'Next file').disabled,true);control(view,'Previous file').click();assert.equal(view.requests.at(-1).file,1);
});

test('branch overview opens split layout, retains individual commit selection and handles empty or unavailable comparisons',()=>{
 const view=viewer(),initial=state({kind:'branch',head:'head',commit:'commit',commits:[{id:'commit',subject:'Change'}],branchDiff:{base:'base',target:'origin/develop'}});
 view.render(initial);view.root.querySelector('.git-branch-diff').click();
 assert.equal(view.requests.at(-1).operation,'branch-diff');
 assert.equal(view.root.querySelector('.git-diff').classList.contains('side-by-side'),true);
 view.render({...initial,version:2,allChanges:true,commit:'',parent:'base'});
 assert.equal(view.root.querySelector('.git-branch-diff').attrs['aria-pressed'],'true');
 assert.equal(view.root.querySelector('.git-commit').attrs['aria-selected'],'false');
 assert.match(view.root.querySelector('.git-commit-detail').textContent,/All branch changes.*origin\/develop.*Committed changes only/);
 view.root.querySelector('.git-commit').click();assert.equal(view.requests.at(-1).commit,'commit');
 view.render({...initial,version:3,allChanges:true,commit:'',files:[],file:-1,diff:''});
 assert.match(view.root.querySelector('.git-detail-empty').textContent,/No branch changes/);
 assert.equal(control(view,'Next difference').disabled,true);assert.equal(control(view,'Next file').disabled,true);
 view.render({...initial,version:4,branchDiff:{note:'Base unavailable'}});
 assert.equal(view.root.querySelector('.git-branch-diff').disabled,true);
 assert.equal(view.root.querySelector('.git-branch-diff').title,'Base unavailable');
});

test('History offers independent branch and full history scopes for aggregate and individual diffs',()=>{
 const view=viewer(),initial=state({kind:'branch',head:'head',commit:'',allChanges:true,branchOnly:true,branchCommits:2,
  branchDiff:{base:'base',target:'origin/develop'},commits:[{id:'local',subject:'Local branch work',presence:'outgoing'},{id:'published',subject:'Published branch work',presence:'shared'}]});
 view.render(initial);
 assert.match(view.root.querySelector('.git-view-context').textContent,/2 branch commits.*common ancestor with origin\/develop/);
 assert.match(view.root.querySelector('.git-view-context').title,/Includes published commits; excludes shared ancestors and remote-only/);
 assert.equal(view.root.querySelector('.git-history-divider'),null);
 assert.equal(view.root.querySelector('.git-view-commits').querySelector('.git-scroll-list').attrs['aria-label'],'Branch commits');
 const scope=view.root.querySelector('.git-history-scope'),history=view.root.querySelector('.git-full-history'),branch=view.root.querySelector('.git-branch-history');
 assert.equal(scope.hidden,false);assert.equal(branch.attrs['aria-pressed'],'true');assert.equal(history.attrs['aria-pressed'],'false');
 view.root.querySelectorAll('.git-commit')[1].click();assert.equal(view.requests.at(-1).commit,'published');
 view.render({...initial,version:2,allChanges:false,commit:'published'});
 assert.match(view.root.querySelector('.git-view-context').textContent,/2 branch commits/);
 assert.equal(view.root.querySelectorAll('.git-commit').length,2);assert.equal(scope.hidden,false);
 history.click();assert.equal(view.requests.at(-1).operation,'history-scope');assert.equal(view.requests.at(-1).scope,'full');
 view.render({...initial,version:3,branchOnly:false,allChanges:false,commit:'published'});
 assert.equal(scope.hidden,false);assert.equal(history.attrs['aria-pressed'],'true');assert.equal(branch.attrs['aria-pressed'],'false');
 assert.equal(view.root.querySelector('.git-view-commits').querySelector('.git-scroll-list').attrs['aria-label'],'Full history');
 assert.ok(view.root.querySelector('.git-history-divider'));
 branch.click();assert.equal(view.requests.at(-1).operation,'history-scope');assert.equal(view.requests.at(-1).scope,'branch');
 view.render({...initial,version:4,allChanges:false,commit:'local'});
 assert.equal(branch.attrs['aria-pressed'],'true');assert.equal(view.root.querySelector('.git-branch-diff').attrs['aria-pressed'],'false');
 view.render({...initial,version:5,loading:true});assert.equal(branch.disabled,true);assert.equal(history.disabled,true);
 const requests=view.requests.length;history.click();assert.equal(view.requests.length,requests);
 view.render(state({version:6,kind:'edits'}));assert.equal(scope.hidden,true);
});

test('empty or unavailable branch ranges never imply a full repository list',()=>{
 const view=viewer(),initial=state({kind:'branch',allChanges:true,branchOnly:true,branchCommits:0,
  branchDiff:{base:'base',target:'origin/main'},commits:[],files:[],file:-1,diff:''});
 view.render(initial);
 assert.match(view.root.querySelector('.git-view-context').textContent,/0 branch commits/);
 assert.match(view.root.querySelector('.git-view-commits').textContent,/No commits added since the branch base/);
 view.render({...initial,version:2,branchCommits:null,error:'Comparison unavailable',branchDiff:{note:'No base'}});
 assert.doesNotMatch(view.root.querySelector('.git-view-context').textContent,/0 branch commits/);
 assert.match(view.root.querySelector('.git-view-commits').textContent,/Branch commits unavailable/);
 assert.equal(view.root.querySelector('.git-full-history').disabled,false);
});

test('combined conflicts fall back to readable unified text without navigating invented differences',()=>{
 const view=viewer(),initial=state();view.render(initial);layoutButton(view,'Side by side').click();
 view.render({...initial,version:2,diff:'@@@ -1 -1 +1 @@@\n++<conflict>'});
 assert.equal(view.root.querySelector('.git-diff').classList.contains('side-by-side'),false);
 assert.equal(layoutButton(view,'Side by side').disabled,true);
 assert.match(view.root.querySelector('.git-diff').textContent,/\+\+<conflict>/);
 assert.equal(control(view,'Next difference').disabled,true);
});

test('split view updates revision labels without replacing unchanged lines and retains meaningful metadata',()=>{
 const view=viewer(),initial=state({kind:'branch',allChanges:true,head:'head-one',parent:'base-one',branchDiff:{target:'origin/main',base:'base-one'},
  diff:'diff --git a/old b/new\nindex 123..456 100644\n--- a/old\n+++ b/new\nrename from old\nrename to new\n@@ -1 +1 @@\n-old\n+new'});
 view.render(initial);layoutButton(view,'Side by side').click();
 const diff=view.root.querySelector('.git-diff'),row=diff.querySelector('.git-split-line');
 assert.doesNotMatch(diff.textContent,/diff --git|index 123|--- a\/old|\+\+\+ b\/new/);
 assert.match(diff.textContent,/rename from old.*rename to new/);
 view.render({...initial,version:2,head:'head-two',parent:'base-two'});
 assert.equal(diff.querySelector('.git-split-line'),row);
 assert.match(view.root.querySelector('.git-diff-sides').textContent,/base-two.*head-two/);
 assert.match(view.root.querySelector('.git-view-status').textContent,/common ancestor with origin\/main/);
});

test('arrow selection keeps history rows and surrounding text steady while the diff loads',()=>{
 const view=viewer(),initial=state({kind:'branch',commit:'a',message:'First commit',tracking:'origin/main',incomingCount:0,
  commits:[{id:'a',subject:'First commit',presence:'shared'},{id:'b',subject:'Next commit',presence:'shared'}]});
 view.render(initial);
 const list=view.root.querySelector('.git-view-commits').querySelector('.git-scroll-list');
 const rows=list.querySelectorAll('button'),context=view.root.querySelector('.git-view-context'),status=view.root.querySelector('.git-view-status');
 const contextNode=context.children[0],statusText=status.textContent,diff=view.root.querySelector('.git-diff'),line=diff.children[0];
 list.scrollTop=80;
 key(list,'ArrowDown');view.render({...initial,version:2,loading:true,editor:null});
 assert.equal(context.children[0],contextNode);assert.equal(status.textContent,statusText);assert.equal(diff.children[0],line);
 assert.deepEqual(list.querySelectorAll('button'),rows);assert.equal(list.scrollTop,80);
 view.render({...initial,version:2,commit:'b',message:'Next commit',diff:'@@ -1 +1 @@\n-new commit'});
 assert.deepEqual(list.querySelectorAll('button'),rows);assert.equal(rows[1].attrs['aria-selected'],'true');
 assert.equal(rows[0].attrs['aria-selected'],'false');assert.equal(list.scrollTop,80);
 assert.match(view.root.querySelector('.git-commit-detail').textContent,/Next commit/);
 assert.match(diff.textContent,/new commit/);
 // An actual history change still replaces the rows and updates their labels.
 view.render({...initial,version:3,commits:[{id:'new',subject:'New remote commit',presence:'incoming'},...initial.commits]});
 assert.notEqual(list.querySelectorAll('button')[0],rows[0]);assert.match(list.textContent,/New remote commit/);
});

test('file selection reuses rows while preserving staged and working selection intents',()=>{
 const view=viewer(),initial=state({files:[{...file('a.go'),group:'Staged'},file('a.go'),file('b.go')],file:0});
 view.render(initial);
 const list=view.root.querySelector('.git-view-files').querySelector('.git-scroll-list'),rows=list.querySelectorAll('button');
 view.render({...initial,version:2,loading:true});view.render({...initial,version:2,file:1});
 assert.deepEqual(list.querySelectorAll('button'),rows);rows[0].click();assert.equal(view.requests.at(-1).file,1);
 view.render({...initial,version:3,file:0});rows[0].click();assert.equal(view.requests.at(-1).file,0);
 view.render({...initial,version:4,file:2});key(list,'ArrowUp');assert.equal(view.requests.at(-1).file,1);
});

test('history arrows select across incoming/shared rows, retain focus through loading, and reveal the selection',()=>{
 const view=viewer(),initial=state({kind:'branch',commit:'incoming',commits:[
  {id:'incoming',subject:'Incoming',presence:'incoming'},
  {id:'shared',subject:'Shared',presence:'shared'},
  {id:'older',subject:'Older',presence:'shared'},
 ]});
 view.render(initial);
 const list=view.root.querySelector('.git-view-commits').querySelector('.git-scroll-list');
 assert.equal(view.document.activeElement,list);assert.equal(list.attrs.role,'listbox');
 assert.equal(key(list,'ArrowDown'),true);
 assert.equal(view.requests[0].operation,'commit');assert.equal(view.requests[0].commit,'shared');assert.equal(view.requests[0].version,1);
 view.render({...initial,version:2,loading:true});
 assert.equal(view.document.activeElement,list);assert.notEqual(view.root.querySelector('.git-view-panes').inert,true);
 key(list,'ArrowDown');assert.equal(view.requests.length,1);
 view.render({...initial,version:2,commit:'shared'});
 const selected=list.querySelector('.selected');
 assert.equal(view.document.activeElement,list);assert.equal(list.attrs['aria-activedescendant'],selected.id);
 assert.equal(selected.attrs['aria-selected'],'true');assert.equal(selected.tabIndex,-1);assert.ok(selected.revealed);
 key(list,'ArrowDown');assert.equal(view.requests[1].commit,'older');assert.equal(view.requests[1].version,2);
 view.render({...initial,version:3,commit:'older'});
 key(list,'ArrowDown');assert.equal(view.requests.length,2); // Do not wrap or fetch another page.
 key(list,'ArrowUp');assert.equal(view.requests[2].commit,'shared');
 key(list,'Home');assert.equal(view.requests[3].commit,'incoming');
 view.render({...initial,version:4});
 key(list,'ArrowUp');assert.equal(view.requests.length,4);
 key(list,'End');assert.equal(view.requests[4].commit,'older');
});

test('file arrows follow displayed paths, not backend indices or staged duplicates',()=>{
 const view=viewer(),initial=state({files:[file('z.go'),{...file('a.go'),group:'Staged'},file('a.go'),file('m.go')],file:2});
 view.render(initial);
 const list=view.root.querySelector('.git-view-files').querySelector('.git-scroll-list');
 assert.equal(view.document.activeElement,list);
 assert.equal(list.querySelectorAll('button').length,3);
 key(list,'ArrowDown');assert.equal(view.requests[0].file,3);assert.equal(view.requests[0].operation,'file');
 view.render({...initial,version:2,file:3});
 key(list,'ArrowUp');assert.equal(view.requests[1].file,2); // Working-tree part of the preceding path.
 key(list,'End');assert.equal(view.requests[2].file,0);
 view.render({...initial,version:3,file:0});
 key(list,'Home');assert.equal(view.requests[3].file,2);
 // Clicking a row returns focus from the diff to the list, including on WebKit.
 view.root.querySelector('.git-diff').focus();list.querySelectorAll('button')[1].click();
 assert.equal(view.document.activeElement,list);assert.equal(view.requests.at(-1).file,3);
});

test('list navigation does not steal focus, swallow modified keys, or request reads while loading',()=>{
 const view=viewer(),initial=state({files:[file('a.go'),file('b.go')]});
 const refresh=view.root.querySelector('.git-refresh');
 refresh.focus();view.render(initial);assert.equal(view.document.activeElement,refresh);
 const list=view.root.querySelector('.git-view-files').querySelector('.git-scroll-list');
 for(const modifiers of [{altKey:true},{ctrlKey:true},{metaKey:true},{shiftKey:true},{isComposing:true},{defaultPrevented:true}]){
  const event={key:'ArrowDown',preventDefault(){assert.fail('Modified key intercepted');},...modifiers};
  list.onkeydown(event);
 }
 assert.equal(key(list,'Tab'),false);assert.equal(key(list,'ArrowLeft'),false);
 const diff=view.root.querySelector('.git-diff');diff.focus();assert.equal(key(diff,'ArrowDown'),false);
 view.render({...initial,version:2,loading:true});
 list.querySelectorAll('button')[1].click();view.root.querySelector('.git-view-tabs').querySelectorAll('button')[1].click();
 assert.equal(view.requests.length,0);
 diff.focus();view.render({...initial,version:2});assert.equal(view.document.activeElement,diff);
 list.focus();list.scrollTop=120;const revealed=list.querySelector('.selected').revealed;
 view.render({...initial,version:3});assert.equal(view.document.activeElement,list);assert.equal(list.scrollTop,120);
 assert.equal(list.querySelector('.selected').revealed,revealed); // An unchanged refresh must not move the scroll.
 view.render({...initial,version:4,files:[],file:-1});
 assert.equal(list.attrs['aria-activedescendant'],undefined);key(list,'ArrowDown');key(list,'End');assert.equal(view.requests.length,0);
});

test('history files navigate independently and ArrowDown from a category enters its list',()=>{
 const view=viewer(),initial=state({kind:'branch',commit:'a',commits:[{id:'a'},{id:'b'}],files:[file('a.go'),file('b.go')]});
 view.render(initial);
 const files=view.root.querySelector('.git-view-files').querySelector('.git-scroll-list');files.focus();
 key(files,'ArrowDown');assert.equal(view.requests[0].operation,'file');assert.equal(view.requests[0].file,1);
 view.render({...initial,version:2,file:1});assert.equal(view.document.activeElement,files);
 const tabs=view.root.querySelector('.git-view-tabs').querySelectorAll('button');
 tabs[0].click();assert.equal(view.document.activeElement,tabs[0]);assert.equal(view.requests[1].operation,'mode');
 view.render(state({version:3,loading:true}));view.render(state({version:3}));
 assert.equal(key(tabs[0],'ArrowDown'),true);assert.equal(view.document.activeElement,files);
 tabs[1].click();view.render({...initial,version:4,loading:true});view.render({...initial,version:4});
 key(tabs[1],'ArrowDown');assert.equal(view.document.activeElement,view.root.querySelector('.git-view-commits').querySelector('.git-scroll-list'));
 assert.equal(view.requests.length,3);
});

test('viewer shortcuts navigate from body and descendant focus, survive replacement, and detach on destroy',()=>{
 const view=viewer(),initial=state({files:[file('one.go'),file('two.go')],
  diff:'@@ -1,5 +1,5 @@\n-old one\n+new one\n context\n-old two\n+new two\n context\n-old three\n+new three'});
 view.render(initial);view.document.body.focus();
 const position=()=>view.root.querySelector('.git-diff-position').textContent;
 assert.equal(key(view.document.activeElement,'ArrowDown',{altKey:true}),true);assert.match(position(),/2 \/ 3/);
 const source=view.root.querySelector('.git-code');
 assert.equal(key(source,'ArrowDown',{altKey:true}),true);assert.match(position(),/3 \/ 3/);
 assert.equal(key(source,'ArrowUp',{altKey:true}),true);assert.match(position(),/2 \/ 3/);
 assert.equal(key(view.document.documentElement,'ArrowUp',{altKey:true}),true);assert.match(position(),/1 \/ 3/);
 source.focus();view.render({...initial,version:2,diff:initial.diff.replace('new one','changed one')});
 assert.equal(view.document.activeElement,view.document.body);
 assert.equal(key(view.document.activeElement,'ArrowDown',{altKey:true}),true);assert.match(position(),/2 \/ 3/);
 const requests=view.requests.length;view.render.destroy();
 assert.equal(key(view.document.body,'ArrowRight',{altKey:true}),false);assert.equal(view.requests.length,requests);
});

test('viewer document shortcuts preserve text controls, modified keys, and handled events',()=>{
 const view=viewer();view.render(state({files:[file('one.go'),file('two.go')],diff:'@@ -1,3 +1,3 @@\n-old one\n+new one\n context\n-old two\n+new two'}));
 for(const modifiers of [{shiftKey:true},{ctrlKey:true},{metaKey:true},{isComposing:true}]){
  assert.equal(key(view.document.body,'ArrowRight',{altKey:true,...modifiers}),false);
 }
 for(const tag of ['input','textarea','select','span']){
  const target=view.document.createElement(tag);if(tag==='span')target.isContentEditable=true;view.root.append(target);
  assert.equal(key(target,'ArrowRight',{altKey:true}),false);
 }
 const source=view.root.querySelector('.git-code');source.onkeydown=event=>event.preventDefault();
 assert.equal(key(source,'ArrowDown',{altKey:true}),true);
 assert.match(view.root.querySelector('.git-diff-position').textContent,/1 \/ 2/);
 assert.equal(view.requests.length,0);
});

function searchablePatch(){
 const changes=new Map([
  [10,['const original = "needle original-only";','const current = "replacement";']],
  [30,['old unrelated change','new unrelated change']],
  [50,['const old = "replacement";','const current = "NEEDLE current-only";']],
  [70,['old markup','const markup = "<script>literal</script>";']],
 ]);
 return '@@ -1,80 +1,80 @@\n'+Array.from({length:80},(_,index)=>{
  const change=changes.get(index+1);return change?`-${change[0]}\n+${change[1]}`:` context line ${index+1}`;
 }).join('\n');
}

test('Cmd+F filters either diff side, retains syntax and file navigation, and skips hidden changes',()=>{
 const view=viewer();view.render(state({files:[file('one.go'),file('two.go')],diff:searchablePatch()}));
 const diff=view.root.querySelector('.git-diff');diff.scrollTop=350;
 assert.equal(key(view.document.body,'f',{metaKey:true}),true);
 const input=view.root.querySelector('.diff-find-input'),bar=view.root.querySelector('.diff-find');
 assert.equal(bar.hidden,false);assert.equal(view.document.activeElement,input);
 input.value='needle';input.oninput();
 assert.equal(view.root.querySelector('.diff-find-status').textContent,'2 matching lines');
 assert.match(diff.textContent,/original-only/);assert.match(diff.textContent,/current-only/);assert.match(diff.textContent,/context line 9/);
 assert.doesNotMatch(diff.textContent,/unrelated change|context line 30/);
 assert.equal(view.root.querySelectorAll('.git-file').length,2);
 assert.equal(diff.querySelectorAll('.diff-search-hit').length,2);
 assert.ok(diff.querySelectorAll('.syntax-keyword').length);assert.ok(diff.querySelectorAll('.diff-search-gap').length);
 assert.equal(view.root.querySelector('.current-difference').dataset.change,'0');
 control(view,'Next difference').click();assert.equal(view.root.querySelector('.current-difference').dataset.change,'2');
 assert.match(view.root.querySelector('.git-diff-position').textContent,/3 \/ 4/);
 assert.equal(control(view,'Next difference').disabled,true);
 control(view,'Previous difference').click();assert.equal(view.root.querySelector('.current-difference').dataset.change,'0');
 layoutButton(view,'Side by side').click();
 assert.equal(diff.querySelectorAll('.diff-search-hit').length,2);assert.ok(diff.querySelectorAll('.git-inline-change').length);
 input.value='<script>';input.oninput();
 assert.equal(diff.querySelector('.diff-search-hit').textContent,'<script>');assert.equal(view.root.querySelectorAll('script').length,0);
 input.value='missing term';input.oninput();
 assert.equal(view.root.querySelector('.diff-find-status').textContent,'No matches');
 assert.match(diff.querySelector('.diff-search-empty').textContent,/No matches/);
 assert.equal(control(view,'Previous difference').disabled,true);assert.equal(control(view,'Next difference').disabled,true);
 assert.equal(key(input,'Escape'),true);assert.equal(bar.hidden,true);assert.equal(input.value,'');
 assert.equal(view.document.activeElement,diff);assert.equal(diff.scrollTop,350);
 assert.match(diff.textContent,/unrelated change/);assert.equal(diff.querySelectorAll('.diff-search-hit').length,0);
 assert.equal(view.requests.length,0);
});

test('diff search preserves current query and focus across loading and refreshed source',()=>{
 const view=viewer(),initial=state({files:[file('one.go'),file('two.go')],diff:searchablePatch()});view.render(initial);
 key(view.document.body,'f',{metaKey:true});
 const input=view.root.querySelector('.diff-find-input');input.value='needle';input.oninput();
 assert.equal(key(input,'f',{metaKey:true}),true);assert.equal(input.selectionStart,0);assert.equal(input.selectionEnd,6);
 view.render({...initial,version:2,loading:true});
 input.value='current-only';input.oninput();
 assert.equal(view.root.querySelector('.diff-find-status').textContent,'1 matching line');
 view.render({...initial,version:2,diff:initial.diff.replace('new unrelated change','new current-only change')});
 assert.equal(view.root.querySelector('.diff-find-input'),input);assert.equal(input.value,'current-only');assert.equal(view.document.activeElement,input);
 assert.equal(view.root.querySelector('.diff-find-status').textContent,'2 matching lines');
 view.render({...initial,version:3,file:1,diff:'@@ -1 +1 @@\n-old\n+different file'});
 assert.equal(input.value,'current-only');assert.equal(view.document.activeElement,input);
 assert.equal(view.root.querySelector('.diff-find-status').textContent,'No matches');
 control(view,'Close find').click();assert.match(view.root.querySelector('.git-diff').textContent,/different file/);
 assert.equal(view.requests.length,0);
});

test('find shortcut respects modifiers, composition, and child event handlers',()=>{
 const view=viewer();view.render(state());const bar=view.root.querySelector('.diff-find');
 for(const modifiers of [{ctrlKey:true},{altKey:true},{shiftKey:true},{isComposing:true}]){
  assert.equal(key(view.document.body,'f',{metaKey:true,...modifiers}),false);assert.equal(bar.hidden,true);
 }
 const source=view.root.querySelector('.git-code');source.onkeydown=event=>event.preventDefault();
 assert.equal(key(source,'f',{metaKey:true}),true);assert.equal(bar.hidden,true);
 view.render.destroy();assert.equal(key(view.document.body,'f',{metaKey:true}),false);assert.equal(bar.hidden,true);
});
