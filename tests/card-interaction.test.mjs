import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../cmd/devjay/frontend/card-interaction.js',import.meta.url),'utf8');
const {installCardActivation}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
test('panel body clicks activate once, while controls, drags, and text selection do not', () => {
 let click, selected=false;
 const activations=[];
 globalThis.window={getSelection:()=>({isCollapsed:!selected})};
 try {
  installCardActivation({addEventListener:(_,fn)=>{click=fn;}},id=>activations.push(id));
  const fire=(control=false,copyable=false,extra={})=>click({button:0,defaultPrevented:false,target:{closest(selector){
   if(selector==='.card') return {dataset:{id:'site'}};
   if(selector==='.copyable') return copyable ? {} : null;
   return control ? {} : null;
  }},...extra});
  fire(); fire(false,true);
  assert.deepEqual(activations,['site','site']);
  fire(true); // Heading, Settings cog, Suggested actions, or move handle.
  fire(false,false,{button:2}); fire(false,false,{defaultPrevented:true});
  selected=true; fire(false,true);
  assert.equal(activations.length,2);
 } finally { delete globalThis.window; }
});
