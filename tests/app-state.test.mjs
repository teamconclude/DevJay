import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import {cardStatus} from '../cmd/devjay/frontend/card-status.js';
import {installCardActivation} from '../cmd/devjay/frontend/card-interaction.js';
import {nextNotice} from '../cmd/devjay/frontend/notice.js';
const source=(await readFile(new URL('../cmd/devjay/frontend/app.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'');

// Exercise the real snapshot renderer with a small DOM and the Wails boundary.
function app(search='') {
 let document;
 class Element {
  constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.style={};this.className='';this.attrs={};this.textContent='';this.listeners={};
   this.classList={add:c=>{this.className+=' '+c;},contains:c=>this.className.split(' ').includes(c),toggle:(c,on)=>{const all=new Set(this.className.split(' '));on?all.add(c):all.delete(c);this.className=[...all].join(' ');}};
  }
  get textContent(){return (this._textContent || '')+this.children.map(child=>child.textContent).join('');}
  set textContent(text){this._textContent=text;for(const child of this.children || [])child.parent=null;this.children=[];}
  append(...nodes){for(const n of nodes){n.remove();n.parent=this;this.children.push(n);}}
  remove(){if(this.parent){const a=this.parent.children;a.splice(a.indexOf(this),1);this.parent=null;}}
  replaceChildren(...nodes){if(this.contains(document.activeElement))document.activeElement=document.body;this.textContent='';this.append(...nodes);}
  insertBefore(n,next){n.remove();n.parent=this;const i=this.children.indexOf(next);this.children.splice(i<0?this.children.length:i,0,n);}
  contains(n){return n===this || this.children.some(c=>c.contains(n));}
  setAttribute(k,v){this.attrs[k]=v;}
  addEventListener(name,fn){(this.listeners[name] ||= []).push(fn);}
  matches(s){if(s.includes(','))return s.split(',').some(part=>this.matches(part.trim()));if(s.startsWith('.'))return this.classList.contains(s.slice(1));if(s==='[data-pref]')return this.dataset.pref!==undefined;if(s==='[data-app-tooltip]')return this.dataset.appTooltip!==undefined;if(s==='[name=sorting]')return this.name==='sorting';return this.tag===s;}
  closest(s){return this.matches(s)?this:this.parent?.closest(s);}
  querySelectorAll(s){return this.children.flatMap(n=>[...(n.matches(s)?[n]:[]),...n.querySelectorAll(s)]);}
  querySelector(s){return this.querySelectorAll(s)[0]||null;}
  focus(){document.activeElement=this;}
  get firstElementChild(){return this.children[0];}
 }
 const root=new Element('main'),body=new Element('body');
 document={body,activeElement:body,createElement:tag=>new Element(tag),createTextNode:text=>{const n=new Element('text');n.textContent=text;return n;},querySelector:s=>s==='#app'?root:body.querySelector(s),querySelectorAll:s=>body.querySelectorAll(s),addEventListener(){}};
 body.append(root);
 const events={},calls=[];
 let drag, bridgeReport;
 const noop=()=>{};
 const context={document,location:{search},URLSearchParams,setTimeout,clearTimeout,nextNotice,
  window:{addEventListener:noop,getSelection:()=>({isCollapsed:true})},
  Events:{On:(name,fn)=>events[name]=fn,Emit:(...args)=>{calls.push(args);return Promise.resolve();}},Window:{},
  createBridge:(send,report)=>{bridgeReport=report;return {emit:async(n,d)=>{await send(`devjay:${n}`,d);report('');},reply:noop};},
  uiReady:()=>noop,installCardSizing:()=>noop,mountEditorSettings:()=>({renderEditor:noop}),
  mountGitViewer:()=>noop,cardStatus,
  mountSuggestedActions:()=>noop,mountViewerSuggestions:()=>noop,
  installPanelDrag:(_list,options)=>{drag=options;},installCardActivation,installDragOriginGuard:noop,installCardHover:noop,installCardMenu:noop,
 };
 vm.runInNewContext(source,context);
 return {document,root,calls,reportBridge:(text,error)=>bridgeReport(text,error),event:(name,data)=>events[name]?.({data}),get drag(){return drag;},snapshot:s=>events['devjay:snapshot']({data:s}),click:target=>{
  const event={button:0,defaultPrevented:false,target};
  for(let node=target;node;node=node.parent)for(const listener of node.listeners.click || [])listener(event);
 }};
}
const site=(id,branch='main')=>({id,path:`/tmp/${id}`,prefix:'/tmp/',name:id,git:{branch},position:{label:'Desktop 1'},window:{}});
const state=sites=>({sites,preferences:{},layout:'horizontal',settingsOpen:true});
const appLabels=view=>view.root.querySelectorAll('.development-app-group').map(group=>group.attrs['aria-label']);

test('ending a panel drag renders the latest snapshot even after cancellation',()=>{
 const view=app();
 view.snapshot(state([site('a','old')]));
 view.drag.busy(true);
 view.snapshot(state([site('a','new')]));
 assert.equal(view.root.querySelector('.branch').textContent,'old');
 view.drag.busy(false);
 assert.equal(view.root.querySelector('.branch').textContent,'new');
});

test('keyboard reordering preserves the same Site row focus across snapshots',async()=>{
 const view=app('?settings');
 view.snapshot(state([site('a'),site('b')]));
 const row=view.root.querySelectorAll('.site-row')[0];row.focus();
 row.onkeydown({target:row,key:'ArrowDown',preventDefault(){}});
 await Promise.resolve();
 assert.deepEqual(Array.from(view.calls.find(([name])=>name==='devjay:order')[1]),['b','a']);
 view.snapshot(state([site('b'),site('a')]));
 assert.equal(view.document.activeElement.dataset.id,'a');
 assert.notEqual(view.document.activeElement,row);
});

test('Changes and confirmation views contain the shared error footer',()=>{
 for(const search of ['?viewer=viewer-1','?actions'])assert.ok(app(search).root.querySelector('.notice'));
});

test('snapshot notices clear when resolved in every view',()=>{
 for(const search of ['', '?site=a', '?settings', '?viewer=viewer-1', '?actions']) {
  const view=app(search), snapshot=state([site('a')]);
  const notice=view.root.querySelector('.notice');
  view.snapshot({...snapshot,quitMessage:'Waiting for the confirmed action to finish before quitting…',error:'An earlier error'});
  assert.match(notice.textContent,/Waiting/);
  assert.equal(notice.classList.contains('error'),false);
  view.snapshot(snapshot);
  assert.equal(notice.textContent,'',search);
  view.snapshot({...snapshot,error:'Could not read Sites'});
  assert.equal(notice.textContent,'Could not read Sites');
  assert.equal(notice.classList.contains('error'),true);
  view.snapshot(snapshot);
  assert.equal(notice.textContent,'',search);
  assert.equal(notice.classList.contains('error'),false);
 }
});

test('unrelated snapshots preserve bridge errors until a successful request',()=>{
 const view=app('?settings'), snapshot=state([site('a')]);
 const notice=view.root.querySelector('.notice');
 view.reportBridge('Could not save Sites',true);
 view.snapshot(snapshot);
 assert.equal(notice.textContent,'Could not save Sites');
 assert.equal(notice.classList.contains('error'),true);
 view.reportBridge('',false);
 assert.equal(notice.textContent,'');
 assert.equal(notice.classList.contains('error'),false);
});

test('successful bridge calls do not clear an active snapshot notice',()=>{
 const view=app('?settings');
 view.snapshot({...state([site('a')]),quitMessage:'Finishing the action…'});
 view.reportBridge('',false);
 assert.equal(view.root.querySelector('.notice').textContent,'Finishing the action…');
});


test('PR tags live in card headers, open the PR, and disappear when the PR is gone',async()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  item.git={branch:'feature',github:'PR #1234 · Approved',pullRequests:[{number:1234,url:'https://github.com/a/b/pull/1234',state:'OPEN'}]};
  view.snapshot(state([item]));
  const tag=view.root.querySelector('.card-heading').querySelector('.pr-tag');
  assert.equal(tag.textContent,'#1234');assert.equal(tag.dataset.prNumber,'1234');
  assert.equal(view.root.querySelector('.card-settings'),null);
  await tag.onclick();
  const request=view.calls.find(([name])=>name==='devjay:pr-open')[1];
  assert.equal(request.siteID,'a');assert.equal(request.number,1234);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'||name==='devjay:viewer-open'),false);
  view.snapshot(state([{...item,git:{branch:'feature',github:'No open pull request'}}]));
  assert.equal(view.root.querySelector('.pr-tag'),null);
  assert.equal(view.root.querySelector('.pr-status'),null);
 }
});

test('edit and commit badges retain their category for click and context menus',async()=>{
 const view=app(),item=site('a');
 item.git={branch:'feature',ahead:2,behind:3,changes:25};view.snapshot(state([item]));
 const badges=view.root.querySelectorAll('.metric');
 assert.deepEqual(badges.map(n=>n.dataset.viewKind),['local','edits','remote']);
 for(const badge of badges)await badge.onclick();
 assert.deepEqual(view.calls.filter(([name])=>name==='devjay:viewer-open').map(([,data])=>data.kind),['local','edits','remote']);
});


test('PR navigation errors appear only in the requesting card window',()=>{
 const panel=app(),siteView=app('?site=a');
 const result={siteID:'a',number:1234,sender:'control-panel',error:'Browser did not confirm opening the PR.'};
 panel.event('devjay:pr-open-result',result);siteView.event('devjay:pr-open-result',result);
 assert.equal(panel.root.querySelector('.notice').textContent,result.error);
 assert.equal(panel.root.querySelector('.notice').classList.contains('error'),true);
 assert.equal(siteView.root.querySelector('.notice').textContent,'');
});

test('development app indicators update independently of Git and distinguish unavailable from no match',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  const indicator=()=>view.root.querySelector('.development-app');
  view.snapshot(state([item]));
  assert.equal(indicator().textContent,'App unknown');
  assert.equal(indicator().closest('[data-app-tooltip]'),undefined);
  const goLand={name:'GoLand',bundleID:'com.jetbrains.goland'};
  const iTerm={name:'iTerm',bundleID:'com.googlecode.iterm2'};
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[goLand]}]));
  assert.deepEqual(appLabels(view),['GoLand']);
  assert.equal(indicator().querySelector('[data-app-tooltip]').dataset.appTooltip,'GoLand');
  const previousIndicator=indicator();
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[goLand]}]));
  assert.equal(indicator(),previousIndicator,'unchanged readings keep the current DOM');
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[goLand,iTerm]}]));
  assert.deepEqual(appLabels(view),['GoLand','iTerm']);
  assert.deepEqual(indicator().querySelectorAll('[data-app-tooltip]').map(node=>node.dataset.appTooltip),['GoLand','iTerm']);
  assert.equal(indicator().parent.children.at(-1),indicator());
  assert.equal(indicator().tag,'div');
  assert.equal(indicator().onclick,undefined);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false,'observations never activate a Site');
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[]}]));
  assert.equal(indicator().textContent,'No app detected');
  assert.equal(indicator().querySelector('[data-app-tooltip]'),null);
  view.snapshot(state([{...item,developmentAppsKnown:false,developmentApps:[goLand]}]));
  assert.equal(indicator().textContent,'App unknown','unavailable readings do not display stale app names');
 }
});

test('agent presence updates without app or Git changes and keeps full app and agent names in both views',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  const goLand={name:'GoLand',bundleID:'com.jetbrains.goland'};
  const indicator=()=>view.root.querySelector('.development-app');
  const show=apps=>view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:apps}]));
  show([goLand]);
  assert.deepEqual(appLabels(view),['GoLand']);
  const before=indicator();
  show([{...goLand,agents:['Claude Code']}]);
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  assert.notEqual(indicator(),before,'agent-only changes refresh the indicator');
  const unchanged=indicator();
  show([{...goLand,agents:['Claude Code']}]);
  assert.equal(indicator(),unchanged,'unchanged agent readings preserve the DOM');
  show([
   {...goLand,agents:['Claude Code','Codex']},
   {name:'iTerm2',bundleID:'com.googlecode.iterm2',agents:['Codex']},
   {name:'ChatGPT',bundleID:'com.openai.chat'},
   {name:'Claude',bundleID:'com.anthropic.claudefordesktop',agents:[]},
  ]);
  assert.deepEqual(appLabels(view),['GoLand with Claude Code and Codex','iTerm2 with Codex','ChatGPT','Claude']);
  assert.deepEqual(indicator().querySelectorAll('[data-app-tooltip]').map(node=>node.dataset.appTooltip),['GoLand with Claude Code and Codex','iTerm2 with Codex','ChatGPT','Claude']);
  assert.doesNotMatch(indicator().textContent,/working|waiting|idle|blocked/i);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false,'presence updates do not activate a Site');
  show([{...goLand,agents:[]}]);
  assert.deepEqual(appLabels(view),['GoLand'],'cleared agent readings do not retain stale labels');
 }
});

test('IDE status setting defaults on and emits boolean preference changes',async()=>{
 const view=app('?settings'), snapshot=state([site('a')]);
 view.snapshot(snapshot);
 const checkbox=view.root.querySelectorAll('[data-pref]').find(node=>node.dataset.pref==='showIDEStatus');
 assert.ok(checkbox);
 assert.equal(checkbox.parent.children[1].textContent,'Show development app status');
 assert.equal(checkbox.checked,true,'snapshots without the new preference keep status enabled');
 checkbox.checked=false;await checkbox.onchange();
 view.snapshot({...snapshot,preferences:{showIDEStatus:false}});
 assert.equal(checkbox.checked,false);
 checkbox.checked=true;await checkbox.onchange();
 view.snapshot({...snapshot,preferences:{showIDEStatus:true}});
 assert.equal(checkbox.checked,true);
 assert.deepEqual(view.calls.filter(([name])=>name==='devjay:showIDEStatus'),[
  ['devjay:showIDEStatus',false],['devjay:showIDEStatus',true],
 ]);
});

test('IDE status can be hidden and restored without app or Git changes in both card views',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  const snapshot=state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',bundleID:'com.jetbrains.goland',agents:['Claude Code']}]}]);
  view.snapshot(snapshot);
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  view.snapshot({...snapshot,preferences:{showIDEStatus:false}});
  assert.equal(view.root.querySelector('.development-app'),null);
  assert.equal(view.root.querySelector('.branch').textContent,'main');
  view.snapshot({...snapshot,preferences:{showIDEStatus:true}});
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

const appIcon='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF9sAAAAASUVORK5CYII=';

test('grouped app and agent icons preserve passive card click behavior in both views',()=>{
 for(const search of ['', '?site=a']) for(const icon of [appIcon,undefined]) {
  const view=app(search);
  view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'iTerm2',bundleID:'com.googlecode.iterm2',agents:['Codex'],icon}]}]));
  const indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.tag,'div');
  assert.equal(indicator.onclick,undefined);
  assert.equal(indicator.querySelector('button'),null);
  const targets=[indicator,...indicator.querySelectorAll('img'),...indicator.querySelectorAll('.development-app-initials'),...indicator.querySelectorAll('[data-app-tooltip]')];
  for(const target of targets) {
   const before=view.calls.filter(([name])=>name==='devjay:activate').length;
   view.click(target);
   const activations=view.calls.filter(([name])=>name==='devjay:activate');
   assert.equal(activations.length,before+(search?0:1));
   if(!search) assert.deepEqual(activations.at(-1),['devjay:activate','a']);
  }
  assert.equal(view.calls.some(([name])=>name==='devjay:viewer-open'),false);
 }
});

test('legacy Detailed preference is ignored and Settings exposes only the app-status toggle',()=>{
 const settings=app('?settings');
 settings.snapshot({...state([site('a')]),appStatusView:'detailed'});
 assert.equal(settings.root.querySelectorAll('select').some(node=>node.attrs['aria-label']==='App status view'),false);
 assert.equal(settings.calls.some(([name])=>name==='devjay:app-status-view'),false);
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],icon:appIcon}]};
  view.snapshot(state([item]));
  const indicator=view.root.querySelector('.development-app');
  const refreshes=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
  assert.equal(indicator.querySelectorAll('img').length,2);
  assert.equal(refreshes(),1);
  view.snapshot({...state([item]),appStatusView:'detailed'});
  assert.equal(view.root.querySelector('.development-app'),indicator,'legacy appearance does not rebuild the icons');
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  assert.equal(refreshes(),1,'ignored appearance does not trigger tooltip refreshes');
 }
});

test('agent changes retain or remove the correct capsule in both views',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],icon:appIcon}]};
  const indicator=()=>view.root.querySelector('.development-app');
  view.snapshot(state([item]));
  assert.equal(indicator().querySelectorAll('img').length,2);
  assert.equal(indicator().querySelector('.development-app-group').classList.contains('has-agents'),true);
  const original=indicator();
  view.snapshot(state([item]));assert.equal(indicator(),original);
  const changed={...item,developmentApps:[{...item.developmentApps[0],agents:[]}]};
  view.snapshot(state([changed]));
  assert.equal(indicator().querySelectorAll('img').length,1);
  assert.equal(indicator().querySelector('.development-app-group').classList.contains('has-agents'),false);
  view.snapshot({...state([changed]),preferences:{showIDEStatus:false}});assert.equal(indicator(),null);
  view.snapshot(state([changed]));assert.equal(indicator().querySelectorAll('img').length,1);
 }
});

test('host icons accept only PNG data and decorative icons fall back without changing group identity',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true};
  const icons=[appIcon,undefined,'https://example.com/icon.png','file:///Applications/App.app/icon.png','data:image/svg+xml;base64,PHN2Zz4=','data:image/png;base64,invalid!'];
  view.snapshot(state([{...item,developmentApps:icons.map((icon,index)=>({name:index===0?'GoLand':'Visual Studio',icon}))}]));
  const slots=view.root.querySelectorAll('.development-app-icon');
  assert.equal(slots.length,icons.length);
  assert.equal(view.root.querySelectorAll('img').length,1,'external, file and non-PNG host sources are never loaded');
  const image=slots[0].querySelector('img'), fallback=slots[0].querySelector('.development-app-initials');
  const group=slots[0].closest('[data-app-tooltip]');
  assert.equal(group.attrs['aria-label'],'GoLand');
  assert.equal(image.src,appIcon);assert.equal(image.alt,'');assert.equal(image.draggable,false);
  assert.equal(image.attrs['aria-hidden'],'true');assert.equal(slots[0].attrs['aria-hidden'],'true');
  assert.equal(image.width,22);assert.equal(image.height,22);
  assert.equal(fallback.textContent,'GL');
  assert.equal(slots[1].querySelector('.development-app-initials').textContent,'VS');
  assert.equal(image.hidden,true);
  image.onload();assert.equal(image.hidden,false);assert.equal(fallback.hidden,true);
  image.onerror();assert.equal(slots[0].querySelector('img'),null);assert.equal(fallback.hidden,false);
  assert.equal(fallback.closest('[data-app-tooltip]'),group);
  assert.equal(view.root.querySelectorAll('.development-app-icon').length,icons.length,'failed images retain their fixed icon slots');
 }
});

test('concise hover targets cover only grouped capsules or single icons',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[
   {name:'GoLand',agents:['Claude Code','Codex'],windowCount:2,icon:appIcon},
   {name:'iTerm2',agents:['Codex'],windowCount:1},
   {name:'ChatGPT',icon:appIcon},
  ]};
  view.snapshot(state([item]));
  const indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.closest('[data-app-tooltip]'),undefined,'row whitespace and outer padding are not hover targets');
  assert.equal(indicator.title,undefined);
  const groups=indicator.querySelectorAll('[data-app-tooltip]');
  assert.deepEqual(groups.map(group=>group.dataset.appTooltip),['GoLand with Claude Code and Codex','iTerm2 with Codex','ChatGPT']);
  for(const group of groups) {
   assert.equal(group.title,undefined);
   assert.equal(group.attrs['aria-label'],group.dataset.appTooltip);
   assert.equal(group.querySelector('[data-app-tooltip]'),null,'one tooltip target covers the entire app group');
   assert.doesNotMatch(group.dataset.appTooltip,/waiting|working|idle|blocked|window|folder/i);
  }
  assert.equal(groups[0].attrs.role,'img');
  assert.equal(groups[0].classList.contains('has-agents'),true);
  assert.equal(groups[2].classList.contains('has-agents'),false);
  assert.equal(groups[2].querySelector('.development-app-agents'),null);
  for(const group of groups) for(const child of [...group.querySelectorAll('img'),...group.querySelectorAll('.development-app-initials')]) {
   assert.equal(child.closest('[data-app-tooltip]'),group,'all child icons use their owning app’s single hover target');
  }
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('bundled agent icons preserve host association and order independently of installed standalone apps',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[
   {name:'GoLand',agents:['Claude Code','Codex'],icon:appIcon},
   {name:'VS Code',agents:['Codex','Claude Code']},
   {name:'iTerm2',agents:['Other Agent']},
  ]}]));
  const groups=view.root.querySelectorAll('.development-app-group');
  assert.equal(groups.length,3);
  assert.deepEqual(groups[0].querySelectorAll('img').map(img=>[img.src,img.width,img.height]),[[appIcon,22,22],['/icons/claude.png',16,16],['/icons/codex.png',16,16]]);
  assert.deepEqual(groups[1].querySelectorAll('img').map(img=>img.src),['/icons/codex.png','/icons/claude.png']);
  const agent=groups[0].querySelector('.development-agent-icon');
  const image=agent.querySelector('img'), fallback=agent.querySelector('.development-app-initials');
  assert.equal(fallback.textContent,'CC');
  image.onload();assert.equal(fallback.hidden,true);
  image.onerror();assert.equal(fallback.hidden,false);assert.equal(agent.querySelector('img'),null);
  assert.equal(fallback.closest('[data-app-tooltip]'),groups[0]);
  assert.equal(groups[2].querySelector('.development-agent-icon').querySelector('.development-app-initials').textContent,'OA');
 }
});

test('unknown and empty app readings retain short text without stale icons or hover targets',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentApps:[{name:'GoLand',icon:appIcon}]};
  const show=value=>view.snapshot(state([value]));
  show({...item,developmentAppsKnown:true});
  show({...item,developmentAppsKnown:false});
  let indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.textContent,'App unknown');assert.equal(indicator.querySelector('img'),null);
  assert.equal(indicator.querySelector('[data-app-tooltip]'),null);assert.equal(indicator.closest('[data-app-tooltip]'),undefined);
  show({...item,developmentAppsKnown:true,developmentApps:[]});
  indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.textContent,'No app detected');assert.equal(indicator.querySelector('img'),null);
  assert.equal(indicator.querySelector('[data-app-tooltip]'),null);assert.equal(indicator.closest('[data-app-tooltip]'),undefined);
 }
});

test('tooltip refresh follows rendered app content and visibility changes, without Git-poll noise',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],windowCount:1,icon:appIcon}]};
  const updates=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh');
  const initial=state([item]);
  view.snapshot(initial);
  assert.equal(updates().length,1,'initial render refreshes after reload');
  assert.equal(updates()[0][1],undefined,'the event carries no state or icon payload');
  view.snapshot(initial);
  view.snapshot(state([{...item,git:{...item.git,checked:'later',branch:'new-branch',changes:4}}]));
  view.snapshot(state([{...item,developmentApps:[{...item.developmentApps[0],icon:undefined}]}]));
  assert.equal(updates().length,1,'unchanged, Git-only, and fixed-size icon changes do not refresh');
  const changed={...item,developmentApps:[{...item.developmentApps[0],agents:['Codex'],windowCount:2}]};
  view.snapshot(state([changed]));
  assert.equal(updates().length,2);
  assert.equal(view.root.querySelector('[data-app-tooltip]').dataset.appTooltip,'GoLand with Codex');
  view.snapshot({...state([changed]),preferences:{showIDEStatus:false}});assert.equal(updates().length,3);
  assert.equal(view.root.querySelector('.development-app'),null);
  view.snapshot(state([changed]));assert.equal(updates().length,4);
  view.snapshot(state([{...changed,path:'/tmp/moved',position:{label:'Desktop 4'}}]));assert.equal(updates().length,5);
 }
});

test('tooltip refresh includes card placement and order, and defers until the final panel-drag render',()=>{
 const view=app(), a={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand'}]}, b=site('b');
 const count=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
 view.snapshot(state([a,b]));assert.equal(count(),1);
 view.snapshot(state([b,a]));assert.equal(count(),2);
 const custom={...state([b,a]),layout:'custom',placements:{a:{x:1,y:2,z:1},b:{x:3,y:4,z:2}}};
 view.snapshot(custom);assert.equal(count(),3);
 view.snapshot({...custom,placements:{...custom.placements,a:{x:40,y:50,z:3}}});assert.equal(count(),4);
 view.drag.busy(true);
 view.snapshot({...custom,sites:[b,{...a,developmentApps:[{name:'GoLand',agents:['Codex']}]}]});
 assert.equal(count(),4,'deferred snapshots do not refresh against stale DOM');
 view.drag.busy(false);assert.equal(count(),5);
 assert.deepEqual(appLabels(view),['GoLand with Codex']);
 view.snapshot(state([]));assert.equal(count(),6,'removing the last card dismisses its tooltip');
 assert.equal(view.root.querySelector('.development-app'),null);
});

test('settings, viewer, and action snapshots never request app tooltip refresh',()=>{
 for(const search of ['?settings','?viewer=viewer-1','?actions']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand'}]};
  view.snapshot(state([item]));
  view.snapshot(state([{...item,developmentApps:[{name:'GoLand',agents:['Codex']}]}]));
  view.snapshot({...state([item]),preferences:{showIDEStatus:false}});
  assert.equal(view.calls.some(([name])=>name==='devjay:app-tooltip-refresh'),false,search);
 }
});
