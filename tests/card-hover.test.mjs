import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source = await readFile(new URL('../cmd/devjay/frontend/card-hover.js', import.meta.url), 'utf8');
const {installCardHover} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
test('native hover selects the card under an inactive pointer and clears on exit', () => {
  const handlers = {}, classes = [new Set(), new Set()];
  const cards = classes.map(set => ({classList:{add:c=>set.add(c),remove:c=>set.delete(c)}}));
  const target = {innerWidth:400,innerHeight:200,addEventListener:(type,fn)=>handlers[type]=fn};
  const document = {addEventListener:(type,fn)=>handlers[type]=fn,elementFromPoint(x,y) {
    if(x<0 || y<0 || x>=400 || y>=200) return null;
    return {closest:()=>cards[x<200 ? 0 : 1]};
  }};
  installCardHover(target,document);
  handlers['devjay:native-hover']({detail:{x:.25,y:.5}});
  assert.ok(classes[0].has('pointer-hover')); assert.equal(classes[1].size,0);
  handlers['devjay:native-hover']({detail:{x:.75,y:.5}});
  assert.equal(classes[0].size,0); assert.ok(classes[1].has('pointer-hover'));
  handlers['devjay:native-hover']({detail:{x:-1,y:-1}});
  assert.equal(classes[1].size,0);
  handlers.pointermove({clientX:100,clientY:100}); assert.ok(classes[0].has('pointer-hover'));
  handlers['devjay:native-hover']({detail:{x:-1,y:-1}}); assert.equal(classes[0].size,0);
});

test('moving between statuses in one inactive card updates just the hovered status',()=>{
 const handlers={}, makeNode=()=>{const classes=new Set();return {classes,classList:{add:c=>classes.add(c),remove:c=>classes.delete(c)}};};
 const card=makeNode(),statuses=[makeNode(),makeNode()];
 const target={innerWidth:200,innerHeight:100,addEventListener:(name,fn)=>handlers[name]=fn};
 const document={addEventListener:(name,fn)=>handlers[name]=fn,elementFromPoint(x,y){
  if(x<0||y<0)return null;
  return {closest:selector=>selector==='.status-link'?(y<50?statuses[x<100?0:1]:null):card};
 }};
 installCardHover(target,document);
 handlers['devjay:native-hover']({detail:{x:.25,y:.25}});
 assert.ok(card.classes.has('pointer-hover'));assert.ok(statuses[0].classes.has('status-hover'));
 handlers['devjay:native-hover']({detail:{x:.75,y:.25}});
 assert.equal(statuses[0].classes.size,0);assert.ok(statuses[1].classes.has('status-hover'));
 handlers.pointermove({clientX:150,clientY:75});
 assert.equal(statuses[1].classes.size,0);assert.ok(card.classes.has('pointer-hover'));
 handlers['devjay:native-hover']({detail:{x:-1,y:-1}});assert.equal(card.classes.size,0);
});
