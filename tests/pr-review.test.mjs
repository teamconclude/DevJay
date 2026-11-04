import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
const source=await readFile(new URL('../cmd/devjay/frontend/pr-review.js',import.meta.url),'utf8');
function viewer(storage=new Map(),failure=false) {
  let document;
  class Element {
    constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.className='';this.scrollTop=0;this.scrollLeft=0;this.style={setProperty(key,value){this[key]=value;}};this.value='';this.checked=false;this.disabled=false;this.classList={contains:c=>this.className.split(' ').includes(c),add:c=>this.classList.toggle(c,true),toggle:(c,on)=>{const values=new Set(this.className.split(' ').filter(Boolean));on?values.add(c):values.delete(c);this.className=[...values].join(' ');}};}
    append(...nodes){for(const node of nodes){if(node.tag==='fragment'){this.append(...node.children.slice());continue;}node.remove();node.parent=this;this.children.push(node);}}
    get parentNode(){return this.parent||null;}
    get nextSibling(){return this.parent?.children[this.parent.children.indexOf(this)+1]||null;}
    insertBefore(node,before){node.remove();node.parent=this;const i=this.children.indexOf(before);this.children.splice(i<0?this.children.length:i,0,node);}
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
    click(event={}){if(!this.disabled)this.onclick?.(event);}
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
  const frames=new Map();let frameID=0,closed=0;
  const context=vm.createContext({document,requestAnimationFrame:fn=>{frames.set(++frameID,fn);return frameID;},cancelAnimationFrame:id=>frames.delete(id),localStorage:{getItem:key=>storage.get(key)||null,setItem:(key,value)=>{if(failure)throw new Error('quota');storage.set(key,value);}}});
  vm.runInContext(source.replace(/^export /gm,''),context);
  const frame=new Element('section');frame.className='branch-review';const footer=new Element('footer');footer.className='br-footer';const card=new Element('article');card.className='br-file-card';root.append(frame);frame.append(card,footer);
  const requests=[],api=context.createPRReview(root,data=>{requests.push(JSON.parse(JSON.stringify(data)));return true;},()=>closed++);
  const gutter=(side,line,event={})=>{let n=card.querySelectorAll('.br-comment-gutter').find(n=>n.dataset.prSide===side&&Number(n.dataset.prLine)===line);if(!n){const row=new Element('div');row.className='br-row';n=new Element('span');row.append(n);card.append(row);api.decorateGutter(n,{path:'new.go',side,line,text:'before\nnew\nafter\n'});}n.click(event);return n;};
  return {root,frame,card,gutter,document,requests,storage,frames,api,closed:()=>closed,flush(){const pending=[...frames.values()];frames.clear();for(const fn of pending)fn();}};
}

const button=(v,label)=>v.root.querySelectorAll('button').find(n=>n.attrs['aria-label']===label);
const field=(v,label)=>v.root.querySelectorAll('textarea').find(n=>n.attrs['aria-label']===label);
const fixture=(extra={})=>({token:'token',repo:'team/project',number:12,url:'https://github.com/team/project/pull/12',head:'a'.repeat(40),base:'b'.repeat(40),viewer:'reviewer',author:'author',files:[{path:'unrelated.go'}],...extra});
const state=()=>({path:'/repo',branch:'feature',reviewPRs:[{number:12}],files:[{path:'new.go'}]});
function open(v,s=fixture()){v.pending=s.pending||null;v.api.sync(state());v.api.receive({loadID:1,snapshot:s});}
function resolve(v,error=''){const a=v.requests.at(-1).prAnchor;assert.ok(a);v.api.receive({loadID:1,anchor:{id:a.id,error,location:error?null:{path:a.path,side:a.side,line:a.line,...(a.startLine&&a.startLine!==a.line?{start_line:a.startLine,start_side:a.side}:{})}}});}
function addComment(v,text='Please simplify this.',side='RIGHT',line=2){v.gutter(side,line);resolve(v);const n=field(v,'Review comment (Markdown supported)');n.value=text;n.oninput();button(v,'Save inline comment to draft review').click();if(v.requests.at(-1).prComment)confirmComment(v);}
function finish(v){button(v,'Finish PR review').click();}


function confirmComment(v){
 const c=v.requests.at(-1).prComment;assert.ok(c);
 v.pending||={id:70,nodeID:'PENDING',head:'a'.repeat(40),body:'',comments:[],version:'1'};
 if(c.action==='delete')v.pending.comments=v.pending.comments.filter(n=>n.id!==c.commentID);
 else if(c.action==='update'){const n=v.pending.comments.find(n=>n.id===c.commentID);Object.assign(n,c.comment,{requestID:c.requestID});}
 else v.pending.comments.push({...c.comment,id:100+v.pending.comments.length,requestID:c.requestID});
 v.pending={...v.pending,version:String(Number(v.pending.version)+1)};
 v.api.receive({loadID:1,pending:v.pending,comment:{requestID:c.requestID,done:true}});
}
const existing=()=>({id:70,nodeID:'PENDING',head:'a'.repeat(40),body:'Summary started on GitHub',version:'1',comments:[{id:100,path:'old.go',side:'RIGHT',line:10,body:'Existing inline feedback'}]});

test('pending review data does not replace the existing file list or diff',()=>{
 const v=viewer(),card=v.card;open(v,fixture({pending:existing()}));v.api.sync(state());assert.equal(v.requests.length,1);assert.equal(v.root.querySelector('.br-file-card'),card);assert.equal(v.root.querySelector('.pr-files'),null);
 assert.match(v.root.querySelector('.pr-draft-list').textContent,/Existing inline feedback/);assert.equal(v.root.querySelector('.pr-submit-panel').hidden,false);assert.equal(field(v,'Review summary (Markdown supported)').value,'Summary started on GitHub');
});
test('Add saves a private inline comment to the pending review, and submission uses that review ID',()=>{
 const v=viewer();open(v);addComment(v);assert.equal(v.requests.at(-1).operation,'pr-review-comment');assert.equal(v.requests.at(-1).prComment.action,'add');assert.equal(v.requests.at(-1).prComment.reviewID,0);
 assert.match(v.root.querySelector('.pr-inline-draft').textContent,/Please simplify/);assert.equal(v.root.querySelector('.pr-draft-list').querySelectorAll('.pr-draft').length,1);
 addComment(v,'Second feedback','RIGHT',3);assert.equal(v.requests.at(-1).prComment.reviewID,70);finish(v);button(v,'Submit review to GitHub').click();
 const req=v.requests.at(-1);assert.equal(req.operation,'pr-review-submit');assert.equal(req.prSubmission.reviewID,70);assert.equal(req.prSubmission.pendingVersion,v.pending.version);assert.deepEqual(req.prSubmission.comments,[]);assert.equal(req.prSubmission.body,'');
 v.api.receive({loadID:1,pending:null,requestID:req.prSubmission.requestID,receiptID:70});assert.match(v.root.querySelector('.pr-notice').textContent,/Review submitted/);
});
test('existing pending review comments are preserved while new comments join it',()=>{
 const v=viewer();open(v,fixture({pending:existing()}));addComment(v);assert.equal(v.requests.at(-1).prComment.reviewID,70);assert.equal(v.pending.comments.length,2);assert.match(v.root.querySelector('.pr-draft-list').textContent,/Existing inline feedback/);
 const n=field(v,'Review summary (Markdown supported)');n.value='Final summary';n.oninput();finish(v);button(v,'Submit review to GitHub').click();assert.equal(v.requests.at(-1).prSubmission.body,'Final summary');
});
test('saved comments use rounded-card header/body/actions and retain exact range and side',()=>{
 const v=viewer();open(v);v.gutter('RIGHT',1);v.gutter('RIGHT',3,{shiftKey:true});resolve(v);const n=field(v,'Review comment (Markdown supported)');n.value='Range';n.oninput();button(v,'Save inline comment to draft review').click();confirmComment(v);
 const first=v.requests.at(-1).prComment.comment;assert.equal(first.start_line,1);assert.equal(first.line,3);
 const card=v.root.querySelector('.pr-draft');assert.match(card.querySelector('.pr-comment-header').textContent,/new.goNew · 1–3/);assert.equal(card.querySelector('.pr-comment-body').textContent,'Range');assert.ok(card.querySelector('.pr-comment-actions'));
 addComment(v,'Deletion feedback','LEFT',2);assert.equal(v.requests.at(-1).prComment.comment.side,'LEFT');
});
test('pending comment edits and deletes address the saved comment instead of making another review',()=>{
 const v=viewer();open(v);addComment(v);button(v,'Edit draft comment on new.go · new 2').click();const n=field(v,'Review comment (Markdown supported)');n.value='Revised';n.oninput();button(v,'Save inline comment to draft review').click();
 assert.equal(v.requests.at(-1).prComment.action,'update');assert.equal(v.requests.at(-1).prComment.commentID,100);assert.equal(v.requests.at(-1).prComment.originalBody,'Please simplify this.');confirmComment(v);
 button(v,'Delete draft comment on new.go · new 2').click();assert.equal(v.requests.at(-1).prComment.action,'delete');confirmComment(v);assert.equal(v.pending.comments.length,0);
});
test('unpublished lines are rejected and stale mapping results cannot move a newer comment',()=>{
 const v=viewer();open(v);v.gutter('RIGHT',1);const id=v.requests.at(-1).prAnchor.id;v.gutter('RIGHT',2);v.api.receive({loadID:1,anchor:{id,location:{path:'new.go',side:'RIGHT',line:99}}});resolve(v,'Unpublished line');const n=field(v,'Review comment (Markdown supported)');n.value='Keep my thought';n.oninput();assert.equal(button(v,'Save inline comment to draft review').disabled,true);
});
test('unfinished text survives recreation without posting and cannot be overwritten by a new location',()=>{
 const storage=new Map(),v=viewer(storage);open(v);v.gutter('RIGHT',2);resolve(v);const n=field(v,'Review comment (Markdown supported)');n.value='Unfinished';n.oninput();v.api.destroy();const next=viewer(storage);open(next);finish(next);button(next,'Continue unfinished PR comment').click();next.gutter('RIGHT',3);assert.equal(field(next,'Review comment (Markdown supported)').value,'Unfinished');assert.equal(next.requests.filter(r=>r.prComment).length,0);
});
test('uncertain comment saves survive restart and require read-only checking before explicit retry',()=>{
 const storage=new Map(),v=viewer(storage);open(v);v.gutter('RIGHT',2);resolve(v);const n=field(v,'Review comment (Markdown supported)');n.value='Save this';n.oninput();button(v,'Save inline comment to draft review').click();const change=v.requests.at(-1).prComment;
 const next=viewer(storage);open(next);button(next,'Check pending comment status').click();assert.equal(next.requests.at(-1).operation,'pr-review-comment-check');assert.equal(next.requests.at(-1).prComment.requestID,change.requestID);
 next.api.receive({loadID:1,comment:{requestID:change.requestID,unknown:true,error:'Unknown'}});
 const check=next.root.querySelectorAll('input').find(n=>n.attrs['aria-label']==='I checked GitHub and the comment change was not saved');check.checked=true;check.onchange();button(next,'Retry pending comment change').click();assert.equal(next.requests.at(-1).operation,'pr-review-comment-retry');
});
test('unknown final submission only reconciles on restart',()=>{
 const storage=new Map(),v=viewer(storage);open(v);addComment(v);finish(v);button(v,'Submit review to GitHub').click();const req=v.requests.at(-1).prSubmission;
 // The server may already have published it, so there is no pending review.
 const next=viewer(storage);open(next);finish(next);button(next,'Submit review to GitHub').click();assert.equal(next.requests.at(-1).operation,'pr-review-check');assert.equal(next.requests.at(-1).prSubmission.requestID,req.requestID);assert.equal(next.requests.at(-1).prSubmission.reviewID,70);assert.equal(next.requests.at(-1).prSubmission.pendingVersion,req.pendingVersion);
});
test('editing an existing offscreen comment survives diff and review refreshes',()=>{
 const v=viewer();open(v,fixture({pending:existing()}));button(v,'Edit draft comment on old.go · new 10').click();const text=field(v,'Review comment (Markdown supported)');text.value='Revised existing feedback';text.oninput();v.api.afterRender();assert.equal(text.parentNode?.parentNode?.parentNode,v.root.querySelector('.pr-draft-list'));
 v.api.receive({loadID:1,pending:{...existing(),version:'2'}});assert.equal(field(v,'Review comment (Markdown supported)'),text);assert.equal(v.document.activeElement,text);assert.equal(text.value,'Revised existing feedback');
});
test('saving an earlier local draft cannot discard a different unfinished comment',()=>{
 const old={requestID:'old-request-123456789',body:'',event:'COMMENT',comments:[{id:'local-comment',path:'old.go',side:'RIGHT',line:1,body:'Earlier local comment'}],revision:'saved'};
 const storage=new Map([['devjay-pr-review:team/project#12',JSON.stringify({['a'.repeat(40)+':'+ 'b'.repeat(40)]:old})]]),v=viewer(storage);open(v);v.gutter('RIGHT',2);resolve(v);const n=field(v,'Review comment (Markdown supported)');n.value='Still writing';n.oninput();finish(v);button(v,'Save local comment on old.go · new 1 to GitHub').click();button(v,'Delete draft comment on old.go · new 1').click();assert.equal(v.requests.filter(r=>r.prComment).length,0);assert.equal(n.value,'Still writing');assert.match(v.root.querySelector('.pr-draft-list').textContent,/Earlier local comment/);
});
test('storage failures cannot save remote comments and keep the native draft guard',()=>{
 const failed=viewer(new Map(),true);open(failed);addComment(failed);assert.equal(failed.requests.filter(r=>r.prComment).length,0);assert.ok(failed.requests.some(r=>r.operation==='pr-review-draft'&&r.editing));
});
test('own approvals stay disabled and empty reviews require a summary',()=>{
 const v=viewer();open(v,fixture({viewer:'AUTHOR'}));finish(v);assert.equal(v.root.querySelector('.pr-action').children[1].disabled,true);button(v,'Submit review to GitHub').click();assert.equal(v.requests.length,1);
});
