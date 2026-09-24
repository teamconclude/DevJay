import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source = await readFile(new URL('../cmd/devjay/frontend/ui-ready.js', import.meta.url), 'utf8');
const {uiReady} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

test('each document acknowledges its first rendered snapshot only once', async () => {
  const sent = [];
  const ready = uiReady(async id => sent.push(id), 'first-document');
  await Promise.all([ready(), ready()]);
  await ready();
  const replacement = uiReady(async id => sent.push(id), 'replacement-document');
  await replacement();
  assert.deepEqual(sent, ['first-document', 'replacement-document']);
});

test('a failed bridge call can retry on the next rendered snapshot', async () => {
  let attempts = 0;
  const ready = uiReady(async () => { if (++attempts === 1) throw Error('bridge unavailable'); }, 'document');
  await ready(); await ready(); await ready();
  assert.equal(attempts, 2);
});
