import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const source = await readFile(new URL('../frontend/panel-drag.js', import.meta.url), 'utf8');
const { insertionIndex, snap } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const rect = (left, top) => ({left, top, width:216, height:152, bottom:top+152});
test('reordering finds positions before, between, and after horizontal cards', () => {
 const cards = [rect(0,0),rect(224,0),rect(448,0)];
 assert.equal(insertionIndex(cards, 2, 50, false), 0);
 assert.equal(insertionIndex(cards, 230, 50, false), 1);
 assert.equal(insertionIndex(cards, 660, 50, false), 3);
});
test('vertical order ignores horizontal position', () => {
 const cards = [rect(0,0),rect(0,160),rect(0,320)];
 assert.equal(insertionIndex(cards, 200, 10, true), 0);
 assert.equal(insertionIndex(cards, 10, 250, true), 2);
 assert.equal(insertionIndex(cards, 200, 480, true), 3);
});
test('custom positions snap to an invisible 8px grid within safe bounds', () => {
 assert.equal(snap(19),16); assert.equal(snap(21),24);
 assert.equal(snap(-40),0); assert.equal(snap(100050),100000);
});
