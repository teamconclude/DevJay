import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source = await readFile(new URL('../frontend/suggested-actions.js', import.meta.url), 'utf8');
const {postMergeSuggestion} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const git = {branch:'feature',defaultBranch:'trunk',mergedPR:{number:4,sameHead:true,base:'trunk'}};
test('suggestions require a merged PR and use the repository default', () => {
 assert.equal(postMergeSuggestion(undefined),null);
 assert.equal(postMergeSuggestion({git:{branch:'feature'}}),null);
 const s = postMergeSuggestion({git});
 assert.equal(s.target,'trunk'); assert.equal(s.branch,'feature'); assert.equal(s.canDelete,true);
 assert.equal(postMergeSuggestion({git:{...git,defaultBranch:''}}),null);
});
test('return suggestion is absent for changed, conflicted, stale, reused or wrong-base branches', () => {
 for(const change of [{changes:1},{conflicts:1},{localNote:'cached'},{isMainBranch:true},{mergedPR:{number:4,sameHead:false,base:'trunk'}},{mergedPR:{number:4,sameHead:true,base:'release'}}]) {
  const s=postMergeSuggestion({git:{...git,...change}});
  assert.equal(s,null);
 }
});

const {siteSuggestions, rebaseCheckForSite, suggestionSteps, mountSuggestedActions, mountViewerSuggestions, recommendedActions} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
test('default branch behind upstream suggests a fast-forward update', () => {
 const [s] = siteSuggestions({git:{branch:'develop',isMainBranch:true,tracking:'origin/develop',behind:3}});
 assert.equal(s.kind,'update'); assert.equal(s.blocked,false);
 assert.match(suggestionSteps(s).join(' '), /Fast-forward develop/);
 for (const patch of [{ahead:1},{rewritten:true},{changes:1},{localNote:'cached'},{remoteNote:'offline'}]) {
  assert.equal(siteSuggestions({git:{branch:'develop',tracking:'origin/develop',behind:3,...patch}})[0].blocked,true);
 }
});
test('feature upstream and default-branch comparisons are independent', () => {
 const feature = {branch:'feature',tracking:'origin/feature',base:{branch:'develop',tracking:'origin/develop',behind:6}};
 const [s] = siteSuggestions({git:feature});
 assert.equal(s.kind,'rebase'); assert.equal(s.target,'develop'); assert.equal(s.blocked,false);
 const both = siteSuggestions({git:{...feature,behind:2}});
 assert.deepEqual(both.map(s=>s.kind),['update','rebase']);
 assert.equal(both[1].blocked,true); assert.match(both[1].warning,/own remote first/);
 assert.equal(siteSuggestions({git:{...feature,changes:1}})[0].blocked,true);
 assert.deepEqual(siteSuggestions({git:{...feature,base:{...feature.base,behind:0}}}),[]);
 assert.equal(siteSuggestions({git:{...feature,defaultBranch:'develop',mergedPR:{number:1,sameHead:true,base:'develop'}}}).length,1);
 assert.equal(siteSuggestions({git:{...feature,defaultBranch:'develop',mergedPR:{number:1,sameHead:true,base:'develop'}}})[0].kind,'merged');
});
test('rebase checks are bound to the site, branch and tested commits', () => {
 const site={id:'c3',git:{branch:'feature',head:'abc',base:{branch:'develop',head:'old'}}};
 const check={siteID:'c3',branch:'feature',head:'abc',target:'develop',baseHead:'old',targetHead:'new',status:'clean'};
 assert.equal(rebaseCheckForSite(check,site).status,'clean');
 assert.equal(rebaseCheckForSite(check,{...site,id:'c2'}),null);
 for(const patch of [{head:'changed'},{branch:'other'},{changes:1},{conflicts:1},{localNote:'cached'},{base:{branch:'develop',head:'newer'}}]) {
  assert.equal(rebaseCheckForSite(check,{...site,git:{...site.git,...patch}}).status,'stale');
 }
 assert.equal(rebaseCheckForSite(check,{...site,git:{...site.git,base:{branch:'develop',head:'new'}}}).status,'clean');
 assert.equal(rebaseCheckForSite({...check,status:'conflicts'},site).status,'conflicts');
});

class Element {
 constructor(tag) {this.tag=tag;this.children=[];this.hidden=false;this.disabled=false;}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.children=nodes;}
 setAttribute(){}
}
function nodes(root) {const result=[];const walk=n=>{result.push(n);n.children.forEach(walk);};walk(root);return result;}
function withDocument(fn) {const previous=globalThis.document;globalThis.document={createElement:tag=>new Element(tag)};try{fn();}finally{globalThis.document=previous;}}

test('direct update opens confirmation without a second review step',()=>withDocument(()=>{
 const root=new Element('root'),calls=[];
 const render=mountSuggestedActions(root,()=>calls.push(['close']),(...args)=>calls.push(args));
 const site={id:'c2',name:'c2',git:{branch:'develop',head:'oldhead123',tracking:'origin/develop',behind:3}};
 const state={suggestedSite:'c2',sites:[site]};
 render({...state,action:{siteID:'c2',kind:'update',status:'preparing',message:'Checking'}});
 assert.equal(calls.length,0);assert.equal(nodes(root).some(n=>n.textContent==='Review action'),false);
 const plan={id:'confirmation-token',kind:'update',branch:'develop',head:site.git.head,target:'origin/develop',targetHead:'newhead123'};
 render({...state,action:{siteID:'c2',kind:'update',status:'ready',plan}});
 const button=nodes(root).find(n=>n.tag==='button'&&n.textContent==='Update branch');button.onclick();button.onclick();
 assert.deepEqual(calls,[['action-confirm','confirmation-token']]);
 render({...state,action:{siteID:'c2',kind:'update',status:'running'}});
 assert.equal(nodes(root).find(n=>n.textContent==='Cancel').disabled,true);
 render({...state,action:{siteID:'c2',kind:'update',status:'complete',message:'Updated'}});
 assert.ok(nodes(root).some(n=>n.textContent==='Done'));assert.ok(nodes(root).some(n=>n.textContent==='✓ Completed'));
}));

test('a recognized rebase never suggests pulling its old upstream history', () => {
 const git={rebased:true,branch:'feature',ahead:11,behind:5,tracking:'origin/feature'};
 assert.deepEqual(siteSuggestions({git}),[]);
 assert.equal(siteSuggestions({git:{...git,rebased:false}})[0].kind,'update');
});

test('viewer action buttons follow live checkout, preserve warnings and prepare only',()=>withDocument(()=>{
 const root=new Element('section'),calls=[];
 const render=mountViewerSuggestions(root,request=>calls.push(request));
 const site={id:'c2',git:{branch:'develop',head:'current',tracking:'origin/develop',behind:3,changes:2}};
 render(site);assert.equal(root.hidden,false);assert.equal(calls.length,0);
 let button=nodes(root).find(n=>n.tag==='button');
 assert.equal(button.textContent,'Update branch');assert.equal(button.disabled,true);
 button.onclick();assert.equal(calls.length,0);
 assert.ok(nodes(root).some(n=>n.textContent?.includes('Commit or stash')));
 render({...site,git:{...site.git,changes:0,head:'new-current'}});
 button=nodes(root).find(n=>n.tag==='button');button.onclick();button.onclick();
 assert.deepEqual(calls,[{siteID:'c2',kind:'update',branch:'develop',head:'new-current',deleteBranch:false}]);
 render(site,{status:'running'});assert.equal(nodes(root).find(n=>n.tag==='button').disabled,true);
 render({...site,git:{...site.git,behind:0}});assert.equal(root.hidden,true);
 render(undefined);assert.equal(root.hidden,true);
}));

test('local-only commits suggest an explicit normal push to origin', () => {
 const git={branch:'main',head:'localsha',ahead:2,behind:0,pushTarget:'origin/main',tracking:'origin/main',isMainBranch:true};
 const [s]=siteSuggestions({git});
 assert.equal(s.kind,'push'); assert.equal(s.title,'Push 2 commits to origin/main'); assert.equal(s.blocked,false);
 assert.match(suggestionSteps(s).join(' '),/Never force-push/);
 for(const patch of [{behind:1},{ahead:0},{rebased:true},{rewritten:true},{pushTarget:''}]) {
  assert.equal(siteSuggestions({git:{...git,...patch}}).some(s=>s.kind==='push'),false);
 }
 for(const patch of [{changes:1},{conflicts:1},{localNote:'cached'}]) {
  assert.equal(siteSuggestions({git:{...git,...patch}})[0].blocked,true);
 }
 // Offline status can be refreshed by Review action; backend checks it afresh.
 assert.equal(siteSuggestions({git:{...git,remoteNote:'offline'}})[0].blocked,false);
});

test('push button opens Confirm push and requires a separate confirmation',()=>withDocument(()=>{
 const viewer=new Element('section'),root=new Element('root'),calls=[];
 const site={id:'dj',name:'DevJay',path:'/Users/test/DevJay',git:{branch:'main',head:'localsha',ahead:3,behind:0,pushTarget:'origin/main'}};
 mountViewerSuggestions(viewer,req=>calls.push(['prepare',req]))(site);
 const push=nodes(viewer).find(n=>n.tag==='button');assert.equal(push.textContent,'Push 3 commits');
 assert.equal(push.className,'suggestion-primary');push.onclick();
 assert.deepEqual(calls,[['prepare',{siteID:'dj',kind:'push',branch:'main',head:'localsha',deleteBranch:false}]]);
 const render=mountSuggestedActions(root,()=>{},(...args)=>calls.push(args));
 const state={suggestedSite:'dj',sites:[site]};
 render({...state,action:{siteID:'dj',kind:'push',status:'preparing'}});
 assert.equal(nodes(root).find(n=>n.tag==='h2').textContent,'Confirm push');
 assert.equal(calls.length,1);
 const plan={id:'token',kind:'push',branch:'main',target:'origin/main',head:'localsha',targetHead:'oldremote',commits:3};
 render({...state,action:{siteID:'dj',kind:'push',status:'ready',plan}});
 const confirm=nodes(root).find(n=>n.tag==='button'&&n.textContent==='Push 3 commits');confirm.onclick();confirm.onclick();
 assert.deepEqual(calls[1],['action-confirm','token']);assert.equal(calls.length,2);
 assert.equal(nodes(root).some(n=>n.textContent==='Review options…'||n.textContent==='Review action'),false);
 render({...state,action:{siteID:'dj',status:'complete',kind:'push',message:'Pushed'}});
 assert.ok(nodes(root).some(n=>n.textContent==='Done'));assert.equal(nodes(root).some(n=>n.textContent?.includes('recovery reference')),false);
}));

test('best action is rightmost, prerequisite actions win, and at most three are shown',()=>withDocument(()=>{
 const cases=[
  {git:{branch:'feature',head:'sha',behind:2,tracking:'origin/feature',base:{branch:'develop',behind:5}},labels:['Rebase onto develop','Update branch']},
  {git:{branch:'feature',head:'sha',ahead:2,behind:0,pushTarget:'origin/feature',base:{branch:'develop',behind:5}},labels:['Push 2 commits','Rebase onto develop']},
 ];
 for(const tc of cases){
  const root=new Element('section');mountViewerSuggestions(root,()=>{})({id:'site',git:tc.git});
  const buttons=nodes(root).filter(n=>n.tag==='button');
  assert.deepEqual(buttons.map(b=>b.textContent),tc.labels);
  assert.equal(buttons.at(-1).className,'suggestion-primary');assert.equal(buttons[0].className,'suggestion-secondary');
 }
 const ranked=recommendedActions([{kind:'push'},{kind:'rebase'},{kind:'update'},{kind:'merged'}]);
 assert.equal(ranked.length,3);assert.equal(ranked.at(-1).kind,'merged');
 assert.equal(recommendedActions([{kind:'update',blocked:true},{kind:'push'}]).at(-1).kind,'push');
}));

test('optional merged-branch deletion requires a newly prepared confirmation',()=>withDocument(()=>{
 const root=new Element('root'),calls=[];
 const render=mountSuggestedActions(root,()=>{},(...args)=>calls.push(args));
 const site={id:'site',name:'site',git:{branch:'feature',defaultBranch:'develop'}};
 const plan={id:'old',kind:'merged',branch:'feature',head:'oldhead12',target:'develop',targetHead:'newhead12',deleteBranch:false};
 const state={suggestedSite:'site',sites:[site],action:{siteID:'site',kind:'merged',status:'ready',plan}};
 render(state);
 const checkbox=nodes(root).find(n=>n.tag==='input');assert.equal(checkbox.checked,false);checkbox.checked=true;checkbox.onchange();
 assert.deepEqual(calls,[['action-prepare',{siteID:'site',kind:'merged',branch:'feature',head:'oldhead12',deleteBranch:true}]]);
 nodes(root).find(n=>n.tag==='button'&&n.textContent==='Return to develop').onclick();assert.equal(calls.length,1);
 render({...state,action:{...state.action,plan:{...plan,id:'new',deleteBranch:true}}});
 nodes(root).find(n=>n.tag==='button'&&n.textContent==='Return to develop').onclick();assert.deepEqual(calls[1],['action-confirm','new']);
}));
