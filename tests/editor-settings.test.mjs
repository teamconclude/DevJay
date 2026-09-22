import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../frontend/editor-settings.js',import.meta.url),'utf8');
const {mountEditorSettings}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
class Element {
 constructor(tag){this.tag=tag;this.children=[];this.disabled=false;this.value='';}
 setAttribute(){}
 append(...items){this.children.push(...items);}
 replaceChildren(){this.children=[];}
}
globalThis.document={createElement:tag=>new Element(tag)};
const go={name:'GoLand',path:'/Applications/GoLand.app',pids:[42]};
const editor={apps:[go],choice:go.path,useProject:true};
function setup(){const root=new Element('div'),requests=[];const view=mountEditorSettings(root,r=>requests.push(r));return {...view,requests,editors:root.children[1].children[0].children[0],automatic:root.children[2].children[0],error:root.children[3]};}
test('global preference and independent automatic option render without per-Site selection',()=>{
 const v=setup();v.renderEditor({editor});
 assert.equal(v.editors.value,go.path);assert.equal(v.automatic.checked,true);assert.equal(v.editors.disabled,false);
 assert.equal(v.requests.length,0);
 v.automatic.checked=false;v.automatic.onchange();
 assert.deepEqual(v.requests,[{operation:'automatic',useProject:false}]);
 assert.equal(v.automatic.disabled,true);assert.equal(v.editors.disabled,true);
 v.renderEditor({editor:{...editor,useProject:false}});assert.equal(v.editors.value,go.path);assert.equal(v.automatic.checked,false);
});
test('editor selection and picker requests are global and re-enable after cancellation',()=>{
 const v=setup();v.renderEditor({editor});v.editors.value=go.path;v.editors.onchange();
 assert.deepEqual(v.requests.at(-1),{operation:'select',editor:go.path});
 v.renderEditor({editor});v.editors.value='__choose_app__';v.editors.onchange();
 assert.deepEqual(v.requests.at(-1),{operation:'browse',editor:''});
 v.renderEditor({editor:{...editor,busy:true}});assert.equal(v.editors.disabled,true);
 v.renderEditor({editor});assert.equal(v.editors.disabled,false);assert.equal(v.editors.value,go.path);
});
test('loading blocks changes and failures remain in Settings',()=>{
 const v=setup();v.renderEditor({editor:null});v.editors.onchange();assert.equal(v.requests.length,0);
 v.renderEditor({editor:{...editor,error:'Could not save'}});assert.equal(v.error.textContent,'Could not save');assert.equal(v.error.hidden,false);
 v.renderEditor({editor});assert.equal(v.error.hidden,true);
});
