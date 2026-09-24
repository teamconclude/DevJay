import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
let source=await readFile(new URL('../cmd/devjay/frontend/git-viewer.js',import.meta.url),'utf8');
const editorSource=await readFile(new URL('../cmd/devjay/frontend/file-editor.js',import.meta.url),'utf8');
source=source.replace('./file-editor.js',`data:text/javascript;base64,${Buffer.from(editorSource).toString('base64')}`);
const {diffRows,summarizeFiles,remoteHistoryContext,localHistoryContext,commitPresenceLabel,viewCategory}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
test('unified diff rows retain independent old and new line numbers',()=>{
 const rows=diffRows('--- a/file\n+++ b/file\n@@ -3,2 +3,3 @@\n context\n-old\n+new\n+added\n\\ No newline at end of file');
 assert.deepEqual(rows.slice(3,7).map(r=>[r.kind,r.old,r.current]),[['context',3,3],['delete',4,''],['add','',4],['add','',5]]);
 assert.equal(rows[7].kind,'meta');
});
test('headers, binary changes, combined conflicts, and HTML-looking code remain literal text',()=>{
 const rows=diffRows('Binary files a/a and b/a differ\n@@@ -1,1 -1,1 +1,1 @@@\n++<script>alert(1)</script>');
 assert.equal(rows[0].kind,'meta');assert.equal(rows[1].kind,'meta');
 assert.equal(rows[2].line,'++<script>alert(1)</script>');assert.equal(rows[2].old,'');
});

test('code resembling a file header remains a numbered addition or deletion inside a hunk',()=>{
 const rows=diffRows('@@ -1 +1 @@\n--- old text\n+++ new text');
 assert.deepEqual(rows.slice(1).map(r=>[r.kind,r.old,r.current]),[['delete',1,''],['add','',1]]);
});

test('summary counts distinct paths and sums staged/unstaged lines separately',()=>{
 const files=[{path:'a',statsKnown:true,added:4,removed:2},{path:'a',statsKnown:true,added:2,removed:1},{path:'b',statsKnown:true,binary:true}];
 assert.deepEqual(summarizeFiles(files),{count:2,added:6,removed:3,binary:1,partial:false});
 assert.equal(summarizeFiles([...files,{path:'large'}]).partial,true);
 assert.equal(summarizeFiles(files,true).partial,true);
 assert.deepEqual(summarizeFiles([]),{count:0,added:0,removed:0,binary:0,partial:false});
});

test('remote history distinguishes already-local commits from incoming changes',()=>{
 assert.equal(commitPresenceLabel('incoming'),'↓ Incoming');
 assert.equal(commitPresenceLabel('local'),'✓ Already local');
 assert.equal(commitPresenceLabel('unknown'),'Local status unknown');
 assert.match(remoteHistoryContext({tracking:'origin/main',incomingCount:0}),/Remote history · origin\/main · 0 incoming commits/);
 assert.match(remoteHistoryContext({incomingCount:-1}),/comparison unavailable/);
});

test('unpublished branch history names the cached comparison without claiming an upstream',()=>{
 const v={comparisonOnly:true,tracking:'origin/main',incomingCount:0,counts:{local:3,remote:18}};
 assert.match(localHistoryContext(v),/Local-only commits compared with origin\/main/);
 assert.match(localHistoryContext(v),/No upstream configured; cached comparison/);
 assert.match(remoteHistoryContext(v),/origin\/main · 0 incoming commits/);
 assert.match(remoteHistoryContext(v),/No upstream configured; cached comparison/);
 assert.equal(viewCategory(v,'local').count,'3');
 assert.match(viewCategory(v,'local').description,/compared with origin\/main/);
 assert.equal(viewCategory(v,'remote').count,'18');
 assert.doesNotMatch(localHistoryContext({...v,comparisonOnly:false}),/No upstream/);
});

test('category counts distinguish empty, unavailable, and full remote history',()=>{
 const v={counts:{edits:0,local:0,remote:248},upstream:'captured-upstream'};
 assert.equal(viewCategory(v,'edits').count,'0');
 assert.equal(viewCategory(v,'local').empty,true);
 assert.equal(viewCategory(v,'remote').empty,false);
 assert.equal(viewCategory(v,'remote').count,'248');
 assert.match(viewCategory(v,'remote').description,/including commits already present locally/);
 assert.equal(viewCategory({loading:true},'edits').count,'…');
 const missing=viewCategory({counts:{edits:2,local:null}},'local');
 assert.equal(missing.count,'—');assert.equal(missing.empty,false);
 assert.match(missing.description,/No upstream/);
 assert.match(viewCategory({upstream:'abc',counts:{remote:null}},'remote').description,/unavailable/);
 // A failed load or absent data must never imply a known empty category.
 assert.equal(viewCategory({error:'Checkout unavailable'},'edits').empty,false);
});
