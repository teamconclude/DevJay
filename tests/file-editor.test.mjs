import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../cmd/devjay/frontend/file-editor.js',import.meta.url),'utf8');
const {mountFileEditor}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
class Element {
 constructor(){this.children=[];this.disabled=false;this.value='';}
 setAttribute(){}
 append(...items){this.children.push(...items);}
 replaceChildren(){this.children=[];}
}
globalThis.document={createElement:()=>new Element()};
function setup(){const root=new Element(),requests=[];const render=mountFileEditor(root,r=>requests.push(r));return {render,requests,open:root.children[0],message:root.children[1]};}
const go={name:'GoLand',path:'/Applications/GoLand.app',projectOpen:true,pids:[42]};
const state={version:8,loading:false,editor:{apps:[go],choice:'',resolved:go,canOpen:true}};
test('renders automatic exact project match and emits only versioned open intent',()=>{
 const {render,requests,open}=setup();render(state);
 assert.equal(open.textContent,'Open in GoLand');assert.equal(open.disabled,false);
 assert.equal(requests.length,0);
 open.onclick();open.onclick();assert.deepEqual(requests,[{version:8,operation:'file-open'}]);
});
test('same-version state updates disable unavailable files and show errors',()=>{
 const {render,requests,open,message}=setup();render(state);
 render({...state,editor:{...state.editor,canOpen:false,note:'File not present'}});
 open.onclick();assert.equal(requests.length,0);assert.equal(message.textContent,'File not present');
 render({...state,editor:{...state.editor,error:'Editor unavailable'}});assert.equal(message.textContent,'Editor unavailable');
 render({...state,loading:true});assert.equal(open.disabled,true);
 render({...state,editor:{...state.editor,busy:true}});assert.equal(open.textContent,'Opening…');assert.equal(open.disabled,true);
});
