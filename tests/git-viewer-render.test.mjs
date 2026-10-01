import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';

const editorSource=(await readFile(new URL('../cmd/devjay/frontend/file-editor.js',import.meta.url),'utf8')).replace(/^export /gm,'');
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
    insertBefore(node,next){node.remove();node.parent=this;const index=this.children.indexOf(next);this.children.splice(index<0?this.children.length:index,0,node);}
    remove(){if(this.parent){const nodes=this.parent.children;nodes.splice(nodes.indexOf(this),1);this.parent=null;}}
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
    click(){if(!this.disabled)this.onclick?.();}
    scrollIntoView(){this.revealed=(this.revealed||0)+1;}
  }
  const body=new Element('body'),root=new Element('main');
  document={body,activeElement:body,createElement:tag=>new Element(tag),createDocumentFragment:()=>new Element('fragment')};
  body.append(root);
  const context=vm.createContext({document});
  vm.runInContext(editorSource+'\n'+viewerSource,context);
  const requests=[];
  return {root,document,requests,render:context.mountGitViewer(root,request=>requests.push(request),null,openBranches)};
}

const file=(path='selected.go')=>({path,group:'Unstaged',status:'M',statsKnown:true,added:1,removed:1});
const state=overrides=>({siteID:'one',name:'one',kind:'edits',version:1,loading:false,branch:'feature',page:0,
  commit:'',commits:[],files:[file()],file:0,counts:{edits:1,local:1,remote:5},
  diff:'@@ -1 +1 @@\n-old\n+selected contents',...overrides});

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
  const view=viewer(siteID=>{calls.push(siteID);return new Promise((resolve,reject)=>{finish=reject;});});
  view.render(state({head:'head'}));
  const button=view.root.querySelector('.git-switch-branch');
  assert.equal(button.hidden,false);assert.equal(button.attrs['aria-haspopup'],'dialog');
  const opening=button.onclick();assert.equal(button.disabled,true);
  view.render(state({head:'head',version:2}));assert.equal(button.disabled,true);
  await button.onclick();assert.deepEqual(calls,['one']);
  finish(new Error('Bridge unavailable'));await assert.rejects(opening,/Bridge unavailable/);
  assert.equal(button.disabled,false);
  view.render(state({head:'head',loading:true}));
  await button.onclick();assert.deepEqual(calls,['one']); // Guard loading without flashing the toolbar.
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
 const event={key,preventDefault(){this.defaultPrevented=true;},...modifiers};
 target.onkeydown?.(event);return !!event.defaultPrevented;
}

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
 const refresh=view.root.querySelector('.git-view-header').querySelectorAll('button').at(-1);
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
