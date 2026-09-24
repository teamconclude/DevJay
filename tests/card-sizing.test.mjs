import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../cmd/devjay/frontend/card-sizing.js',import.meta.url),'utf8');
const {requiredContentHeight}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
test('card overflow grows the viewport by the missing content height',()=>{
 assert.equal(requiredContentHeight(174,220,157),237);
 assert.equal(requiredContentHeight(500,800,470),830);
 // Preserve extra space and tolerate subpixel rounding.
 assert.equal(requiredContentHeight(300,270,270),null);
 assert.equal(requiredContentHeight(300,271,270),null);
 assert.equal(requiredContentHeight(0,100,80),null);
 assert.equal(requiredContentHeight(174,NaN,150),null);
});
