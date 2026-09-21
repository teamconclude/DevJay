import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const source = await readFile(new URL('../frontend/card-status.js', import.meta.url), 'utf8');
const { cardStatus } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const clean = {summary:'Up to date', ahead:0, behind:0, changes:0, github:'No open pull request'};
test('clean default branches are quiet; feature branches show the short no-PR label', () => {
 assert.equal(cardStatus({...clean,isMainBranch:true}).pr,'');
 assert.equal(cardStatus(clean).pr,'No open PR');
 assert.equal(cardStatus(clean).quiet,'In sync');
 assert.equal(cardStatus({...clean,isMainBranch:true,github:'PR #3 · Approved'}).pr,'PR #3 · Approved');
});
test('local edits, outgoing and incoming commits stay distinct without duplicate summaries', () => {
 const s = cardStatus({...clean,summary:'Diverged · 8 ahead / 4 behind',ahead:8,behind:4,changes:2});
 assert.deepEqual(s.local.map(n => n.value),[8,2]);
 assert.deepEqual(s.remote.map(n => n.value),[4]);
 assert.equal(s.alert,null); assert.equal(s.quiet,'');
});
test('conflicts and rewritten history retain prominent warnings', () => {
 const s=cardStatus({...clean,summary:'Merge conflicts',conflicts:2,changes:4,rewritten:true,behind:1});
 assert.equal(s.local[0].tone,'error'); assert.equal(s.local[0].label,'conflicts');
 assert.equal(s.remote[1].label,'Rewritten'); assert.equal(s.quiet,'');
});
test('stale, unavailable, and special Git states never appear freshly synchronized', () => {
 for (const remoteNote of ['Remote not checked yet','Remote status is stale','Remote check failed · cached comparison']) {
  const s=cardStatus({...clean,remoteNote}); assert.equal(s.quiet,''); assert.ok(s.freshness);
 }
 const s=cardStatus({...clean,summary:'No upstream branch',tone:'muted'});
 assert.equal(s.alert.text,'No upstream branch'); assert.equal(s.quiet,'');
 assert.equal(cardStatus({...clean,github:'PR #9 · Changes requested'}).prTone,'warning');
});
