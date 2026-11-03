import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
const source = await readFile(new URL('../cmd/devjay/frontend/branch-review.js',import.meta.url),'utf8');
const syntaxSource = await readFile(new URL('../cmd/devjay/frontend/syntax-highlight.js',import.meta.url),'utf8');
const searchSource = await readFile(new URL('../cmd/devjay/frontend/diff-search.js',import.meta.url),'utf8');
const syntaxURL = `data:text/javascript;base64,${Buffer.from(syntaxSource).toString('base64')}`;
const searchURL = `data:text/javascript;base64,${Buffer.from(searchSource.replace('./syntax-highlight.js',syntaxURL)).toString('base64')}`;
const moduleSource = source.replace('./syntax-highlight.js',syntaxURL).replace('./diff-search.js',searchURL);
const {reviewTextLines,reviewDiffModel,reviewVisibleRows,reviewUnifiedRows,reviewCommitSummary,reviewFileMatches,reviewIgnoreMatchers,reviewFileIgnored} = await import(`data:text/javascript;base64,${Buffer.from(moduleSource).toString('base64')}`);

test('full-file model preserves prefix and suffix, beginning/end insertions, deletions and CRLF',()=>{
  const model=reviewDiffModel({oldText:'one\r\ntwo\r\nthree\r\n',newText:'first\r\none\r\nthree\r\nlast\r\n',hunks:[
    {oldStart:0,oldLines:0,newStart:1,newLines:1},
    {oldStart:2,oldLines:1,newStart:2,newLines:0},
    {oldStart:3,oldLines:0,newStart:4,newLines:1},
  ]});
  assert.deepEqual(model.rows.map(row=>[row.kind,row.before?.number,row.before?.text,row.after?.number,row.after?.text]),[
    ['change',undefined,undefined,1,'first'],['context',1,'one',2,'one'],['change',2,'two',undefined,undefined],['context',3,'three',3,'three'],['change',undefined,undefined,4,'last'],
  ]);
  assert.deepEqual(reviewTextLines('one\n'),['one']);assert.deepEqual(reviewTextLines(''),[]);assert.deepEqual(reviewTextLines('\n'),['']);
});
test('whitespace-ignored context retains the actual original and working text',()=>{
  const {rows}=reviewDiffModel({oldText:'  same\nold\n',newText:'same\nnew\n',hunks:[{oldStart:2,oldLines:1,newStart:2,newLines:1}]});
  assert.equal(rows[0].before.text,'  same');assert.equal(rows[0].after.text,'same');assert.equal(rows[0].kind,'context');
});
test('unified replacements group deletions before additions',()=>{
  const {rows}=reviewDiffModel({oldText:'old1\nold2\n',newText:'new1\nnew2\n',hunks:[{oldStart:1,oldLines:2,newStart:1,newLines:2}]});
  assert.deepEqual(reviewUnifiedRows(rows).map(row=>row.before?.text||row.after?.text),['old1','old2','new1','new2']);
});
test('continuous context expansion reveals requested lines without losing change boundaries',()=>{
  const rows=Array.from({length:30},(_,i)=>({kind:i===14?'change':'context',before:{number:i+1,text:'line'}}));
  const collapsed=reviewVisibleRows(rows);
  assert.deepEqual(collapsed.filter(row=>row.kind==='gap').map(row=>[row.from,row.to]),[[0,11],[18,30]]);
  const expanded=reviewVisibleRows(rows,new Set([0,1,2]));
  assert.deepEqual(expanded.filter(row=>row.kind==='gap').map(row=>[row.from,row.to]),[[3,11],[18,30]]);
});
test('provenance states uncommitted work and uncertain deletion history explicitly',()=>{
  const text=reviewCommitSummary({uncommitted:true,commits:[{id:'abcdef12345',subject:'Update',author:'Ada',date:'2026-01-01'}],note:'Deleted-line provenance unavailable.'});
  assert.match(text,/Uncommitted/);assert.match(text,/abcdef12 Update/);assert.match(text,/Ada/);assert.match(text,/Deleted-line provenance unavailable/);
  assert.equal(reviewFileMatches({path:'src/current.go',oldPath:'src/old.go'},'SRC OLD'),true);
  assert.equal(reviewFileMatches({path:'src/current.go'},'src missing'),false);
});

function viewer(storage = new Map()) {
  let document;
  class Element {
    constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.scrollTop=0;this.scrollLeft=0;this.style={setProperty(key,value){this[key]=value;}};this.value='';this.checked=false;this.disabled=false;this.classList={contains:c=>this.className.split(' ').includes(c),add:c=>this.classList.toggle(c,true),toggle:(c,on)=>{const values=new Set(this.className.split(' ').filter(Boolean));on?values.add(c):values.delete(c);this.className=[...values].join(' ');}};}
    append(...nodes){for(const node of nodes){if(node.tag==='fragment'){this.append(...node.children.slice());continue;}node.remove();node.parent=this;this.children.push(node);}}
    remove(){if(this.parent){if(this.contains(document.activeElement))document.activeElement=document.body;this.parent.children.splice(this.parent.children.indexOf(this),1);this.parent=null;}}
    replaceChildren(...nodes){if(this.children.some(node=>node.contains(document.activeElement)))document.activeElement=document.body;this.children.forEach(node=>node.parent=null);this.children=[];this._text='';this.append(...nodes);}
    set textContent(text){this.replaceChildren();this._text=String(text);}
    get textContent(){return (this._text||'')+this.children.map(node=>node.textContent).join('');}
    set value(value){this._value=this.tag==='textarea'?String(value).replace(/\r\n?/g,'\n'):value;}
    get value(){return this._value;}
    get tagName(){return this.tag.toUpperCase();}
    contains(node){return node===this||this.children.some(child=>child.contains(node));}
    setAttribute(key,value){this.attrs[key]=value;}
    querySelectorAll(selector){return this.children.flatMap(node=>[...(selector.startsWith('.')?node.classList.contains(selector.slice(1)):node.tag===selector)?[node]:[],...node.querySelectorAll(selector)]);}
    querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
    focus(){document.activeElement=this;}
    select(){this.selectionStart=0;this.selectionEnd=this.value.length;}
    click(){if(!this.disabled)this.onclick?.();}
    addEventListener(type,listener){(this.listeners||={})[type]||=new Set();this.listeners[type].add(listener);}
    removeEventListener(type,listener){this.listeners?.[type]?.delete(listener);}
    dispatchEvent(event){event.target??=this;event.currentTarget=this;this['on'+event.type]?.(event);for(const listener of this.listeners?.[event.type]||[])listener(event);if(event.bubbles&&!event.cancelBubble){if(this.parent)this.parent.dispatchEvent(event);else if(this===document.documentElement)document.dispatchEvent(event);}return !event.defaultPrevented;}
    scrollIntoView(){this.revealed=true;}
    getBoundingClientRect(){return {top:0,bottom:0,height:0};}
  }
  const root=new Element('main'),body=new Element('body'),html=new Element('html'),listeners={};
  document={body,documentElement:html,activeElement:body,createElement:tag=>new Element(tag),createDocumentFragment:()=>new Element('fragment'),
    addEventListener(type,listener){(listeners[type]||=new Set()).add(listener);},removeEventListener(type,listener){listeners[type]?.delete(listener);},
    dispatchEvent(event){event.target??=this;event.currentTarget=this;for(const listener of listeners[event.type]||[])listener(event);return !event.defaultPrevented;}};
  html.append(body);body.append(root);
  const frames=new Map(),timers=new Map();let nextFrame=0,nextTimer=0,now=0;
  const context=vm.createContext({document,setTimeout:(fn,delay)=>{timers.set(++nextTimer,{fn,at:now+delay});return nextTimer;},clearTimeout:id=>timers.delete(id),requestAnimationFrame:fn=>{frames.set(++nextFrame,fn);return nextFrame;},cancelAnimationFrame:id=>frames.delete(id),localStorage:{setItem:(key,value)=>storage.set(key,value),getItem:key=>storage.has(key)?storage.get(key):null},sessionStorage:{setItem:(key,value)=>storage.set(key,value),getItem:key=>storage.get(key)||null,removeItem:key=>storage.delete(key)}});vm.runInContext(syntaxSource.replace(/^export /gm,'')+'\n'+searchSource.replace(/^import .*;\n/gm,'').replace(/^export /gm,'')+'\n'+source.replace(/^import .*;\n/gm,'').replace(/^export /gm,''),context);
  const requests=[],prefetchRequests=[];const api=context.createBranchReview(root,data=>{(data.operation==='review-prefetch'?prefetchRequests:requests).push(JSON.parse(JSON.stringify(data)));return true;});
  const flushFrames=()=>{const pending=[...frames.values()];frames.clear();for(const fn of pending)fn();};
  return {root,document,requests,prefetchRequests,frames,timers,flushFrames,advance(ms){now+=ms;for(const [id,timer] of [...timers])if(timer.at<=now){timers.delete(id);timer.fn();}},...api,prefetch(next){flushFrames();api.prefetch({prefetchID:prefetchRequests.at(-1)?.prefetchID,...next});}};
}
const commit={id:'abc123456789',subject:'Add greeting',author:'Ada',date:'2026-01-01T12:00:00Z'};
const payload=(path='one.go',overrides={})=>({path,oldText:'prefix\nold\nsuffix\n',newText:'prefix\n<script>literal</script>\nsuffix\n',hunks:[{oldStart:2,oldLines:1,newStart:2,newLines:1,commits:[commit],uncommitted:true}],commits:[commit],editable:true,token:path+'-token',...overrides});
const state=(overrides={})=>({kind:'review',version:1,siteID:'site',path:'/repo',branch:'feature',head:'head',captured:'snapshot1',file:0,files:[{path:'one.go',status:'M',added:1,removed:1,statsKnown:true}],review:{baseRef:'main',base:'base',refs:['main','develop'],ignoreWhitespace:false,selected:payload()},...overrides});
const control=(view,label)=>view.root.querySelectorAll('button').find(node=>node.attrs['aria-label']===label);

test('dedicated view defaults to whole-file side by side and renders source as literal text',()=>{
  const view=viewer();view.render(state());
  assert.equal(view.root.querySelector('.br-code').classList.contains('br-split'),true);
  assert.match(view.root.querySelector('.br-code').textContent,/prefix/);assert.match(view.root.querySelector('.br-code').textContent,/suffix/);
  assert.match(view.root.querySelector('.br-code').textContent,/<script>literal<\/script>/);assert.equal(view.root.querySelectorAll('script').length,0);
  assert.match(view.root.querySelector('.br-commit-panel').textContent,/Add greeting/);assert.match(view.root.querySelector('.br-commit-panel').textContent,/Uncommitted/);
  control(view,'Unified').click();assert.equal(view.root.querySelector('.br-code').classList.contains('br-unified'),true);
});
test('syntax colors preserve source and independent original/current lexical state in every review layout',()=>{
  const oldText='/* original comment\nstill a comment */\nconst name = "old";\n';
  const newText='const live = 7;\n// working comment\nconst name = "<script>literal</script>";\n';
  const data=payload('one.go',{oldText,newText,hunks:[{oldStart:1,oldLines:3,newStart:1,newLines:3,commits:[]} ]});
  const initial=state({review:{...state().review,selected:data}}),view=viewer();view.render(initial);
  const sources=()=>view.root.querySelectorAll('.br-source').map(node=>node.textContent);
  const originalSources=sources();
  assert.deepEqual(originalSources,['/* original comment','const live = 7;','still a comment */','// working comment','const name = "old";','const name = "<script>literal</script>";']);
  const cells=view.root.querySelectorAll('.br-cell');
  assert.match(cells[0].querySelector('.syntax-comment').textContent,/original comment/);
  assert.equal(cells[1].querySelector('.syntax-keyword').textContent,'const');
  assert.match(cells[2].querySelector('.syntax-comment').textContent,/still a comment/);
  assert.equal(view.root.querySelectorAll('script').length,0);
  const option=view.root.querySelectorAll('label').find(node=>node.textContent==='Syntax highlighting').querySelector('input');
  assert.equal(option.checked,true);option.checked=false;option.onchange();
  assert.equal(view.root.querySelectorAll('.syntax-keyword').length,0);assert.deepEqual(sources(),originalSources);
  option.checked=true;option.onchange();control(view,'Unified').click();
  assert.ok(view.root.querySelectorAll('.syntax-keyword').length);assert.ok(view.root.querySelectorAll('.syntax-string').length);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();
  assert.ok(view.root.querySelectorAll('.syntax-keyword').length);assert.equal(view.root.querySelectorAll('script').length,0);
  // Viewing with syntax colors does not alter the original working text used
  // to initialize the editor or the bytes sent by an explicit save.
  control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  assert.equal(view.root.querySelector('.br-edit-text').value,newText);
  control(view,'Save changes to working file (Command+S)').click();assert.equal(view.requests.at(-1).content,newText);
});
test('renamed files detect syntax separately for old and current paths and leave unknown text plain',()=>{
  const data=payload('notes.txt',{oldPath:'before.go',oldText:'func Before() {}\n',newText:'func After() {}\n',hunks:[{oldStart:1,oldLines:1,newStart:1,newLines:1,commits:[]}]});
  const view=viewer();view.render(state({files:[{path:'notes.txt',oldPath:'before.go',status:'R'}],review:{...state().review,selected:data}}));
  const cells=view.root.querySelectorAll('.br-cell');
  assert.equal(cells[0].querySelector('.syntax-keyword').textContent,'func');
  assert.equal(cells[1].querySelector('.syntax-keyword'),null);
  assert.equal(cells[1].querySelector('.br-source').textContent,'func After() {}');
});
test('continuous mode expands context and requests unloaded files sequentially',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();
  assert.equal(view.root.querySelectorAll('.br-file-card').length,2);
  assert.equal(control(view,'Load diff for two.go'),undefined);
  view.flushFrames();assert.equal(view.requests.length,0);assert.equal(view.prefetchRequests.length,1);assert.deepEqual(view.prefetchRequests[0].prefetch,[1]);
  view.prefetch({...initial,review:{...initial.review,selected:payload('two.go')}});
  assert.equal(view.root.querySelectorAll('.br-code').length,2);
});
test('continuous scrolling follows the visible file without rebuilding content or stealing focus',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'},{path:'three.go'}]});view.render(initial);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();
  const content=view.root.querySelector('.br-content'),list=view.root.querySelector('.br-files'),rows=view.root.querySelectorAll('.br-file'),cards=view.root.querySelectorAll('.br-file-card');
  content.clientHeight=400;content.scrollHeight=1460;content.getBoundingClientRect=()=>({top:100,bottom:500,height:400});
  list.getBoundingClientRect=()=>({top:100,bottom:200,height:100});
  const measureRows=()=>view.root.querySelectorAll('.br-file').forEach((row,i)=>{row.getBoundingClientRect=()=>({top:100+i*45-list.scrollTop,bottom:145+i*45-list.scrollTop,height:45});});measureRows();
  cards.forEach((card,i)=>{card.getBoundingClientRect=()=>({top:120+i*620-content.scrollTop,bottom:120+i*620+(i===2?200:600)-content.scrollTop});});
  const scroll=top=>{content.scrollTop=top;content.dispatchEvent({type:'scroll'});view.flushFrames();};
  const position=()=>view.root.querySelectorAll('.br-nav-group')[1].textContent;
  content.focus();scroll(590);assert.equal(position(),'←1 / 3→');
  scroll(620);assert.equal(position(),'←2 / 3→');
  assert.equal(rows[1].attrs['aria-selected'],'true');assert.equal(rows[0].attrs['aria-selected'],'false');
  assert.equal(view.document.activeElement,content);assert.equal(content.scrollTop,620);assert.equal(view.root.querySelectorAll('.br-file-card')[0],cards[0]);
  assert.equal(view.requests.length,0);assert.ok(view.prefetchRequests[0].prefetch.includes(1));
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  assert.equal(content.scrollTop,620);assert.equal(position(),'←2 / 3→');
  view.render({...initial,version:3,loading:true});scroll(1060);assert.equal(position(),'←2 / 3→');
  view.render({...initial,version:4,file:1,review:{...initial.review,selected:payload('two.go')}});view.flushFrames();
  assert.equal(position(),'←3 / 3→');assert.ok(list.scrollTop>0);
  // Explicit navigation to a short file at a clamped scroll position wins
  // over its queued scroll event, until the user actually scrolls again.
  rows[1].click();measureRows();scroll(1060);assert.equal(position(),'←2 / 3→');
  scroll(0);assert.equal(position(),'←1 / 3→');assert.equal(list.scrollTop,0);
  assert.equal(view.document.activeElement,list); // The explicit file click owns keyboard focus.
});
test('scroll tracking is coalesced, isolated to continuous mode and removed on teardown',()=>{
  const view=viewer();view.render(state());const content=view.root.querySelector('.br-content');
  content.dispatchEvent({type:'scroll'});assert.equal(view.frames.size,0);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();
  content.dispatchEvent({type:'scroll'});content.dispatchEvent({type:'scroll'});assert.equal(view.frames.size,1);
  mode.value='file';mode.onchange();view.flushFrames();assert.equal(view.root.querySelectorAll('.br-nav-group')[1].textContent,'←1 / 1→');
  mode.value='continuous';mode.onchange();content.dispatchEvent({type:'scroll'});assert.equal(view.frames.size,1);
  view.destroy();assert.equal(view.frames.size,0);content.dispatchEvent({type:'scroll'});assert.equal(view.frames.size,0);
});
test('file navigation reuses sidebar rows and keeps selection, focus and changed metadata current',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go',status:'M'},{path:'two.go',status:'A'}]});view.render(initial);
  const list=view.root.querySelector('.br-files'),rows=view.root.querySelectorAll('.br-file');
  list.scrollTop=41;rows[1].click();
  assert.deepEqual(view.root.querySelectorAll('.br-file'),rows);
  assert.equal(rows[0].attrs['aria-selected'],'false');assert.equal(rows[1].attrs['aria-selected'],'true');
  assert.equal(view.document.activeElement,list);assert.equal(list.scrollTop,41);
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  assert.deepEqual(view.root.querySelectorAll('.br-file'),rows);
  rows[0].click();assert.equal(rows[0].attrs['aria-selected'],'true');
  view.render({...initial,version:3,captured:'snapshot2',files:[initial.files[0],{path:'two.go',status:'M',statsKnown:true,added:5,removed:2}]});
  assert.notEqual(view.root.querySelectorAll('.br-file')[1],rows[1]);
  assert.match(view.root.querySelectorAll('.br-file')[1].textContent,/\+5−2/);
  assert.equal(view.document.activeElement,list);
});
test('review preload starts the whole filtered list without changing selection or issuing foreground reads',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'one_test.go'},{path:'two.go'},{path:'three.go'},{path:'four.go'}]});
  view.render(initial);view.flushFrames();
  assert.deepEqual(view.prefetchRequests,[{operation:'review-prefetch',version:1,prefetchID:1,prefetch:[2,3,4]}]);
  assert.equal(view.requests.length,0);
  view.prefetch({...initial,file:2,review:{...initial.review,selected:payload('two.go',{newText:'PREFETCHED CONTENT\n'})}});
  assert.equal(view.root.querySelector('.br-file').attrs['aria-selected'],'true');
  view.root.querySelectorAll('.br-file')[1].click();
  assert.match(view.root.querySelector('.br-code').textContent,/PREFETCHED CONTENT/);
  assert.equal(view.requests.length,0);
  view.flushFrames();assert.equal(view.prefetchRequests.length,1);
  control(view,'Edit current working file').click();
  assert.equal(view.requests.at(-1).operation,'review-file');assert.equal(view.requests.at(-1).file,2);
  assert.equal(view.root.querySelector('.br-edit-text'),null);
});
test('stale prefetch cannot cross refreshes or overwrite a newer foreground result',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  for(const changed of [{captured:'older'},{head:'old-head'},{review:{...initial.review,base:'other-base'}},{review:{...initial.review,worktreeFingerprint:'older'}}]) {
    const stale={...initial,...changed};stale.review={...stale.review,selected:payload('two.go',{newText:'STALE CONTENT\n'})};view.prefetch(stale);
  }
  view.root.querySelectorAll('.br-file')[1].click();assert.equal(view.requests.length,0);
  assert.doesNotMatch(view.root.querySelector('.br-content').textContent,/STALE CONTENT/);
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go',{newText:'FRESH CONTENT\n'})}});
  view.prefetch({...initial,file:1,review:{...initial.review,selected:payload('two.go',{newText:'STALE CONTENT\n'})}});
  assert.match(view.root.querySelector('.br-code').textContent,/FRESH CONTENT/);
  view.destroy();view.prefetch(initial);assert.equal(view.frames.size,0);
});
test('cached Review navigation stays usable while an unrelated file is loading',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'},{path:'three.go'}]});view.render(initial);
  view.prefetch({...initial,file:1,review:{...initial.review,selected:payload('two.go',{newText:'READY CONTENT\n'})}});
  view.root.querySelectorAll('.br-file')[2].click();
  view.render({...initial,version:2,loading:true});
  assert.equal(control(view,'Previous file (Option+Left)').disabled,false);
  control(view,'Previous file (Option+Left)').click();
  assert.match(view.root.querySelector('.br-code').textContent,/READY CONTENT/);
  assert.equal(control(view,'Edit current working file').disabled,true);
  assert.equal(view.requests.length,0);
});
test('opening Review drains every filtered file and retains more than the old 24-file cache',()=>{
  const files=Array.from({length:37},(_,i)=>({path:`file-${i}.go`}));files.splice(3,0,{path:'skip_test.go'});
  const initial=state({files,review:{...state().review,selected:payload(files[0].path)}}),view=viewer();view.render(initial);
  const loaded=new Set([files[0].path]);let batches=0;
  while(loaded.size<37){
    view.flushFrames();const request=view.prefetchRequests.at(-1);assert.ok(request);assert.ok(request.prefetch.length<=8);assert.ok(++batches<10);
    for(const index of request.prefetch){const path=files[index].path;assert.notEqual(path,'skip_test.go');assert.equal(loaded.has(path),false);loaded.add(path);view.prefetch({...initial,prefetchID:request.prefetchID,review:{...initial.review,selected:payload(path)}});}
    view.prefetch({...initial,prefetchID:request.prefetchID,done:true});
  }
  view.flushFrames();const requests=view.prefetchRequests.length;
  assert.equal(view.requests.length,0);assert.equal(view.root.querySelector('.br-preload-status').textContent,'37 files ready');
  const rows=view.root.querySelectorAll('.br-file');rows.at(-1).click();rows[0].click();view.flushFrames();
  assert.equal(view.prefetchRequests.length,requests);assert.match(view.root.querySelector('.br-code').textContent,/literal/);
});
test('held file arrows coalesce paints, keep controls stable and never queue foreground reads',()=>{
  const files=Array.from({length:35},(_,i)=>({path:`file-${i}.go`})),initial=state({files,review:{...state().review,selected:payload(files[0].path)}}),view=viewer();view.render(initial);view.flushFrames();
  const batch=view.prefetchRequests[0],list=view.root.querySelector('.br-files'),toolbar=view.root.querySelector('.br-toolbar'),base=view.root.querySelector('.br-base-select'),refresh=control(view,'Refresh branch review');
  const card=view.root.querySelector('.br-file-card');let disabledChanges=0;
  for(const node of [base,refresh]){let disabled=node.disabled;Object.defineProperty(node,'disabled',{get:()=>disabled,set:value=>{if(value!==disabled)disabledChanges++;disabled=value;}});}
  view.root.querySelector('.br-content').scrollTop=150;
  list.focus();for(let i=0;i<40;i++)list.onkeydown({key:'ArrowDown',repeat:true,preventDefault(){}});
  assert.equal(selectedPath(view),'file-34.go');assert.equal(view.root.querySelector('.br-file-card'),card);
  assert.equal(view.requests.length,0);view.flushFrames();assert.equal(view.root.querySelector('.br-file-card'),card);assert.equal(view.root.querySelector('.br-content').scrollTop,150);
  const selectedCard=view.root.querySelector('.br-file-card');
  for(const index of batch.prefetch)view.prefetch({...initial,prefetchID:batch.prefetchID,review:{...initial.review,selected:payload(files[index].path)}});
  assert.equal(view.root.querySelector('.br-file-card'),selectedCard);assert.equal(view.root.querySelector('.br-toolbar'),toolbar);assert.equal(disabledChanges,0);
  view.prefetch({...initial,prefetchID:batch.prefetchID,done:true});view.flushFrames();
  assert.equal(view.prefetchRequests.at(-1).prefetch[0],34);assert.equal(view.requests.length,0);
  view.flushFrames();assert.equal(view.prefetchRequests.length,2);
});
test('hidden-window preload resumes and rejects the cancelled batch',()=>{
  const initial=state({files:[{path:'one.go'},{path:'two.go'}]}),view=viewer();view.render(initial);view.flushFrames();const old=view.prefetchRequests[0];
  view.setVisible(false);view.prefetch({...initial,prefetchID:old.prefetchID,review:{...initial.review,selected:payload('two.go')}});
  view.setVisible(true);view.flushFrames();const current=view.prefetchRequests.at(-1);assert.notEqual(current.prefetchID,old.prefetchID);
  view.prefetch({...initial,prefetchID:old.prefetchID,done:true});view.flushFrames();assert.equal(view.prefetchRequests.length,2);
  view.prefetch({...initial,prefetchID:current.prefetchID,review:{...initial.review,selected:payload('two.go')}});
  view.prefetch({...initial,prefetchID:current.prefetchID,done:true});view.flushFrames();assert.equal(view.root.querySelector('.br-preload-status').textContent,'2 files ready');
});
test('full refresh invalidates same-size cached file content while selections retain it',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  view.root.querySelectorAll('.br-file')[1].click();
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  view.root.querySelectorAll('.br-file')[0].click();assert.match(view.root.querySelector('.br-code').textContent,/literal/);
  const next={...initial,version:3,captured:'snapshot2',review:{...initial.review,selected:payload('one.go',{newText:'prefix\nNEW CONTENT\nsuffix\n'})}};
  view.render(next);assert.match(view.root.querySelector('.br-code').textContent,/NEW CONTENT/);
  view.root.querySelectorAll('.br-file')[1].click();view.flushFrames();assert.ok(view.prefetchRequests.at(-1).prefetch.includes(1));
});
test('edit refreshes backend selection and waits for native guard acknowledgement',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();
  assert.equal(view.requests.at(-1).operation,'review-file');assert.equal(view.root.querySelector('.br-edit-text'),null);
  view.render({...initial,version:2});
  const text=view.root.querySelector('.br-edit-text');assert.equal(text.readOnly,true);assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.requests.at(-1).file,0);assert.equal(view.requests.at(-1).token,'one.go-token');
  view.render({...initial,version:2,reviewEditing:true});assert.equal(text.readOnly,false);
  text.value='saved text';text.oninput();control(view,'Save changes to working file (Command+S)').click();
  assert.equal(view.requests.at(-1).operation,'review-save');assert.equal(view.requests.at(-1).content,'saved text');
  view.render({...initial,version:3,reviewEditing:true,error:'The working file changed'});
  assert.equal(view.root.querySelector('.br-edit-text'),text);assert.equal(text.value,'saved text');assert.equal(text.readOnly,false);assert.match(view.root.querySelector('.br-edit-warning').textContent,/still here/);
});
test('successful save waits for edit guard release before replacing textarea',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  const text=view.root.querySelector('.br-edit-text');text.value='saved';control(view,'Save changes to working file (Command+S)').click();
  view.render({...initial,version:3,reviewEditing:true,review:{...initial.review,selected:payload('one.go',{newText:'saved'})}});
  assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.requests.at(-1).editing,false);assert.equal(view.root.querySelector('.br-edit-text'),text);
  view.render({...initial,version:3,reviewEditing:false,review:{...initial.review,selected:payload('one.go',{newText:'saved'})}});
  assert.equal(view.root.querySelector('.br-edit-text'),null);
});
test('dirty cancellation asks inside the editor and preserves draft until explicitly discarded',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  const text=view.root.querySelector('.br-edit-text');text.value='unsaved';control(view,'Cancel editing').click();assert.match(view.root.querySelector('.br-discard').textContent,/Discard/);
  control(view,'Keep editing').click();assert.equal(view.root.querySelector('.br-discard'),null);assert.equal(text.value,'unsaved');
});
test('no-final-newline marker and independent horizontal scroll controls remain available',()=>{
  const view=viewer();view.render(state({review:{baseRef:'main',base:'base',refs:['main'],selected:payload('one.go',{oldText:'old',newText:'new'})}}));
  assert.match(view.root.querySelector('.br-file-card').textContent,/No newline at end of original and current file/);
  const scrolls=view.root.querySelectorAll('.br-horizontal-scroll');assert.equal(scrolls.length,2);scrolls[0].scrollLeft=40;scrolls[0].onscroll();assert.equal(view.root.querySelector('.br-file-card').style['--br-left-scroll'],'-40px');
});

test('unexpected WebKit reload restores a window draft under the native edit guard',()=>{
  const storage=new Map(),initial=state(),first=viewer(storage);first.render(initial);control(first,'Edit current working file').click();first.render({...initial,version:2});first.render({...initial,version:2,reviewEditing:true});
  first.root.querySelector('.br-edit-text').value='recovered draft';first.root.querySelector('.br-edit-text').oninput();
  const restored=viewer(storage);restored.render({...initial,version:2,reviewEditing:true});
  assert.equal(restored.root.querySelector('.br-edit-text').value,'recovered draft');assert.equal(restored.root.querySelector('.br-edit-text').readOnly,false);
  assert.match(restored.root.querySelector('.br-notice').textContent,/restored/);
});
test('orphan edit guard without a recoverable draft is released so the window can close',()=>{
  const view=viewer();view.render(state({reviewEditing:true}));
  assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.requests.at(-1).editing,false);
});
test('CRLF files keep CRLF on explicit Save',()=>{
  const view=viewer(),initial=state({review:{baseRef:'main',base:'base',refs:['main'],selected:payload('one.go',{newText:'one\r\ntwo\r\n'})}});view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  view.root.querySelector('.br-edit-text').value='one\nchanged\n';control(view,'Save changes to working file (Command+S)').click();
  assert.equal(view.requests.at(-1).content,'one\r\nchanged\r\n');
});
test('unchanged Save preserves raw source after textarea normalization and draft recovery',()=>{
  for(const text of ['one\r\ntwo\r\n','one\r\ntwo\n','one\rtwo\r']){
    const storage=new Map(),initial=state({review:{...state().review,selected:payload('one.go',{newText:text})}}),first=viewer(storage);
    first.render(initial);control(first,'Edit current working file').click();first.render({...initial,version:2});first.render({...initial,version:2,reviewEditing:true});
    assert.equal(first.root.querySelector('.br-edit-text').value,'one\ntwo\n');
    const recovered=viewer(storage);recovered.render({...initial,version:2,reviewEditing:true});
    assert.equal(recovered.root.querySelector('.br-edit-text').value,'one\ntwo\n');
    control(recovered,'Save changes to working file (Command+S)').click();assert.equal(recovered.requests.at(-1).content,text);
  }
});
test('custom Tab insertion cannot mutate a textarea before edit guard ACK or during Save',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});
  const text=view.root.querySelector('.br-edit-text');text.setRangeText=()=>{throw Error('mutated read-only editor');};
  text.onkeydown({key:'Tab',preventDefault(){}});
  view.render({...initial,version:2,reviewEditing:true});control(view,'Save changes to working file (Command+S)').click();text.onkeydown({key:'Tab',preventDefault(){}});
});

test('pending edit retries its file selection when a newer unrelated snapshot rejects the request',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();view.flushFrames();
  const second={...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}};view.render(second);
  control(view,'Edit current working file').click();assert.equal(view.requests.at(-1).file,0);
  view.render({...second,version:3});assert.equal(view.requests.at(-1).operation,'review-file');assert.equal(view.requests.at(-1).file,0);assert.equal(view.requests.at(-1).version,3);
  view.render({...initial,version:4});assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.root.querySelector('.br-edit-text').readOnly,true);
});
test('failed file reads do not automatically retry forever',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);view.root.querySelectorAll('.br-file')[1].click();
  view.prefetch({...initial,error:'File unavailable',review:{...initial.review,selected:payload('two.go')}});
  view.prefetch({...initial,done:true});view.flushFrames();view.flushFrames();
  assert.equal(view.prefetchRequests.length,1);assert.equal(view.requests.length,0);
  control(view,'Retry loading two.go').click();view.flushFrames();assert.equal(view.prefetchRequests.length,2);
});

test('cached file navigation avoids another read and preserves change selection across identical updates',()=>{
  const before=Array.from({length:80},(_,index)=>`line ${index+1}`),after=before.slice();
  after[19]='first replacement';after[59]='second replacement';
  const data=payload('one.go',{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:[
    {oldStart:20,oldLines:1,newStart:20,newLines:1,commits:[]},
    {oldStart:60,oldLines:1,newStart:60,newLines:1,commits:[]},
  ]});
  const initial=state({files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:data}}),view=viewer();
  view.render(initial);control(view,'Next file (Option+Right)').click();
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  const reads=view.requests.length;control(view,'Previous file (Option+Left)').click();
  assert.equal(view.requests.length,reads);
  control(view,'Next change (Option+Down)').click();
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  // Model the browser's scroll position after revealing the second block. The
  // update has identical content and must not put the reader back at the top.
  const content=view.root.querySelector('.br-content');content.scrollTop=900;
  view.render({...initial,version:3});
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  assert.equal(content.scrollTop,900);
});

test('pending file selection retains the diff and scroll with only a delayed loading indicator',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'},{path:'three.go'}]});view.render(initial);
  const content=view.root.querySelector('.br-content'),card=view.root.querySelector('.br-file-card'),details=view.root.querySelector('.br-commit-panel').children[0],loading=view.root.querySelector('.br-file-loading');content.scrollTop=180;
  view.root.querySelectorAll('.br-file')[1].click();
  assert.equal(selectedPath(view),'two.go');assert.equal(view.root.querySelector('.br-file-card'),card);assert.equal(content.scrollTop,180);
  assert.equal(view.root.querySelector('.br-commit-panel').children[0],details);assert.equal(loading.hidden,true);
  assert.equal(control(view,'Load diff for two.go'),undefined);assert.equal(control(view,'Edit current working file').disabled,true);
  view.advance(249);assert.equal(loading.hidden,true);view.advance(1);assert.equal(loading.hidden,false);assert.equal(loading.textContent,'Loading…');
  view.root.querySelectorAll('.br-file')[2].click();assert.equal(loading.hidden,true);
  view.prefetch({...initial,review:{...initial.review,selected:payload('two.go')}});assert.equal(view.root.querySelector('.br-file-card'),card);
  view.prefetch({...initial,review:{...initial.review,selected:payload('three.go')}});
  assert.equal(view.root.querySelector('.br-file-card').dataset.path,'three.go');assert.equal(loading.hidden,true);
  view.advance(1000);assert.equal(loading.hidden,true);assert.equal(view.timers.size,0);
});
test('loading indicators cancel on a cached selection, hidden window and teardown',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  const rows=view.root.querySelectorAll('.br-file'),loading=view.root.querySelector('.br-file-loading');
  rows[1].click();view.advance(100);rows[0].click();view.advance(500);assert.equal(loading.hidden,true);assert.equal(view.timers.size,0);
  rows[1].click();view.setVisible(false);view.advance(500);assert.equal(loading.hidden,true);
  view.setVisible(true);view.advance(250);assert.equal(loading.hidden,false);
  view.destroy();assert.equal(view.timers.size,0);
});
test('selecting an uncached file reveals its first changed block when its text arrives',()=>{
  const before=Array.from({length:250},(_,index)=>`line ${index+1}`),after=before.slice();after[183]='replacement';
  const data=payload('two.go',{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:[
    {oldStart:184,oldLines:1,newStart:184,newLines:1,commits:[]},
  ]});
  const initial=state({files:[{path:'one.go'},{path:'two.go'}]}),view=viewer();view.render(initial);
  const previousCode=view.root.querySelector('.br-code');
  control(view,'Next file (Option+Right)').click();
  assert.equal(view.root.querySelector('.br-code'),previousCode);
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:data}});
  const change=view.root.querySelector('.br-current-change');
  assert.equal(change.dataset.path,'two.go');assert.equal(change.dataset.change,'0');
  assert.equal(change.revealed,true);
  assert.equal(control(view,'Previous change (Option+Up)').disabled,false);
  assert.equal(control(view,'Next change (Option+Down)').disabled,true);
  // A later refresh must preserve the reader's position in surrounding context,
  // rather than repeat the initial jump to this file's only changed block.
  const content=view.root.querySelector('.br-content');content.scrollTop=500;
  view.render({...initial,version:3,captured:'snapshot2',file:1,review:{...initial.review,selected:data}});
  assert.equal(content.scrollTop,500);
  assert.notEqual(view.root.querySelector('.br-current-change').revealed,true);
});

test('hovering another block leaves explicit change navigation and selection intact',()=>{
  const data=payload('one.go',{oldText:'old first\ncontext\nold second\n',newText:'new first\ncontext\nnew second\n',hunks:[
    {oldStart:1,oldLines:1,newStart:1,newLines:1,commits:[commit]},
    {oldStart:3,oldLines:1,newStart:3,newLines:1,commits:[{...commit,subject:'Second change'}]},
  ]});
  const view=viewer();view.render(state({review:{...state().review,selected:data}}));
  const changes=view.root.querySelectorAll('.br-change');
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'0');
  assert.match(changes[1].title,/Second change/);
  changes[1].onmouseenter?.();
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'0');
  assert.equal(control(view,'Previous change (Option+Up)').disabled,true);
  assert.equal(control(view,'Next change (Option+Down)').disabled,false);
  changes[1].click();
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  assert.match(view.root.querySelector('.br-commit-panel').textContent,/Second change/);
});

test('change and filtered-file counters stay between their arrows and track boundaries',()=>{
  const view=viewer();view.render(state({files:[],review:{...state().review,selected:null}}));
  const groups=view.root.querySelectorAll('.br-nav-group');
  const changes=groups.find(node=>node.attrs['aria-label']==='Changes in current file');
  const files=groups.find(node=>node.attrs['aria-label']==='Files in filtered list');
  assert.deepEqual(changes.children.map(node=>node.textContent),['↑','0 / 0','↓']);
  assert.deepEqual(files.children.map(node=>node.textContent),['←','0 / 0','→']);
  assert.equal(changes.children[0].disabled,true);assert.equal(changes.children[2].disabled,true);
  const data=payload('one.go',{oldText:'old first\ncontext\nold second\n',newText:'new first\ncontext\nnew second\n',hunks:[
    {oldStart:1,oldLines:1,newStart:1,newLines:1,commits:[]},
    {oldStart:3,oldLines:1,newStart:3,newLines:1,commits:[]},
  ]});
  view.render(state({version:2,files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:data}}));
  assert.deepEqual(changes.children.map(node=>node.textContent),['↑','1 / 2','↓']);
  assert.deepEqual(files.children.map(node=>node.textContent),['←','1 / 2','→']);
  assert.equal(changes.children[0].disabled,true);assert.equal(changes.children[2].disabled,false);
  changes.children[2].click();
  assert.equal(changes.children[1].textContent,'2 / 2');
  assert.equal(changes.children[1].attrs['aria-label'],'Change 2 of 2 in current file');
  assert.equal(changes.children[0].disabled,false);assert.equal(changes.children[2].disabled,false);
  const filter=view.root.querySelector('.br-filter');filter.value='one';filter.oninput();
  assert.deepEqual(files.children.map(node=>node.textContent),['←','1 / 1','→']);
  assert.equal(files.children[1].attrs['aria-label'],'File 1 of 1');
  assert.equal(files.children[0].disabled,true);assert.equal(files.children[2].disabled,true);
  assert.equal(changes.children[1].textContent,'2 / 2');
  assert.equal(changes.children[2].disabled,true);
});

test('change navigation reveals the correct page across split and unified row boundaries',()=>{
  const before=Array.from({length:5002},(_,index)=>`line ${index+1}`),after=before.slice();
  const lines=[20,5000,5002];for(const line of lines)after[line-1]=`replacement ${line}`;
  const data=payload('one.go',{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:lines.map(line=>({oldStart:line,oldLines:1,newStart:line,newLines:1,commits:[]}))});
  const view=viewer();view.render(state({review:{...state().review,selected:data}}));
  const selected=()=>view.root.querySelector('.br-current-change');
  const next=()=>control(view,'Next change (Option+Down)').click();
  const previous=()=>control(view,'Previous change (Option+Up)').click();
  assert.equal(selected().dataset.change,'0');assert.equal(selected().revealed,true);
  next();assert.equal(selected().dataset.change,'1');assert.equal(selected().revealed,true);
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 1–5000 of 5002/);
  // A replacement contributes two unified rows, so the second block starts on
  // the following page in unified mode even though it fits the first split page.
  control(view,'Unified').click();
  assert.equal(selected().dataset.change,'1');assert.equal(selected().revealed,true);
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 5001–5005 of 5005/);
  previous();assert.equal(selected().dataset.change,'0');assert.equal(selected().revealed,true);
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 1–5000 of 5005/);
  control(view,'Side by side').click();next();next();
  assert.equal(selected().dataset.change,'2');assert.equal(selected().revealed,true);
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 5001–5002 of 5002/);
  view.render(state({version:2,captured:'snapshot2',review:{...state().review,selected:data}}));
  assert.ok(selected(),'refresh retains the selected change on its later page');
  assert.equal(selected().dataset.change,'2');assert.equal(selected().revealed,true);
  assert.equal(view.root.querySelector('.br-change-position').textContent,'3 / 3');
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 5001–5002 of 5002/);
  dispatchKey(view.document.body,'f',{metaKey:true});
  const find=view.root.querySelector('.diff-find-input');find.value='replacement 5002';find.oninput();
  assert.equal(selected().dataset.change,'2');assert.equal(selected().revealed,true);
  assert.equal(view.root.querySelector('.br-row-paging'),null);
  find.value='line';find.oninput();
  assert.equal(selected().dataset.change,'2');assert.equal(selected().revealed,true);
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 5001–5002 of 5002/);
  find.value='replacement 5002';find.oninput();dispatchKey(find,'Escape');
  assert.equal(selected().dataset.change,'2');assert.equal(selected().revealed,true);
  assert.match(view.root.querySelector('.br-row-paging').textContent,/Rows 5001–5002 of 5002/);
});

const separatedChanges=(path,count=2)=>{
  const before=Array.from({length:count*3+1},(_,index)=>`line ${index+1}`),after=before.slice();
  const lines=Array.from({length:count},(_,index)=>index*3+2);
  for(const line of lines)after[line-1]=`replacement ${line}`;
  return payload(path,{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:lines.map(line=>({oldStart:line,oldLines:1,newStart:line,newLines:1,commits:[]}))});
};
const selectedPath=view=>view.root.querySelectorAll('.br-file').find(node=>node.attrs['aria-selected']==='true')?.dataset.path;
const dispatchKey=(target,key,modifiers={})=>{
  const event={type:'keydown',key,bubbles:true,cancelable:true,preventDefault(){this.defaultPrevented=true;},stopPropagation(){this.cancelBubble=true;},...modifiers};
  target.dispatchEvent(event);return !!event.defaultPrevented;
};
const optionArrow=(view,key)=>{
  assert.equal(dispatchKey(view.document.activeElement,key,{altKey:true}),true);
};

test('change buttons and Option arrows cross cached files at the correct ends without wrapping',()=>{
  const one=separatedChanges('one.go'),two=separatedChanges('two.go');
  const initial=state({files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:one}}),view=viewer();view.render(initial);
  control(view,'Next file (Option+Right)').click();
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:two}});
  control(view,'Previous file (Option+Left)').click();
  const next=()=>control(view,'Next change (Option+Down)').click(),previous=()=>control(view,'Previous change (Option+Up)').click();
  const current=()=>view.root.querySelector('.br-current-change');
  next();assert.equal(current().dataset.change,'1');
  next();assert.equal(selectedPath(view),'two.go');assert.equal(current().dataset.change,'0');assert.equal(current().revealed,true);
  next();assert.equal(current().dataset.change,'1');assert.equal(control(view,'Next change (Option+Down)').disabled,true);
  optionArrow(view,'ArrowDown');assert.equal(selectedPath(view),'two.go');assert.equal(current().dataset.change,'1');
  previous();assert.equal(current().dataset.change,'0');
  previous();assert.equal(selectedPath(view),'one.go');assert.equal(current().dataset.change,'1');assert.equal(current().revealed,true);
  assert.equal(view.root.querySelector('.br-change-position').textContent,'2 / 2');
  optionArrow(view,'ArrowUp');assert.equal(current().dataset.change,'0');
  assert.equal(control(view,'Previous change (Option+Up)').disabled,true);
  optionArrow(view,'ArrowUp');assert.equal(selectedPath(view),'one.go');assert.equal(current().dataset.change,'0');
  optionArrow(view,'ArrowDown');optionArrow(view,'ArrowDown');
  assert.equal(selectedPath(view),'two.go');assert.equal(current().dataset.change,'0');
  const filter=view.root.querySelector('.br-filter');filter.value='one';filter.oninput();
  assert.equal(selectedPath(view),'one.go');next();
  assert.equal(control(view,'Next change (Option+Down)').disabled,true);
  optionArrow(view,'ArrowDown');assert.equal(selectedPath(view),'one.go');assert.equal(current().dataset.change,'1');
});

for(const presentation of ['file','continuous'])test(`cross-file changes wait for uncached text and reveal the requested end in ${presentation} view`,()=>{
  const initial=state({file:1,files:[{path:'one.go'},{path:'two.go'},{path:'three.go'}],review:{...state().review,selected:separatedChanges('two.go')}}),view=viewer();view.render(initial);
  if(presentation==='continuous'){const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();}
  control(view,'Previous change (Option+Up)').click();
  view.flushFrames();assert.equal(selectedPath(view),'one.go');assert.equal(view.prefetchRequests.at(-1).prefetch[0],0);
  assert.equal(control(view,'Previous change (Option+Up)').disabled,true);
  assert.equal(control(view,'Next change (Option+Down)').disabled,true);
  assert.equal(control(view,'Next file (Option+Right)').disabled,false);
  const requests=view.requests.length;optionArrow(view,'ArrowDown');optionArrow(view,'ArrowUp');
  assert.equal(selectedPath(view),'one.go');assert.equal(view.requests.length,requests);
  view.render({...initial,version:2,file:0,review:{...initial.review,selected:separatedChanges('one.go',3)}});
  const selected=view.root.querySelector('.br-current-change');
  assert.equal(selected.dataset.path,'one.go');assert.equal(selected.dataset.change,'2');assert.equal(selected.revealed,true);
  assert.equal(view.root.querySelector('.br-change-position').textContent,'3 / 3');
  optionArrow(view,'ArrowUp');assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  optionArrow(view,'ArrowDown');optionArrow(view,'ArrowDown');
  assert.equal(selectedPath(view),'two.go');assert.equal(view.root.querySelector('.br-current-change').dataset.change,'0');
  assert.equal(view.root.querySelector('.br-current-change').revealed,true);
  optionArrow(view,'ArrowDown');optionArrow(view,'ArrowDown');assert.equal(selectedPath(view),'three.go');
  assert.equal(control(view,'Next change (Option+Down)').disabled,true);
  view.render({...initial,version:3,file:2,review:{...initial.review,selected:separatedChanges('three.go')}});
  const following=view.root.querySelector('.br-current-change');
  assert.equal(following.dataset.path,'three.go');assert.equal(following.dataset.change,'0');assert.equal(following.revealed,true);
});

test('a late previous-file reply does not move a newer file selection to its last block',()=>{
  const initial=state({file:1,files:[{path:'one.go'},{path:'two.go'},{path:'three.go'}],review:{...state().review,selected:separatedChanges('two.go')}}),view=viewer();view.render(initial);
  optionArrow(view,'ArrowUp');assert.equal(selectedPath(view),'one.go');
  control(view,'Next file (Option+Right)').click();control(view,'Next file (Option+Right)').click();
  assert.equal(selectedPath(view),'three.go');
  view.render({...initial,version:2,file:0,review:{...initial.review,selected:separatedChanges('one.go',3)}});
  view.flushFrames();assert.equal(selectedPath(view),'three.go');assert.equal(view.prefetchRequests.at(-1).prefetch[0],2);
  assert.equal(view.root.querySelector('.br-current-change').dataset.path,'two.go');
  view.render({...initial,version:3,file:2,review:{...initial.review,selected:separatedChanges('three.go')}});
  const selected=view.root.querySelector('.br-current-change');
  assert.equal(selected.dataset.path,'three.go');assert.equal(selected.dataset.change,'0');assert.equal(selected.revealed,true);
});

test('change navigation lands on metadata-only files before proceeding to the following file',()=>{
  const initial=state({files:[{path:'one.go'},{path:'metadata.go'},{path:'three.go'}],review:{...state().review,selected:separatedChanges('one.go')}}),view=viewer();view.render(initial);
  const next=()=>control(view,'Next change (Option+Down)').click(),previous=()=>control(view,'Previous change (Option+Up)').click();
  next();next();assert.equal(selectedPath(view),'metadata.go');
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:separatedChanges('metadata.go',0)}});
  assert.equal(selectedPath(view),'metadata.go');assert.equal(view.root.querySelector('.br-change-position').textContent,'0 / 0');
  assert.equal(control(view,'Previous change (Option+Up)').disabled,false);assert.equal(control(view,'Next change (Option+Down)').disabled,false);
  next();assert.equal(selectedPath(view),'three.go');
  view.render({...initial,version:3,file:2,review:{...initial.review,selected:separatedChanges('three.go',1)}});
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'0');
  previous();assert.equal(selectedPath(view),'metadata.go');assert.equal(view.root.querySelector('.br-current-change'),null);
  previous();assert.equal(selectedPath(view),'one.go');assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  const filter=view.root.querySelector('.br-filter');filter.value='metadata';filter.oninput();
  assert.equal(selectedPath(view),'metadata.go');
  assert.equal(control(view,'Previous change (Option+Up)').disabled,true);assert.equal(control(view,'Next change (Option+Down)').disabled,true);
});

test('review shortcuts work from body and descendant focus, survive replacement, and detach on destroy',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:separatedChanges('one.go',3)}});view.render(initial);
  const current=()=>view.root.querySelector('.br-current-change').dataset.change;
  assert.equal(view.document.activeElement,view.document.body);
  optionArrow(view,'ArrowDown');assert.equal(current(),'1');
  const source=view.root.querySelector('.br-source');
  assert.equal(dispatchKey(source,'ArrowDown',{altKey:true}),true);assert.equal(current(),'2');assert.equal(selectedPath(view),'one.go');
  assert.equal(dispatchKey(source,'ArrowUp',{altKey:true}),true);assert.equal(current(),'1');
  assert.equal(dispatchKey(view.document.documentElement,'ArrowUp',{altKey:true}),true);assert.equal(current(),'0');
  source.focus();
  const syntax=view.root.querySelectorAll('label').find(node=>node.textContent==='Syntax highlighting').querySelector('input');
  syntax.checked=false;syntax.onchange();
  assert.equal(view.document.activeElement,view.document.body);
  optionArrow(view,'ArrowDown');assert.equal(current(),'1');
  const requests=view.requests.length;view.destroy();
  assert.equal(dispatchKey(view.document.body,'ArrowRight',{altKey:true}),false);
  assert.equal(view.requests.length,requests);
});

test('review document shortcuts preserve editing, text controls, modified keys, and handled events',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:separatedChanges('one.go',3)}});view.render(initial);
  const current=()=>view.root.querySelector('.br-current-change').dataset.change;
  for(const modifiers of [{shiftKey:true},{ctrlKey:true},{metaKey:true},{isComposing:true}]){
    assert.equal(dispatchKey(view.document.body,'ArrowDown',{altKey:true,...modifiers}),false);assert.equal(current(),'0');
  }
  const editable=view.document.createElement('span');editable.isContentEditable=true;
  const textarea=view.document.createElement('textarea');view.root.append(editable,textarea);
  for(const target of [view.root.querySelector('.br-filter'),view.root.querySelector('.br-base-select'),textarea,editable]){
    assert.equal(dispatchKey(target,'ArrowRight',{altKey:true}),false);
    assert.equal(selectedPath(view),'one.go');
  }
  const source=view.root.querySelector('.br-source');source.onkeydown=event=>event.preventDefault();
  assert.equal(dispatchKey(source,'ArrowDown',{altKey:true}),true);assert.equal(current(),'0');
  assert.equal(view.requests.length,0);
  control(view,'Edit current working file').click();
  assert.equal(dispatchKey(view.document.body,'ArrowDown',{altKey:true}),false);assert.equal(current(),'0');
  view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  assert.equal(dispatchKey(view.document.body,'ArrowRight',{altKey:true}),false);
  assert.equal(selectedPath(view),'one.go');
});

test('Command+F finds diff text immediately, preserves source highlighting, and Escape restores full view',()=>{
  const before=Array.from({length:70},(_,i)=>`// context ${i+1}`),after=[...before];
  before[10]='const earlier = "before only";';after[10]='const later = "after only";';
  before[45]='const literal = "<script>[value]</script>";';after[45]=before[45];
  const data=payload('name-without-query.go',{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:[{oldStart:11,oldLines:1,newStart:11,newLines:1,commits:[commit]}]});
  const view=viewer(),initial=state({files:[{path:data.path,status:'M'}],review:{...state().review,selected:data}});view.render(initial);
  const input=view.root.querySelector('.diff-find-input'),bar=view.root.querySelector('.diff-find');
  assert.equal(bar.hidden,true);assert.equal(dispatchKey(view.document.body,'f',{metaKey:true}),true);
  assert.equal(bar.hidden,false);assert.equal(view.document.activeElement,input);
  input.value='BEFORE ONLY';input.oninput();
  assert.equal(view.root.querySelectorAll('.br-row').length,7);
  assert.equal(view.root.querySelector('.diff-search-hit').textContent,'before only');
  assert.match(view.root.querySelector('.diff-find-status').textContent,/1 matching line/);
  assert.equal(view.root.querySelectorAll('.br-file').length,1);assert.equal(view.requests.length,0);
  input.value='[value]';input.oninput();
  assert.equal(view.root.querySelectorAll('.br-row').length,7);
  assert.equal(view.root.querySelector('.diff-search-hit').textContent,'[value]');
  assert.equal(view.root.querySelectorAll('script').length,0);
  assert.ok(view.root.querySelector('.syntax-keyword'));
  dispatchKey(input,'f',{metaKey:true});assert.equal(input.selectionStart,0);assert.equal(input.selectionEnd,input.value.length);
  view.render({...initial,version:2});assert.equal(input.value,'[value]');assert.equal(view.document.activeElement,input);
  control(view,'Unified').click();assert.equal(view.root.querySelector('.diff-search-hit').textContent,'[value]');
  input.value='nothing matches this';input.oninput();
  assert.equal(view.root.querySelectorAll('.br-row').length,0);assert.match(view.root.querySelector('.diff-search-empty').textContent,/No matching text/);
  assert.equal(dispatchKey(input,'Escape'),true);assert.equal(bar.hidden,true);assert.equal(input.value,'');
  assert.equal(view.root.querySelectorAll('.br-row').length,71);assert.equal(view.document.activeElement,view.root.querySelector('.br-content'));
});

test('continuous text find includes collapsed context, loaded file scope, and new file payloads',()=>{
  const view=viewer(),before=Array.from({length:60},(_,i)=>`// line ${i+1}`);before[40]='// needle in unchanged context';
  const data=payload('one.go',{oldText:before.join('\n'),newText:before.join('\n'),hunks:[]});
  const initial=state({files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:data}});view.render(initial);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();
  dispatchKey(view.document.body,'f',{metaKey:true});const input=view.root.querySelector('.diff-find-input');input.value='needle';input.oninput();
  assert.match(view.root.querySelector('.diff-find-status').textContent,/1 matching line.*1 \/ 2 files loaded/);
  assert.equal(view.root.querySelector('.diff-search-hit').textContent,'needle');
  assert.equal(control(view,'Load diff for two.go'),undefined);view.flushFrames();
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go',{newText:'prefix\nneedle in second file\nsuffix\n'})}});
  assert.equal(input.value,'needle');assert.match(view.root.querySelector('.diff-find-status').textContent,/2 matching lines.*2 \/ 2 files loaded/);
  assert.equal(view.document.activeElement,input);
});

test('unified search displays original-only matches in whitespace-ignored context',()=>{
  const data=payload('one.go',{oldText:'const label = "old  context";\nold\n',newText:'const label = "old context";\nnew\n'});
  const view=viewer();view.render(state({review:{...state().review,ignoreWhitespace:true,selected:data}}));
  control(view,'Unified').click();
  const input=view.root.querySelector('.diff-find-input');input.value='old  context';input.oninput();
  const hit=view.root.querySelector('.diff-search-hit');assert.equal(hit.textContent,'old  context');
  assert.equal(hit.querySelector('.syntax-string').textContent,'old  context');
  assert.match(view.root.querySelector('.diff-find-status').textContent,/1 matching line/);
  const row=view.root.querySelector('.br-row');assert.deepEqual(row.querySelectorAll('.br-line-number').map(node=>node.textContent),['1','1']);
  input.value='';input.oninput();
  assert.match(view.root.querySelector('.br-row').textContent,/old context/);assert.doesNotMatch(view.root.querySelector('.br-row').textContent,/old  context/);
});

test('ignore globs match names at any depth, folder paths, wildcards and literal punctuation',()=>{
  const ignores=reviewIgnoreMatchers('*_test.go, *_gen.go\n*.pb.go\nnode_modules/\n/dist/**\nsrc/**/generated?.go\nfile[1].go');
  for(const path of ['main_test.go','src/deep/main_test.go','api/client_gen.go','api/v1/service.pb.go','web/node_modules/module/index.js','dist/a/b.js','src/generated1.go','src/a/b/generated2.go','file[1].go']) assert.equal(reviewFileIgnored({path},ignores),true,path);
  for(const path of ['main.go','main_test.go.bak','src/tests.go','web/dist/app.js','src/generated12.go','file1.go']) assert.equal(reviewFileIgnored({path},ignores),false,path);
  assert.equal(reviewFileIgnored({path:'src/real.go',oldPath:'src/old_gen.go'},ignores),false);
  assert.equal(reviewFileIgnored({path:'anything.go'},reviewIgnoreMatchers(' , \n')),false);
});

test('clicking the file list retains arrow navigation across redraws and asynchronous reads',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'one_test.go'},{path:'two.go'},{path:'three.go'}]});view.render(initial);
  view.root.querySelectorAll('.br-file')[0].click();
  const list=view.root.querySelector('.br-files');assert.equal(view.document.activeElement,list);
  assert.equal(dispatchKey(view.document.activeElement,'ArrowDown'),true);assert.equal(selectedPath(view),'two.go');assert.equal(view.document.activeElement,list);
  view.render({...initial,version:2,file:2,review:{...initial.review,selected:payload('two.go')}});
  assert.equal(view.document.activeElement,list);
  view.render({...initial,version:3,loading:true});
  dispatchKey(view.document.activeElement,'ArrowDown');assert.equal(selectedPath(view),'three.go');
  dispatchKey(view.document.activeElement,'ArrowDown');assert.equal(selectedPath(view),'three.go');
  dispatchKey(view.document.activeElement,'ArrowUp');assert.equal(selectedPath(view),'two.go');
  dispatchKey(view.document.activeElement,'Home');assert.equal(selectedPath(view),'one.go');
  dispatchKey(view.document.activeElement,'End');assert.equal(selectedPath(view),'three.go');
  assert.equal(dispatchKey(view.root.querySelector('.br-ignore-patterns'),'ArrowUp'),false);
});

test('review file arrows recover document focus and stop after clicking outside the list',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  const list=view.root.querySelector('.br-files');list.querySelector('.br-file').click();view.document.body.focus();
  assert.equal(dispatchKey(view.document.body,'ArrowDown'),true);assert.equal(selectedPath(view),'two.go');assert.equal(view.document.activeElement,list);
  view.document.body.focus();assert.equal(dispatchKey(view.document.body,'ArrowUp'),true);assert.equal(selectedPath(view),'one.go');
  assert.equal(dispatchKey(view.root.querySelector('.br-filter'),'ArrowDown'),false);
  view.root.querySelector('.br-content').dispatchEvent({type:'pointerdown',bubbles:true});view.document.body.focus();
  assert.equal(dispatchKey(view.document.body,'ArrowDown'),false);assert.equal(selectedPath(view),'one.go');
  list.querySelector('.br-file').click();view.destroy();view.document.body.focus();
  assert.equal(dispatchKey(view.document.body,'ArrowDown'),false);
});

test('default ignores choose a visible initial file and never reveal a late ignored response',()=>{
  const view=viewer(),initial=state({files:[{path:'first_test.go'},{path:'client_gen.go'},{path:'app.go'}],review:{...state().review,selected:payload('first_test.go')}});view.render(initial);
  assert.deepEqual(view.root.querySelectorAll('.br-file').map(n=>n.dataset.path),['app.go']);assert.equal(selectedPath(view),'app.go');
  assert.equal(view.root.querySelector('.br-files-title').textContent,'1 / 3 file');
  assert.equal(view.root.querySelector('.br-ignore-options').querySelector('summary').textContent,'Ignored files · 2 hidden');
  view.flushFrames();assert.equal(view.prefetchRequests.at(-1).prefetch[0],2);
  view.render({...initial,version:2});assert.equal(selectedPath(view),'app.go');assert.equal(view.root.querySelector('.br-file-card').dataset.path,'app.go');
  const show=view.root.querySelector('.br-show-ignored').querySelector('input');show.checked=true;show.onchange();
  assert.equal(view.root.querySelectorAll('.br-file').length,3);
  view.root.querySelectorAll('.br-file')[0].click();assert.equal(selectedPath(view),'first_test.go');
  show.checked=false;show.onchange();assert.equal(selectedPath(view),'app.go');
});

test('ignore patterns and search filter both presentations, with an honest empty state',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  const patterns=view.root.querySelector('.br-ignore-patterns');patterns.value='*.go';patterns.oninput();
  assert.equal(view.root.querySelectorAll('.br-file').length,0);assert.equal(view.root.querySelectorAll('.br-file-card').length,0);
  assert.equal(view.root.querySelector('.br-preload-status').textContent,'');
  assert.match(view.root.querySelector('.br-empty-state').textContent,/show ignored files/);
  assert.equal(control(view,'Edit current working file').disabled,true);
  assert.equal(control(view,'Next file (Option+Right)').disabled,true);
  view.render({...initial,version:2});assert.equal(view.root.querySelectorAll('.br-file-card').length,0);
  patterns.value='two.go';patterns.oninput();assert.equal(selectedPath(view),'one.go');
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();
  assert.deepEqual(view.root.querySelectorAll('.br-file-card').map(n=>n.dataset.path),['one.go']);
  const filter=view.root.querySelector('.br-filter');filter.value='two';filter.oninput();assert.equal(view.root.querySelectorAll('.br-file-card').length,0);
  const show=view.root.querySelector('.br-show-ignored').querySelector('input');show.checked=true;show.onchange();assert.equal(selectedPath(view),'two.go');
  assert.deepEqual(view.root.querySelectorAll('.br-file-card').map(n=>n.dataset.path),['two.go']);
});

test('custom and empty ignore patterns survive reopening and stay scoped to the repository',()=>{
  const storage=new Map(),first=viewer(storage);first.render(state());
  const patterns=first.root.querySelector('.br-ignore-patterns');patterns.value='*.pb.go';patterns.oninput();
  const second=viewer(storage);second.render(state());assert.equal(second.root.querySelector('.br-ignore-patterns').value,'*.pb.go');
  const third=viewer(storage);third.render(state({path:'/other'}));assert.match(third.root.querySelector('.br-ignore-patterns').value,/_test.go/);
  patterns.value='';patterns.oninput();const cleared=viewer(storage);cleared.render(state());assert.equal(cleared.root.querySelector('.br-ignore-patterns').value,'');
});

test('ignore controls cannot interrupt an edit and recovery preserves a now-ignored draft',()=>{
  const storage=new Map(),first=viewer(storage),initial=state();first.render(initial);
  control(first,'Edit current working file').click();first.render({...initial,version:2});first.render({...initial,version:2,reviewEditing:true});
  assert.equal(first.root.querySelector('.br-ignore-patterns').disabled,true);assert.equal(first.root.querySelector('.br-show-ignored').querySelector('input').disabled,true);
  const editor=first.root.querySelector('.br-edit-text');editor.value='unsaved';editor.oninput();
  // Another review can change the repository preference while this draft is open.
  storage.set('devjay-branch-review-ignore:/repo','*.go');
  const restored=viewer(storage);restored.render({...initial,version:3,reviewEditing:true});
  assert.equal(restored.root.querySelector('.br-edit-text').value,'unsaved');
  assert.equal(restored.root.querySelector('.br-edit-text').attrs['aria-label'],'Edit one.go');
});
