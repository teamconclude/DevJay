import test from 'node:test';
import assert from 'node:assert/strict';
import {cardMenuTarget, installCardMenu} from '../cmd/devjay/frontend/card-menu.js';

function target(content, id = 'site-a') {
  return {closest: selector => selector === '.card, .site-detail' ? {dataset:{id},getBoundingClientRect:()=>({left:0,top:4,width:216,height:152})} : content && {dataset:content},
    getBoundingClientRect: () => ({left:10,top:20,width:100,height:16})};
}
test('right-click targets category, heading, PR or app, with desktop navigation on the background', () => {
  assert.deepEqual(cardMenuTarget(target(null)), {siteID:'site-a',kind:'site',number:0,bundleID:''});
  for (const kind of ['auto','edits','branch','local','remote']) assert.equal(cardMenuTarget(target({viewKind:kind})).kind,kind);
  assert.equal(cardMenuTarget(target({prNumber:'1234'})).number,1234);
  assert.equal(cardMenuTarget(target({appBundle:'com.jetbrains.goland'})).bundleID,'com.jetbrains.goland');
  assert.deepEqual(cardMenuTarget(target({viewKind:'arbitrary',prNumber:'NaN'})), {siteID:'site-a',kind:'site',number:0,bundleID:''});
  assert.equal(cardMenuTarget(target(null,'')),null);
});
test('mouse and keyboard menus suppress the default menu and capture their own target', () => {
  const handlers={}, requests=[];
  installCardMenu({addEventListener:(name,fn)=>handlers[name]=fn}, request=>requests.push(request));
  let prevented=0,stopped=0;
  const event={target:target({viewKind:'remote'}),clientX:128.2,clientY:72.9,
    preventDefault:()=>prevented++,stopPropagation:()=>stopped++};
  handlers.contextmenu(event);
  handlers.keydown({...event,key:'F10',shiftKey:true,target:target({prNumber:'8'},'site-b')});
  handlers.keydown({...event,key:'Enter'});
  assert.deepEqual(requests,[{siteID:'site-a',kind:'remote',number:0,bundleID:'',anchor:{x:0,y:4,width:216,height:152},x:128,y:73},
    {siteID:'site-b',kind:'site',number:8,bundleID:'',anchor:{x:0,y:4,width:216,height:152},x:30,y:36}]);
  assert.equal(prevented,2);assert.equal(stopped,2);
});
