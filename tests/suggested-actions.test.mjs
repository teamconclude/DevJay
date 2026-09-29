import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
let source = await readFile(new URL('../cmd/devjay/frontend/suggested-actions.js', import.meta.url), 'utf8');
const prSource = await readFile(new URL('../cmd/devjay/frontend/pr-status.js', import.meta.url), 'utf8');
source = source.replace('./pr-status.js', `data:text/javascript;base64,${Buffer.from(prSource).toString('base64')}`);
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

const {siteSuggestions, suggestionSteps, mountSuggestedActions, mountViewerSuggestions, recommendedActions} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
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
class Element {
 constructor(tag) {this.tag=tag;this.children=[];this.hidden=false;this.disabled=false;this.attrs={};}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.children=nodes;}
 setAttribute(name,value){this.attrs[name]=value;}
}
function nodes(root) {const result=[];const walk=n=>{result.push(n);n.children.forEach(walk);};walk(root);return result;}
function withDocument(fn) {const previous=globalThis.document;globalThis.document={createElement:tag=>new Element(tag)};try{fn();}finally{globalThis.document=previous;}}

const classNode=(root,name)=>nodes(root).find(n=>n.className?.split(' ').includes(name));
const branchState=()=>({suggestedSite:'site',sites:[{id:'site',name:'project',path:'/tmp/project',git:{branch:'stale',head:'stale-head'}}],
 action:{siteID:'site',kind:'switch',status:'choosing',currentBranch:'feature',currentHead:'head123456',message:'Known remote branches are listed. The selected upstream is checked before confirmation.',branches:[
  {name:'feature',local:true,current:true},{name:'main',local:true,current:false},{name:'release/next',remote:'origin',local:false,current:false},
 ]}});
const switchPlan=(patch={})=>({id:'switch-token',kind:'switch',branch:'feature',head:'head123456',target:'main',targetHead:'target123456',targetRemote:'',remoteTarget:'origin/main',canUpdateTarget:true,updateTarget:false,remoteAhead:3,localAhead:0,createTarget:false,...patch});

test('branch discovery waits for explicit choice and filtering survives snapshots without replacing search',()=>withDocument(()=>{
 const root=new Element('root'),calls=[],render=mountSuggestedActions(root,()=>{},(...args)=>calls.push(args)),state=branchState();
 render({...state,action:{...state.action,status:'loading-branches',branches:undefined}});
 assert.ok(nodes(root).some(n=>n.textContent==='Loading branches…'));assert.equal(calls.length,0);
 render(state);
 const search=nodes(root).find(n=>n.type==='search'),rows=nodes(root).filter(n=>n.className==='branch-choice');
 assert.equal(rows.length,3);assert.equal(rows[0].disabled,true);rows[0].onclick();assert.equal(calls.length,0);
 assert.match(rows[2].attrs['aria-label'],/^release\/next · Remote · origin/);
 search.value='release';search.oninput();assert.deepEqual(rows.map(n=>n.hidden),[true,true,false]);
 render({...state,sites:[{...state.sites[0],git:{branch:'unrelated',head:'other'}}],action:{...state.action,message:'Remote list refreshed'}});
 assert.equal(nodes(root).find(n=>n.type==='search'),search);assert.equal(search.value,'release');
 assert.equal(nodes(root).find(n=>n.className==='branch-choice'),rows[0]);
 rows[2].onclick();rows[2].onclick();
 assert.deepEqual(calls,[['action-prepare',{siteID:'site',kind:'switch',branch:'feature',head:'head123456',targetBranch:'release/next',targetRemote:'origin',updateTarget:false}]]);
 assert.equal(search.disabled,true);assert.ok(rows.every(n=>n.disabled));
 assert.equal(calls.some(([kind])=>kind==='action-confirm'),false);
}));

test('switch defaults to the local tip and remote opt-in prepares a new plan before confirmation',()=>withDocument(()=>{
 const root=new Element('root'),calls=[],render=mountSuggestedActions(root,()=>{},(...args)=>calls.push(args)),state=branchState(),plan=switchPlan();
 render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan}});
 assert.equal(calls.length,0);
 assert.ok(nodes(root).some(n=>n.textContent==='Switch to existing local branch main at its current commit.'));
 const option=classNode(root,'branch-remote-option'),checkbox=classNode(root,'branch-remote-checkbox'),confirm=classNode(root,'suggestion-primary');
 assert.equal(option.hidden,false);assert.equal(checkbox.checked,false);
 assert.ok(nodes(root).some(n=>n.textContent==='Use newer remote version from origin/main (3 newer commits)'));
 checkbox.checked=true;checkbox.onchange();confirm.onclick();
 assert.deepEqual(calls,[['action-prepare',{siteID:'site',kind:'switch',branch:'feature',head:'head123456',targetBranch:'main',targetRemote:'',updateTarget:true}]]);
 render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:{...plan,id:'updated-token',updateTarget:true}}});
 assert.ok(nodes(root).some(n=>n.textContent==='Switch to main and fast-forward it to the confirmed origin/main commit.'));
 assert.equal(checkbox.checked,true);checkbox.checked=false;checkbox.onchange();
 assert.equal(calls[1][1].updateTarget,false);
 render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:{...plan,id:'local-token'}}});
 confirm.onclick();confirm.onclick();
 assert.deepEqual(calls[2],['action-confirm','local-token']);assert.equal(calls.length,3);
}));

test('diverged or locally ahead targets never offer remote replacement and remote-only creation is explicit',()=>withDocument(()=>{
 const root=new Element('root'),calls=[],render=mountSuggestedActions(root,()=>{},(...args)=>calls.push(args)),state=branchState();
 for(const patch of [{localAhead:2,remoteAhead:0},{localAhead:2,remoteAhead:3},{canUpdateTarget:false},{remoteAhead:0}]){
  render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:switchPlan({...patch,remoteNote:'Local commits are kept.'})}});
  assert.equal(classNode(root,'branch-remote-option').hidden,true);
  const checkbox=classNode(root,'branch-remote-checkbox');checkbox.checked=true;checkbox.onchange();assert.equal(calls.length,0);
  assert.ok(nodes(root).some(n=>n.textContent==='Local commits are kept.'));
 }
 render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:switchPlan({localAhead:2,remoteAhead:3,canUpdateTarget:false})}});
 assert.ok(nodes(root).some(n=>n.textContent==='main has 2 local commits not in origin/main; its local version will be kept.'));
 render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:switchPlan({localAhead:2,remoteNote:'Remote unavailable; keeping the local version.'})}});
 assert.equal(nodes(root).some(n=>n.textContent?.includes('2 local commits not in')),false,'unavailable remote notes take precedence over counts');
 render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:switchPlan({target:'release/next',targetRemote:'origin',remoteTarget:'origin/release/next',createTarget:true})}});
 assert.equal(classNode(root,'branch-remote-option').hidden,true);
 assert.ok(nodes(root).some(n=>n.textContent==='Create local branch release/next from origin/release/next and switch to it.'));
 classNode(root,'suggestion-primary').onclick();assert.deepEqual(calls,[['action-confirm','switch-token']]);
}));

test('blocked branch switches show saved local-work guidance and can reopen the chooser without mutation',()=>withDocument(()=>{
 const root=new Element('root'),calls=[],render=mountSuggestedActions(root,()=>{},(...args)=>calls.push(args)),state=branchState();
 render({...state,action:{siteID:'site',kind:'switch',status:'failed',message:'Commit or stash local changes before switching.',files:['file.txt'],currentBranch:'feature',currentHead:'head123456'}});
 assert.ok(nodes(root).some(n=>n.textContent==='Commit or stash local changes before switching.'));
 assert.ok(nodes(root).some(n=>n.textContent==='file.txt'));assert.equal(classNode(root,'suggestion-primary').hidden,true);
 const back=classNode(root,'branch-back');assert.equal(back.hidden,false);back.onclick();back.onclick();
 assert.deepEqual(calls,[['branches-open',{siteID:'site'}]]);
 render({...state,action:{...state.action,status:'loading-branches'}});render(state);
 assert.equal(nodes(root).find(n=>n.type==='search').disabled,false);
 assert.equal(nodes(root).filter(n=>n.className==='branch-choice'&&!n.disabled).length,2);
}));

test('branch-choice and remote-option delivery failures restore retry without accepting an old plan',async()=>{
 const previous=globalThis.document;globalThis.document={createElement:tag=>new Element(tag)};
 try {
  const root=new Element('root'),state=branchState(),render=mountSuggestedActions(root,()=>{},async()=>false);
  render(state);const choice=nodes(root).find(n=>n.attrs['aria-label']?.startsWith('main · Local branch'));
  const selecting=choice.onclick();assert.equal(choice.disabled,true);await selecting;assert.equal(choice.disabled,false);
  assert.equal(nodes(root).find(n=>n.type==='search').disabled,false);
  render({...state,action:{siteID:'site',kind:'switch',status:'ready',plan:switchPlan()}});
  const checkbox=classNode(root,'branch-remote-checkbox'),confirm=classNode(root,'suggestion-primary');
  checkbox.checked=true;const preparing=checkbox.onchange();assert.equal(confirm.disabled,true);await preparing;
  assert.equal(checkbox.checked,false);assert.equal(checkbox.disabled,false);assert.equal(confirm.disabled,false);
  const back=classNode(root,'branch-back'),opening=back.onclick();assert.equal(back.disabled,true);await opening;assert.equal(back.disabled,false);
 } finally {globalThis.document=previous;}
});

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
 render({...site,git:{...site.git,changes:0}},{status:'running'});assert.equal(nodes(root).find(n=>n.tag==='button').disabled,true);
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
 // Offline status can be refreshed during push preparation; backend checks it afresh.
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
 const checkbox=nodes(root).find(n=>n.tag==='input'&&n.type==='checkbox'&&n.className==='');assert.equal(checkbox.checked,false);checkbox.checked=true;checkbox.onchange();
 assert.deepEqual(calls,[['action-prepare',{siteID:'site',kind:'merged',branch:'feature',head:'oldhead12',deleteBranch:true}]]);
 nodes(root).find(n=>n.tag==='button'&&n.textContent==='Return to develop').onclick();assert.equal(calls.length,1);
 render({...state,action:{...state.action,plan:{...plan,id:'new',deleteBranch:true}}});
 nodes(root).find(n=>n.tag==='button'&&n.textContent==='Return to develop').onclick();assert.deepEqual(calls[1],['action-confirm','new']);
}));

test('verified remote rewrites offer replacement instead of an unsafe update or push',()=>{
 const git={branch:'solo',head:'old',tracking:'origin/solo',ahead:3,behind:10,canReplaceRemote:true,base:{branch:'develop',behind:4}};
 const [s]=siteSuggestions({git});
 assert.equal(s.kind,'replace');assert.equal(s.blocked,false);assert.equal(siteSuggestions({git}).length,1);
 assert.match(suggestionSteps(s).join(' '),/exact local tip/);
 assert.match(suggestionSteps(s).join(' '),/recovery reference/);
 assert.equal(siteSuggestions({git:{...git,canReplaceRemote:false}}).some(s=>s.kind==='replace'),false);
 for(const change of [{changes:1},{conflicts:1},{localNote:'cached'}]) assert.equal(siteSuggestions({git:{...git,...change}})[0].blocked,true);
 // A remote rewind can have zero incoming commits, but is still a replacement.
 assert.equal(siteSuggestions({git:{...git,behind:0,pushTarget:'origin/solo'}})[0].kind,'replace');
});

test('replacement requires explicit confirmation and never starts from rendering',()=>withDocument(()=>{
 const viewer=new Element('section'),root=new Element('root'),calls=[];
 const site={id:'site',name:'checkout',git:{branch:'solo',head:'oldtip123',tracking:'origin/solo',ahead:3,behind:10,canReplaceRemote:true}};
 mountViewerSuggestions(viewer,req=>calls.push(['prepare',req]))(site);
 const button=nodes(viewer).find(n=>n.tag==='button');assert.equal(button.textContent,'Replace from remote');
 assert.equal(calls.length,0);button.onclick();assert.equal(calls[0][1].kind,'replace');
 const render=mountSuggestedActions(root,()=>calls.push(['cancel']),(...args)=>calls.push(args));
 const plan={id:'replacement-token',kind:'replace',branch:'solo',head:'oldtip123',target:'origin/solo',targetHead:'newtip123'};
 const state={suggestedSite:'site',sites:[site],action:{siteID:'site',kind:'replace',status:'ready',plan}};
 render(state);render(state);
 assert.equal(calls.length,1);assert.ok(nodes(root).some(n=>n.textContent==='Confirm branch replacement'));
 assert.ok(nodes(root).some(n=>n.textContent?.includes('old commits will be kept')));
 const confirm=nodes(root).find(n=>n.tag==='button'&&n.textContent==='Replace branch');confirm.onclick();confirm.onclick();
 assert.deepEqual(calls[1],['action-confirm','replacement-token']);assert.equal(calls.length,2);
}));

test('a feature tracking the default branch can rebase when diverged',()=>{
 const suggestions=siteSuggestions({git:{branch:'feature',ahead:1,behind:2,tracking:'origin/main',base:{branch:'main',tracking:'origin/main',behind:2}}});
 assert.equal(suggestions.find(s=>s.kind==='rebase').blocked,false);
 assert.equal(suggestions.find(s=>s.kind==='update').blocked,true);
});

test('failed prepare and confirm delivery re-enable the same rendered buttons',async()=>{
 const previous=globalThis.document;
 globalThis.document={createElement:tag=>new Element(tag)};
 try {
  const site={id:'site',git:{branch:'main',head:'old',tracking:'origin/main',behind:1}};
  const viewer=new Element('section');
  mountViewerSuggestions(viewer,async()=>false)(site);
  const prepare=nodes(viewer).find(n=>n.tag==='button');
  const preparing=prepare.onclick();assert.equal(prepare.disabled,true);await preparing;assert.equal(prepare.disabled,false);
  const popup=new Element('section');
  const render=mountSuggestedActions(popup,()=>{},async()=>{throw new Error('bridge failed');});
  render({suggestedSite:'site',sites:[site],action:{siteID:'site',status:'ready',plan:{id:'token',kind:'update',branch:'main',target:'origin/main',head:'old',targetHead:'new'}}});
  const confirm=nodes(popup).find(n=>n.textContent==='Update branch');
  const confirming=confirm.onclick();assert.equal(confirm.disabled,true);await confirming;assert.equal(confirm.disabled,false);
 } finally {globalThis.document=previous;}
});

test('branch rows show PR review and CI independently, support PR search, and preserve filtering during enrichment',()=>withDocument(()=>{
 const root=new Element('root'),render=mountSuggestedActions(root,()=>{},()=>{}),state=branchState();
 render(state);
 const search=nodes(root).find(n=>n.type==='search');search.value='main';search.oninput();
 const enriched={...state,action:{...state.action,branches:state.action.branches.map(branch=>({...branch,prStatus:'known',pullRequests:branch.name==='main'?[{number:42,state:'Approved',ciState:'failure'}]:[]}))}};
 render(enriched);assert.equal(search.value,'main');
 const tag=classNode(root,'branch-pr');assert.match(tag.textContent,/#42 · Approved · CI failed/);assert.match(tag.className,/ci-failed/);assert.doesNotMatch(tag.className,/pr-approved/);
 const rows=nodes(root).filter(n=>n.className==='branch-choice');assert.deepEqual(rows.map(n=>n.hidden),[true,false,true]);
 search.value='#42';search.oninput();assert.deepEqual(rows.map(n=>n.hidden),[true,false,true]);
 enriched.action.branches[1].pullRequests[0].ciState='success';render(enriched);
 assert.match(classNode(root,'branch-pr').className,/pr-approved/);assert.match(classNode(root,'branch-pr').textContent,/CI passed/);
}));

test('PR enrichment cannot unlock a branch request in flight and delivery failure still permits retry',async()=>{
 const previous=globalThis.document;globalThis.document={createElement:tag=>new Element(tag)};
 try {
  let finish;const state=branchState(),root=new Element('root');
  const render=mountSuggestedActions(root,()=>{},()=>new Promise(resolve=>{finish=resolve;}));
  render(state);const selecting=nodes(root).find(n=>n.attrs['aria-label']?.startsWith('main ·')).onclick();
  render({...state,action:{...state.action,branches:state.action.branches.map(b=>({...b,prStatus:'known'}))}});
  assert.ok(nodes(root).filter(n=>n.className==='branch-choice').every(n=>n.disabled));
  finish(false);await selecting;
  assert.equal(nodes(root).find(n=>n.attrs['aria-label']?.startsWith('main ·')).disabled,false);
 } finally {globalThis.document=previous;}
});
