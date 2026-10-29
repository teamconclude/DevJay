import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
let source=await readFile(new URL('../cmd/devjay/frontend/git-viewer.js',import.meta.url),'utf8');
const editorSource=await readFile(new URL('../cmd/devjay/frontend/file-editor.js',import.meta.url),'utf8');
source=source.replace('./file-editor.js',`data:text/javascript;base64,${Buffer.from(editorSource).toString('base64')}`);
const {diffRows,splitDiff,changedText,summarizeFiles,remoteHistoryContext,localHistoryContext,commitPresenceLabel,branchHistoryContext,viewCategory,sameDiffSelection}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
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

test('split diff pairs replacements, pads one-sided lines and counts distinct changes within a hunk',()=>{
 const diff=splitDiff('@@ -10,5 +10,6 @@\n context\n-old one\n-old two\n+new one\n context\n+added one\n+added two\n context');
 assert.equal(diff.changes,2);
 const changes=diff.rows.filter(row=>row.kind==='change');
 assert.deepEqual(changes.map(row=>[row.change,row.before?.old,row.after?.current]),[[0,11,11],[0,12,undefined],[1,undefined,13],[1,undefined,14]]);
 assert.deepEqual(diff.rows.filter(row=>row.kind==='context').map(row=>[row.before.old,row.after.current]),[[10,10],[13,12],[14,15]]);
 assert.deepEqual(diff.unified.filter(row=>['add','delete'].includes(row.kind)).map(row=>row.change),[0,0,0,1,1]);
});

test('split diff preserves no-newline annotations, literal source, additions and deletions',()=>{
 const diff=splitDiff('@@ -1 +1 @@\n-<script>old</script>\n\\ No newline at end of file\n+<script>new</script>\n\\ No newline at end of file');
 assert.equal(diff.changes,1);
 assert.equal(diff.rows[1].before.noNewline,true);assert.equal(diff.rows[1].after.noNewline,true);
 assert.equal(diff.rows[1].after.line,'+<script>new</script>');
 assert.equal(splitDiff('@@ -0,0 +1 @@\n+new').rows[1].before,undefined);
 assert.equal(splitDiff('@@ -1 +0,0 @@\n-removed').rows[1].after,undefined);
 for(const text of ['', 'Binary files a/a and b/a differ', 'old mode 100644\nnew mode 100755','rename from old\nrename to new'])assert.equal(splitDiff(text).changes,0);
 assert.equal(splitDiff('@@@ -1 -1 +1 @@@\n++conflict').supported,false);
});

test('inline change highlights only differing text, including Unicode and empty sides',()=>{
 assert.deepEqual(changedText('return old(value);','return new(value);'),[['return ','old','(value);'],['return ','new','(value);']]);
 assert.deepEqual(changedText('a😀z','a🌍z'),[['a','😀','z'],['a','🌍','z']]);
 assert.deepEqual(changedText('','added'),[['','',''],['','added','']]);
 assert.deepEqual(changedText('same','same'),[['same','',''],['same','','']]);
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
 const history={branchOnly:true,branchCommits:2,counts:{branch:248}};
 assert.equal(viewCategory(history,'branch').count,'2');
 assert.match(viewCategory(history,'branch').description,/2 commits added since the branch base/);
 assert.equal(viewCategory({...history,branchOnly:false},'branch').count,'248');
 assert.match(viewCategory({...history,branchOnly:false},'branch').description,/full history/);
 assert.equal(viewCategory({...history,branchCommits:null},'branch').count,'—');
 assert.match(viewCategory({...history,branchCommits:null},'branch').description,/Full history/);
});


test('refresh keeps a selected diff by path and section when list indices shift',()=>{
 const selected={path:'b.go',group:'unstaged'}, base={siteID:'one',kind:'edits',commit:'',file:0,files:[selected]};
 assert.equal(sameDiffSelection(base,{...base,file:1,files:[{path:'a.go',group:'unstaged'},selected]}),true);
 for(const changes of [{kind:'remote'},{siteID:'two'},{commit:'changed'},{files:[{...selected,group:'staged'}]},{files:[{...selected,path:'new.go'}]},{file:-1}]) {
  assert.equal(sameDiffSelection(base,{...base,...changes}),false);
 }
});


test('diverged history distinguishes branch work from inherited commits and remote versions',()=>{
 const v={branch:'feature',tracking:'origin/feature',upstream:'remote',counts:{local:1131},incomingCount:11,branchWork:{local:11,inherited:1120,base:'origin/develop'}};
 assert.match(branchHistoryContext(v),/11 local-only · 11 remote-only/);
 assert.match(branchHistoryContext(v),/1120 additional commits already on origin\/develop/);
 assert.doesNotMatch(branchHistoryContext(v),/not pushed|incoming/);
 assert.equal(commitPresenceLabel('inherited',v),'✓ On origin/develop');
 assert.equal(commitPresenceLabel('outgoing',v),'↑ Local only');
 assert.equal(commitPresenceLabel('incoming',v),'↓ Remote only');
 assert.equal(commitPresenceLabel('incoming',{...v,counts:{local:0}}),'↓ Incoming');
});
