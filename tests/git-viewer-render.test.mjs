import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';

const editorSource=(await readFile(new URL('../cmd/devjay/frontend/file-editor.js',import.meta.url),'utf8')).replace(/^export /gm,'');
const viewerSource=(await readFile(new URL('../cmd/devjay/frontend/git-viewer.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'').replace(/^export /gm,'');

// Mount the production renderer. Fragments, focus and scroll are represented
// here so assertions cover updates to the actual diff DOM, not just helpers.
function viewer() {
  let document;
  class Element {
    constructor(tag) {
      this.tag=tag;this.children=[];this.dataset={};this.className='';this.attrs={};this.scrollTop=0;this.scrollLeft=0;
      this.classList={contains:c=>this.className.split(' ').includes(c),add:c=>this.classList.toggle(c,true),
        toggle:(c,on)=>{const classes=new Set(this.className.split(' ').filter(Boolean));on?classes.add(c):classes.delete(c);this.className=[...classes].join(' ');}};
    }
    append(...nodes) {
      for(const node of nodes) {
        if(node.tag==='fragment'){this.append(...node.children.slice());continue;}
        node.remove();node.parent=this;this.children.push(node);
      }
    }
    remove(){if(this.parent){const nodes=this.parent.children;nodes.splice(nodes.indexOf(this),1);this.parent=null;}}
    replaceChildren(...nodes){if(this.contains(document.activeElement))document.activeElement=document.body;for(const node of this.children)node.parent=null;this.children=[];this._text='';this.scrollTop=0;this.scrollLeft=0;this.append(...nodes);}
    set textContent(text){this.replaceChildren();this._text=String(text);}
    get textContent(){return (this._text||'')+this.children.map(node=>node.textContent).join('');}
    contains(node){return node===this||this.children.some(child=>child.contains(node));}
    setAttribute(key,value){this.attrs[key]=value;}
    matches(selector){return selector.startsWith('.')?this.classList.contains(selector.slice(1)):this.tag===selector;}
    querySelectorAll(selector){return this.children.flatMap(child=>[...(child.matches(selector)?[child]:[]),...child.querySelectorAll(selector)]);}
    querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
    focus(){document.activeElement=this;}
  }
  const body=new Element('body'),root=new Element('main');
  document={body,activeElement:body,createElement:tag=>new Element(tag),createDocumentFragment:()=>new Element('fragment')};
  body.append(root);
  const context=vm.createContext({document});
  vm.runInContext(editorSource+'\n'+viewerSource,context);
  return {root,document,render:context.mountGitViewer(root,()=>{})};
}

const file=(path='selected.go')=>({path,group:'Unstaged',status:'M',statsKnown:true,added:1,removed:1});
const state=overrides=>({siteID:'one',name:'one',kind:'edits',version:1,loading:false,branch:'feature',page:0,
  commit:'',commits:[],files:[file()],file:0,counts:{edits:1,local:1,remote:5},
  diff:'@@ -1 +1 @@\n-old\n+selected contents',...overrides});

test('rapid category changes back to the last rendered category restore the cleared diff',()=>{
  const view=viewer(),initial=state({kind:'remote',commit:'a',commits:[{id:'a',subject:'Selected',date:'2026-01-01'}]});
  view.render(initial);
  const diff=view.root.querySelector('.git-diff');
  assert.match(diff.textContent,/selected contents/);
  view.render({...initial,version:2,kind:'local',loading:true});
  assert.equal(diff.children.length,0);
  view.render({...initial,version:3,loading:true});
  view.render({...initial,version:3});
  assert.match(diff.textContent,/selected contents/);
  assert.ok(diff.children.length>0);
});

test('an unrelated refresh preserves selected diff nodes and both scroll axes',()=>{
  const view=viewer(),initial=state();view.render(initial);
  const diff=view.root.querySelector('.git-diff'),line=diff.children[2];
  diff.scrollTop=160;diff.scrollLeft=45;
  view.render({...initial,version:2,files:[file('earlier.go'),file()],file:1,counts:{edits:2,local:1,remote:5}});
  assert.equal(diff.children[2],line);
  assert.equal(diff.scrollTop,160);assert.equal(diff.scrollLeft,45);
});

test('updated selected-file contents rebuild the diff while preserving scroll',()=>{
  const view=viewer(),initial=state();view.render(initial);
  const diff=view.root.querySelector('.git-diff'),line=diff.children[2];
  diff.scrollTop=160;diff.scrollLeft=45;
  view.render({...initial,version:2,diff:'@@ -1 +1 @@\n-old\n+new contents'});
  assert.notEqual(diff.children[2],line);assert.match(diff.textContent,/new contents/);
  assert.equal(diff.scrollTop,160);assert.equal(diff.scrollLeft,45);
});

test('changing the selected file resets diff scroll even when its contents match',()=>{
  const view=viewer(),initial=state();view.render(initial);
  const diff=view.root.querySelector('.git-diff'),line=diff.children[2];
  diff.scrollTop=160;diff.scrollLeft=45;
  view.render({...initial,version:2,files:[file('other.go')]});
  assert.notEqual(diff.children[2],line);
  assert.equal(diff.scrollTop,0);assert.equal(diff.scrollLeft,0);
});

test('background updates preserve expanded commit messages until another commit is selected',()=>{
  const view=viewer(),initial=state({kind:'remote',commit:'a',message:'Subject\n\nDetailed commit message',
    commits:[{id:'a',subject:'Subject',date:'2026-01-01'}]});
  view.render(initial);
  const details=view.root.querySelector('.git-commit-detail').querySelector('details');
  details.open=true;
  view.render({...initial,version:2,counts:{edits:2,local:1,remote:5},files:[file('another.go')]});
  assert.equal(view.root.querySelector('.git-commit-detail').querySelector('details'),details);
  assert.equal(details.open,true);assert.match(details.textContent,/Detailed commit message/);
  view.render({...initial,version:3,commit:'b',message:'Another commit',commits:[{id:'b',subject:'Another commit',date:'2026-01-02'}]});
  const next=view.root.querySelector('.git-commit-detail').querySelector('details');
  assert.notEqual(next,details);assert.equal(!!next.open,false);assert.match(next.textContent,/Another commit/);
});
