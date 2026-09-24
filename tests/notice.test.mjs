import test from 'node:test';
import assert from 'node:assert/strict';
import {nextNotice} from '../cmd/devjay/frontend/notice.js';

test('a quit snapshot produces non-error notice text', () => {
  const notice = nextNotice(undefined, 'snapshot', {quitMessage:'Finishing the action…'});
  assert.deepEqual(notice, {source:'snapshot', text:'Finishing the action…', error:false});
});

test('a later empty snapshot clears its previous notice', () => {
  for (const value of [{quitMessage:'Finishing the action…'}, {error:'Could not read Sites'}]) {
    const notice = nextNotice(undefined, 'snapshot', value);
    assert.deepEqual(nextNotice(notice, 'snapshot', {}), {source:'snapshot', text:'', error:false});
  }
});

test('a bridge error survives unrelated snapshots and clears on bridge success', () => {
  const notice = nextNotice(undefined, 'bridge', {text:'Could not save Sites', error:true});
  const afterSnapshot = nextNotice(notice, 'snapshot', {});
  assert.deepEqual(afterSnapshot, {source:'bridge', text:'Could not save Sites', error:true});
  assert.deepEqual(nextNotice(afterSnapshot, 'bridge', {text:'', error:false}), {source:'bridge', text:'', error:false});
});

test('a snapshot error sets the error flag', () => {
  assert.deepEqual(nextNotice(undefined, 'snapshot', {error:'Could not read Sites'}), {
    source:'snapshot', text:'Could not read Sites', error:true,
  });
});

test('bridge success leaves an active snapshot notice intact', () => {
  for (const value of [{quitMessage:'Finishing the action…'}, {error:'Could not read Sites'}]) {
    const notice = nextNotice(undefined, 'snapshot', value);
    assert.deepEqual(nextNotice(notice, 'bridge', {text:'', error:false}), notice);
  }
});

test('a quit message takes precedence over a simultaneous snapshot error', () => {
  assert.deepEqual(nextNotice(undefined, 'snapshot', {quitMessage:'Finishing the action…', error:'An earlier error'}), {
    source:'snapshot', text:'Finishing the action…', error:false,
  });
});

test('a new nonempty message takes ownership from the other source', () => {
  const snapshot = nextNotice(undefined, 'snapshot', {error:'Could not read Sites'});
  const bridge = nextNotice(snapshot, 'bridge', {text:'Could not save Sites', error:true});
  assert.equal(bridge.source, 'bridge');
  assert.equal(bridge.text, 'Could not save Sites');
  assert.deepEqual(nextNotice(bridge, 'snapshot', {}), bridge);
  const quit = nextNotice(bridge, 'snapshot', {quitMessage:'Finishing the action…'});
  assert.deepEqual(quit, {source:'snapshot', text:'Finishing the action…', error:false});
});
