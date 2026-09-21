import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source = await readFile(new URL('../frontend/card-hover.js', import.meta.url), 'utf8');
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
  handlers.pointerleave(); assert.equal(classes[0].size,0);
});
