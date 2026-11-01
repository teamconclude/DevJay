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
const {reviewTextLines,reviewDiffModel,reviewVisibleRows,reviewUnifiedRows,reviewCommitSummary,reviewFileMatches} = await import(`data:text/javascript;base64,${Buffer.from(moduleSource).toString('base64')}`);

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
  }
  const root=new Element('main'),body=new Element('body'),html=new Element('html'),listeners={};
  document={body,documentElement:html,activeElement:body,createElement:tag=>new Element(tag),createDocumentFragment:()=>new Element('fragment'),
    addEventListener(type,listener){(listeners[type]||=new Set()).add(listener);},removeEventListener(type,listener){listeners[type]?.delete(listener);},
    dispatchEvent(event){event.target??=this;event.currentTarget=this;for(const listener of listeners[event.type]||[])listener(event);return !event.defaultPrevented;}};
  html.append(body);body.append(root);
  const context=vm.createContext({document,sessionStorage:{setItem:(key,value)=>storage.set(key,value),getItem:key=>storage.get(key)||null,removeItem:key=>storage.delete(key)}});vm.runInContext(syntaxSource.replace(/^export /gm,'')+'\n'+searchSource.replace(/^import .*;\n/gm,'').replace(/^export /gm,'')+'\n'+source.replace(/^import .*;\n/gm,'').replace(/^export /gm,''),context);
  const requests=[];const api=context.createBranchReview(root,data=>{requests.push(JSON.parse(JSON.stringify(data)));return true;});
  return {root,document,requests,...api};
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
  assert.equal(view.root.querySelector('textarea').value,newText);
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
  control(view,'Load diff for two.go').click();control(view,'Load diff for two.go').click();
  assert.equal(view.requests.length,1);assert.equal(view.requests[0].operation,'review-file');assert.equal(view.requests[0].file,1);
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  assert.equal(view.root.querySelectorAll('.br-code').length,2);
});
test('full refresh invalidates same-size cached file content while selections retain it',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  view.root.querySelectorAll('.br-file')[1].click();
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  view.root.querySelectorAll('.br-file')[0].click();assert.match(view.root.querySelector('.br-code').textContent,/literal/);
  const next={...initial,version:3,captured:'snapshot2',review:{...initial.review,selected:payload('one.go',{newText:'prefix\nNEW CONTENT\nsuffix\n'})}};
  view.render(next);assert.match(view.root.querySelector('.br-code').textContent,/NEW CONTENT/);
  view.root.querySelectorAll('.br-file')[1].click();assert.equal(view.requests.at(-1).file,1);
});
test('edit refreshes backend selection and waits for native guard acknowledgement',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();
  assert.equal(view.requests.at(-1).operation,'review-file');assert.equal(view.root.querySelector('textarea'),null);
  view.render({...initial,version:2});
  const text=view.root.querySelector('textarea');assert.equal(text.readOnly,true);assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.requests.at(-1).file,0);assert.equal(view.requests.at(-1).token,'one.go-token');
  view.render({...initial,version:2,reviewEditing:true});assert.equal(text.readOnly,false);
  text.value='saved text';text.oninput();control(view,'Save changes to working file (Command+S)').click();
  assert.equal(view.requests.at(-1).operation,'review-save');assert.equal(view.requests.at(-1).content,'saved text');
  view.render({...initial,version:3,reviewEditing:true,error:'The working file changed'});
  assert.equal(view.root.querySelector('textarea'),text);assert.equal(text.value,'saved text');assert.equal(text.readOnly,false);assert.match(view.root.querySelector('.br-edit-warning').textContent,/still here/);
});
test('successful save waits for edit guard release before replacing textarea',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  const text=view.root.querySelector('textarea');text.value='saved';control(view,'Save changes to working file (Command+S)').click();
  view.render({...initial,version:3,reviewEditing:true,review:{...initial.review,selected:payload('one.go',{newText:'saved'})}});
  assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.requests.at(-1).editing,false);assert.equal(view.root.querySelector('textarea'),text);
  view.render({...initial,version:3,reviewEditing:false,review:{...initial.review,selected:payload('one.go',{newText:'saved'})}});
  assert.equal(view.root.querySelector('textarea'),null);
});
test('dirty cancellation asks inside the editor and preserves draft until explicitly discarded',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  const text=view.root.querySelector('textarea');text.value='unsaved';control(view,'Cancel editing').click();assert.match(view.root.querySelector('.br-discard').textContent,/Discard/);
  control(view,'Keep editing').click();assert.equal(view.root.querySelector('.br-discard'),null);assert.equal(text.value,'unsaved');
});
test('no-final-newline marker and independent horizontal scroll controls remain available',()=>{
  const view=viewer();view.render(state({review:{baseRef:'main',base:'base',refs:['main'],selected:payload('one.go',{oldText:'old',newText:'new'})}}));
  assert.match(view.root.querySelector('.br-file-card').textContent,/No newline at end of original and current file/);
  const scrolls=view.root.querySelectorAll('.br-horizontal-scroll');assert.equal(scrolls.length,2);scrolls[0].scrollLeft=40;scrolls[0].onscroll();assert.equal(view.root.querySelector('.br-file-card').style['--br-left-scroll'],'-40px');
});

test('unexpected WebKit reload restores a window draft under the native edit guard',()=>{
  const storage=new Map(),initial=state(),first=viewer(storage);first.render(initial);control(first,'Edit current working file').click();first.render({...initial,version:2});first.render({...initial,version:2,reviewEditing:true});
  first.root.querySelector('textarea').value='recovered draft';first.root.querySelector('textarea').oninput();
  const restored=viewer(storage);restored.render({...initial,version:2,reviewEditing:true});
  assert.equal(restored.root.querySelector('textarea').value,'recovered draft');assert.equal(restored.root.querySelector('textarea').readOnly,false);
  assert.match(restored.root.querySelector('.br-notice').textContent,/restored/);
});
test('orphan edit guard without a recoverable draft is released so the window can close',()=>{
  const view=viewer();view.render(state({reviewEditing:true}));
  assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.requests.at(-1).editing,false);
});
test('CRLF files keep CRLF on explicit Save',()=>{
  const view=viewer(),initial=state({review:{baseRef:'main',base:'base',refs:['main'],selected:payload('one.go',{newText:'one\r\ntwo\r\n'})}});view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});view.render({...initial,version:2,reviewEditing:true});
  view.root.querySelector('textarea').value='one\nchanged\n';control(view,'Save changes to working file (Command+S)').click();
  assert.equal(view.requests.at(-1).content,'one\r\nchanged\r\n');
});
test('unchanged Save preserves raw source after textarea normalization and draft recovery',()=>{
  for(const text of ['one\r\ntwo\r\n','one\r\ntwo\n','one\rtwo\r']){
    const storage=new Map(),initial=state({review:{...state().review,selected:payload('one.go',{newText:text})}}),first=viewer(storage);
    first.render(initial);control(first,'Edit current working file').click();first.render({...initial,version:2});first.render({...initial,version:2,reviewEditing:true});
    assert.equal(first.root.querySelector('textarea').value,'one\ntwo\n');
    const recovered=viewer(storage);recovered.render({...initial,version:2,reviewEditing:true});
    assert.equal(recovered.root.querySelector('textarea').value,'one\ntwo\n');
    control(recovered,'Save changes to working file (Command+S)').click();assert.equal(recovered.requests.at(-1).content,text);
  }
});
test('custom Tab insertion cannot mutate a textarea before edit guard ACK or during Save',()=>{
  const view=viewer(),initial=state();view.render(initial);control(view,'Edit current working file').click();view.render({...initial,version:2});
  const text=view.root.querySelector('textarea');text.setRangeText=()=>{throw Error('mutated read-only editor');};
  text.onkeydown({key:'Tab',preventDefault(){}});
  view.render({...initial,version:2,reviewEditing:true});control(view,'Save changes to working file (Command+S)').click();text.onkeydown({key:'Tab',preventDefault(){}});
});

test('pending edit retries its file selection when a newer unrelated snapshot rejects the request',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);
  const mode=view.root.querySelector('.br-presentation');mode.value='continuous';mode.onchange();control(view,'Load diff for two.go').click();
  const second={...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}};view.render(second);
  control(view,'Edit current working file').click();assert.equal(view.requests.at(-1).file,0);
  view.render({...second,version:3});assert.equal(view.requests.at(-1).operation,'review-file');assert.equal(view.requests.at(-1).file,0);assert.equal(view.requests.at(-1).version,3);
  view.render({...initial,version:4});assert.equal(view.requests.at(-1).operation,'review-edit');assert.equal(view.root.querySelector('textarea').readOnly,true);
});
test('failed file reads do not automatically retry forever',()=>{
  const view=viewer(),initial=state({files:[{path:'one.go'},{path:'two.go'}]});view.render(initial);view.root.querySelectorAll('.br-file')[1].click();
  const failed={...initial,version:2,file:1,error:'File unavailable',review:{...initial.review,selected:null}};view.render(failed);view.render(failed);
  assert.equal(view.requests.length,1);control(view,'Load diff for two.go').click();assert.equal(view.requests.length,2);
});

test('a cached file reread preserves change navigation completed before the reply',()=>{
  const before=Array.from({length:80},(_,index)=>`line ${index+1}`),after=before.slice();
  after[19]='first replacement';after[59]='second replacement';
  const data=payload('one.go',{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:[
    {oldStart:20,oldLines:1,newStart:20,newLines:1,commits:[]},
    {oldStart:60,oldLines:1,newStart:60,newLines:1,commits:[]},
  ]});
  const initial=state({files:[{path:'one.go'},{path:'two.go'}],review:{...state().review,selected:data}}),view=viewer();
  view.render(initial);control(view,'Next file (Option+Right)').click();
  view.render({...initial,version:2,file:1,review:{...initial.review,selected:payload('two.go')}});
  control(view,'Previous file (Option+Left)').click();
  assert.equal(view.requests.at(-1).operation,'review-file');assert.equal(view.requests.at(-1).file,0);
  control(view,'Next change (Option+Down)').click();
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  // Model the browser's scroll position after revealing the second block. The
  // reread has identical content and must not put the reader back at the top.
  const content=view.root.querySelector('.br-content');content.scrollTop=900;
  view.render({...initial,version:3});
  assert.equal(view.root.querySelector('.br-current-change').dataset.change,'1');
  assert.equal(content.scrollTop,900);
});

test('selecting an uncached file reveals its first changed block when its text arrives',()=>{
  const before=Array.from({length:250},(_,index)=>`line ${index+1}`),after=before.slice();after[183]='replacement';
  const data=payload('two.go',{oldText:before.join('\n')+'\n',newText:after.join('\n')+'\n',hunks:[
    {oldStart:184,oldLines:1,newStart:184,newLines:1,commits:[]},
  ]});
  const initial=state({files:[{path:'one.go'},{path:'two.go'}]}),view=viewer();view.render(initial);
  control(view,'Next file (Option+Right)').click();
  assert.equal(view.root.querySelector('.br-code'),null);
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
  assert.equal(selectedPath(view),'one.go');assert.equal(view.requests.at(-1).file,0);
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
  assert.equal(selectedPath(view),'three.go');assert.equal(view.requests.at(-1).file,2);
  assert.equal(view.root.querySelector('.br-current-change'),null);
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
  assert.ok(control(view,'Load diff for two.go'));control(view,'Load diff for two.go').click();
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
