import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const source = await readFile(new URL('../cmd/devjay/frontend/card-status.js', import.meta.url), 'utf8');
const { cardStatus } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const clean = {summary:'Up to date', ahead:0, behind:0, changes:0, github:'No open pull request'};
test('branches without a PR have no PR label', () => {
 assert.equal(cardStatus({...clean,isMainBranch:true}).pr,'');
 assert.equal(cardStatus(clean).pr,'');
 assert.equal(cardStatus(clean).quiet,'In sync');
 assert.equal(cardStatus({...clean,isMainBranch:true,github:'PR #3 · Approved'}).pr,'PR #3 · Approved');
});
test('local edits, outgoing and incoming commits stay distinct without duplicate summaries', () => {
 const s = cardStatus({...clean,summary:'Diverged · 8 ahead / 4 behind',ahead:8,behind:4,changes:2});
 assert.deepEqual(s.local.map(n => n.value),[8,2]);
 assert.deepEqual(s.remote.map(n => n.value),[4]);
 assert.equal(s.alert.text,'History differs'); assert.equal(s.quiet,'');
});
test('diverged branch counts exclude inherited default history without claiming a safe rebase', () => {
 const git={...clean,summary:'Diverged · 1131 ahead / 11 behind',ahead:1131,behind:11,changes:2,branchWork:{local:11,inherited:1120,base:'origin/develop'}};
 const s=cardStatus(git);
 assert.deepEqual(s.local.map(n=>[n.value,n.label]),[[11,'local only'],[2,'edits']]);
 assert.equal(s.remote[0].label,'remote only');assert.equal(s.remote[0].value,11);
 assert.equal(s.alert.text,'History differs');assert.equal(s.alert.publishRebased,undefined);
 assert.match(s.alert.title,/1120 additional commits already on origin\/develop/);
 assert.match(s.alert.title,/manually/);
 const unavailable=cardStatus({...git,branchWork:null});
 assert.equal(unavailable.local[0].value,1131);assert.equal(unavailable.local[0].label,'local only');
 const allInherited=cardStatus({...git,branchWork:{...git.branchWork,local:0,inherited:1131}});
 assert.equal(allInherited.local.length,1);assert.equal(allInherited.local[0].label,'edits');
 assert.equal(allInherited.remote[0].value,11);assert.equal(allInherited.alert.text,'History differs');
});
test('branches without an upstream show counts against an explicit remote comparison', () => {
 const s=cardStatus({...clean,summary:'No upstream branch',changes:1,comparison:{tracking:'origin/main',ahead:3,behind:2},base:{tracking:'origin/main',branch:'main',behind:2}});
 assert.deepEqual(s.local.map(n=>n.value),[3,1]);
 assert.equal(s.local[0].view,'branch');
 assert.match(s.local[0].title,/origin\/main/);
 assert.deepEqual(s.remote.map(n=>n.value),[2]);
 assert.equal(s.base,undefined);
 assert.equal(s.alert.text,'No upstream branch');
 assert.equal(s.quiet,'');
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

test('review findings replace the generic PR label with separate severity badges', () => {
 const s = cardStatus({...clean,github:'PR #2812 · Review required',reviews:[{number:2812,blockers:1,warnings:7,detail:'Latest reported findings'}]});
 assert.equal(s.pr,'');
 assert.equal(s.reviews[0].label,'PR #2812');
 assert.deepEqual(s.reviews[0].values.map(x=>[x.symbol,x.value,x.label,x.tone]),[['⊗',1,'blocker','error'],['⚠︎',7,'warnings','warning']]);
 assert.match(s.reviews[0].title,/Review required/);
});
test('stale reviews retain muted historical counts and their reason', () => {
 const s = cardStatus({...clean,github:'PR #2812 · Review required',reviews:[{number:2812,blockers:0,warnings:3,earlier:true,detail:'Latest report on an earlier commit'}]});
 assert.equal(s.pr,'');
 assert.deepEqual(s.reviews[0].values.map(x=>[x.value,x.label,x.tone]),[[3,'warnings','muted']]);
 assert.equal(s.reviews[0].note,'Changes since review');
 const blocker = cardStatus({...clean,github:'PR #2 · Approved',reviews:[{number:2,blockers:1,warnings:7,earlier:true}]});
 assert.deepEqual(blocker.reviews[0].values.map(x=>x.value),[1,7]);
 const noFindings = cardStatus({...clean,github:'PR #2 · Approved',reviews:[{number:2,blockers:0,warnings:0,earlier:true}]});
 assert.equal(noFindings.reviews[0].note,'Changes since review');
 assert.equal(noFindings.pr,'PR #2 · Approved');
 assert.equal(noFindings.reviews[0].values[0].label,'No findings reported');
 assert.equal(noFindings.reviews[0].values[0].tone,'muted');
 assert.equal(noFindings.reviews[0].stale,true);
 assert.match(noFindings.reviews[0].title,/Stale review/);
 assert.deepEqual(blocker.reviews[0].values.map(x=>x.tone),['muted','muted']);
});
test('unavailable review freshness preserves findings without claiming a current clean review', () => {
 const review={number:2,blockers:0,warnings:0,freshnessUnavailable:true,detail:'PR changes could not be checked'};
 const s=cardStatus({...clean,github:'PR #2 · Review required',reviews:[review]});
 assert.equal(s.reviews[0].note,'Review freshness unavailable');
 assert.equal(s.reviews[0].stale,false);
 assert.equal(s.reviews[0].values[0].label,'No findings reported');
 assert.equal(s.reviews[0].values[0].symbol,'');
 const findings=cardStatus({...clean,github:'PR #2 · Review required',reviews:[{...review,blockers:1,warnings:3}]});
 assert.deepEqual(findings.reviews[0].values.map(n=>n.value),[1,3]);
 const changed=cardStatus({...clean,github:'PR #2 · Review required',reviews:[{...review,earlier:true}]});
 assert.equal(changed.reviews[0].note,'Changes since review');
});
test('review change notes distinguish PR edits from pushed commits', () => {
 const report = {number:2822,blockers:0,warnings:0,earlier:true};
 for (const changeNote of [
  'Commits changed since review',
  'PR title changed since review',
  'PR description changed since review',
  'PR title and description changed since review',
  'Commits and PR details changed since review',
 ]) {
  const s = cardStatus({...clean,github:'PR #2822 · Open',reviews:[{...report,changeNote}]});
  assert.equal(s.reviews[0].note,changeNote);
  assert.equal(s.reviews[0].stale,true);
  assert.ok(s.reviews[0].title.includes(changeNote));
  assert.equal(s.reviews[0].values[0].label,'No findings reported');
  assert.equal(s.reviews[0].values[0].symbol,'');
 }
});
test('unavailable reviews hide counts; current clean reports retain their clean indicator', () => {
 const s = cardStatus({...clean,github:'PR #2 · Approved',reviews:[{number:2,blockers:1,warnings:7,unavailable:true,detail:'Report detail'}]});
 assert.equal(s.reviews.length,0); assert.equal(s.prTone,'warning'); assert.match(s.pr,/Review unavailable/);
 const cleanReview = cardStatus({...clean,github:'PR #2 · Review required',reviews:[{number:2,blockers:0,warnings:0,detail:'Explicit clean report'}]});
 assert.equal(cleanReview.reviews[0].values[0].label,'No findings');
 assert.equal(cleanReview.reviews[0].note,'');
 assert.match(cleanReview.reviews[0].title,/Review required/);
 assert.equal(cardStatus({...clean,github:'PR #2 · Review required'}).pr,'PR #2 · Review required');
});

test('failed local reads retain cached work and PR findings but never show In sync', () => {
 const localNote = 'Status check timed out · cached';
 const s = cardStatus({...clean,localNote,changes:2,github:'PR #4 · Approved',reviews:[{number:4,blockers:1,warnings:7,detail:'Last known report'}]});
 assert.equal(s.quiet,''); assert.equal(s.alert.text,localNote); assert.equal(s.alert.tone,'warning');
 assert.equal(s.local[0].value,2); assert.equal(s.reviews[0].values[0].value,1);
 assert.equal(cardStatus({...clean,localNote}).quiet,'');
});

test('GitHub approval takes precedence over no findings and remains visible with warnings', () => {
 const approved = {...clean,github:'PR #2812 · Approved'};
 const report = {number:2812,blockers:0,warnings:0,detail:'Latest reported findings: 0 blockers, 0 warnings'};
 const s = cardStatus({...approved,reviews:[report]});
 assert.equal(s.pr,'');
 assert.equal(s.reviews[0].values[0].label,'Approved');
 assert.equal(s.reviews[0].values[0].tone,'good');
 assert.equal(s.reviews[0].values[0].symbol,'✓');
 const findings = cardStatus({...approved,reviews:[{...report,warnings:3,earlier:true}]});
 assert.equal(findings.pr,'PR #2812 · Approved');
 assert.equal(findings.prTone,'good');
 assert.equal(findings.reviews[0].values[0].value,3);
 assert.equal(findings.reviews[0].note,'Changes since review');
 const unavailable = cardStatus({...approved,reviews:[{...report,unavailable:true}]});
 assert.match(unavailable.pr,/Approved/);
 assert.match(unavailable.pr,/Review unavailable/);
 const separate = cardStatus({...clean,github:'PR #1 · Approved; PR #2812 · Review required',reviews:[report]});
 assert.equal(separate.pr,'PR #1 · Approved');
 assert.equal(separate.reviews[0].values[0].label,'No findings');
});

test('merged PRs replace review state while preserving local work', () => {
 const mergedPR = {number:2812,sameHead:true,mergedAt:'2026-09-21T19:43:32Z',url:'https://github.com/teamconclude/project/pull/2812'};
 const s = cardStatus({...clean,changes:2,github:'PR #2812 · Merged',mergedPR,reviews:[{number:2812,blockers:0,warnings:0}]});
 assert.equal(s.pr,'PR #2812 · Merged');
 assert.equal(s.prTone,'merged');
 assert.equal(s.prNote,'Branch left over after merge');
 assert.equal(s.local[0].value,2);
 assert.equal(s.quiet,''); assert.equal(s.reviews.length,0);
 assert.match(s.prTitle,/https:\/\/github.com/);
 const changed = cardStatus({...clean,mergedPR:{...mergedPR,sameHead:false}});
 assert.equal(changed.pr,'');
 assert.equal(changed.prNote,'');
 assert.equal(changed.prTone,'muted');
});

test('new default-branch commits are distinct from upstream sync and marked when cached', () => {
 const git={summary:'Up to date',isMainBranch:false,base:{branch:'develop',tracking:'origin/develop',behind:6}};
 const result=cardStatus(git);
 assert.equal(result.quiet,''); assert.equal(result.remote.length,0);
 assert.equal(result.base,'develop · 6 newer commits');
 assert.match(cardStatus({...git,base:{...git.base,note:'Default branch unavailable'}}).base,/cached/);
 assert.equal(cardStatus({...git,mergedPR:{number:1,sameHead:true}}).base,undefined);
});

test('recognized rebases replace raw divergence counts while retaining edits and freshness', () => {
 const git={rebased:true,ahead:11,behind:5,changes:2,summary:'Rebased · ready to publish',tone:'info'};
 const result=cardStatus(git);
 assert.equal(result.alert.text,'Rebased · ready to publish');
 assert.equal(result.remote.length,0);
 assert.equal(result.local.length,1);assert.equal(result.local[0].label,'edits');
 assert.match(result.alert.title,/11 local and 5 remote/);
 assert.match(result.alert.title,/check and push the rebased branch/);
 assert.equal(result.alert.publishRebased,true);
 assert.equal(cardStatus({...git,remoteNote:'Offline'}).alert.text,'Rebased · remote not verified');
 assert.notEqual(cardStatus({...git,localNote:'Status unavailable · cached'}).alert.text,'Rebased · ready to publish');
 const divergent=cardStatus({...git,rebased:false,summary:'Diverged · 11 ahead / 5 behind'});
 assert.equal(divergent.remote[0].value,5);assert.equal(divergent.local[0].value,11);
});
