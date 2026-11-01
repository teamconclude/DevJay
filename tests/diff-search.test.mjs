import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';

const syntaxSource=await readFile(new URL('../cmd/devjay/frontend/syntax-highlight.js',import.meta.url),'utf8');
const syntaxURL=`data:text/javascript;base64,${Buffer.from(syntaxSource).toString('base64')}`;
const source=(await readFile(new URL('../cmd/devjay/frontend/diff-search.js',import.meta.url),'utf8')).replace('./syntax-highlight.js',syntaxURL);
const {findTextMatches,filterSearchRows,appendSearchSyntax}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const {syntaxLines}=await import(syntaxURL);

test('literal search ignores case, escapes regex punctuation, and retains Unicode offsets',()=>{
  const literal='.*+?^${}()|[]\\';
  assert.deepEqual(findTextMatches(`prefix ${literal} suffix`,literal),[{start:7,end:7+literal.length}]);
  const text='İ 😀 TOKEN token';
  assert.deepEqual(findTextMatches(text,'ToKeN'),[{start:text.indexOf('TOKEN'),end:text.indexOf('TOKEN')+5},{start:text.indexOf('token'),end:text.length}]);
  assert.deepEqual(findTextMatches('a a',''),[]);
  assert.deepEqual(findTextMatches('a a',' '),[{start:1,end:2}]);
});

test('search filters either side with three context rows, merging overlap and keeping source identities',()=>{
  const rows=Array.from({length:30},(_,index)=>({before:`old ${index}`,after:`new ${index}`}));
  rows[8].before='NEEDLE original';rows[12].after='needle current needle';rows[24].after='needle';
  const result=filterSearchRows(rows,'needle',row=>[row.before,row.after]);
  assert.equal(result.matchCount,4);assert.equal(result.matchingRows,3);
  assert.deepEqual(result.rows.filter(row=>row.kind==='search-gap'),[
    {kind:'search-gap',count:5,from:0,to:5},
    {kind:'search-gap',count:5,from:16,to:21},
    {kind:'search-gap',count:2,from:28,to:30},
  ]);
  assert.equal(result.rows[1],rows[5]);assert.ok(result.rows.includes(rows[8]));assert.ok(result.rows.includes(rows[12]));
  assert.equal(rows.length,30);assert.ok(rows.every(row=>!('kind' in row)));
  assert.equal(filterSearchRows(rows,'',row=>[row.after]).rows,rows);
  assert.deepEqual(filterSearchRows(rows,'missing',row=>[row.before,row.after]),{rows:[],matchCount:0,matchingRows:0});
});

function element(tag='div'){
  const node={tag,className:'',children:[],_text:'',append(...nodes){this.children.push(...nodes);},
    set textContent(value){this._text=value;this.children=[];},get textContent(){return this._text+this.children.map(child=>child.textContent).join('');}};
  node.ownerDocument={createElement:element};return node;
}
const descendants=node=>node.children.flatMap(child=>[child,...descendants(child)]);

test('search highlights literal source while retaining syntax colors and range slicing',()=>{
  const text='const label = "<img onerror=boom>";',tokens=syntaxLines('fixture.js',text)[0],node=element();
  appendSearchSyntax(node,text,tokens,'label = "<img');
  assert.equal(node.textContent,text);assert.equal(descendants(node).some(child=>child.tag==='img'),false);
  const mark=descendants(node).find(child=>child.className==='diff-search-hit');
  assert.equal(mark.textContent,'label = "<img');
  assert.ok(descendants(node).some(child=>child.className==='syntax-keyword'&&child.textContent==='const'));
  assert.ok(descendants(mark).some(child=>child.className==='syntax-string'));
  const first=element(),second=element(),cut=text.indexOf('<img')+2;
  appendSearchSyntax(first,text,tokens,'<img',0,cut);appendSearchSyntax(second,text,tokens,'<img',cut,text.length);
  assert.equal(first.textContent+second.textContent,text);
  assert.equal(descendants(first).find(child=>child.className==='diff-search-hit').textContent,'<i');
  assert.equal(descendants(second).find(child=>child.className==='diff-search-hit').textContent,'mg');
  const plain=element();appendSearchSyntax(plain,text,null,'missing');assert.equal(plain.textContent,text);
});

test('repeated source limits highlight nodes while preserving full text and accurate match counts',()=>{
  const text='a'.repeat(4096),node=element();appendSearchSyntax(node,text,null,'a');
  const marks=root=>descendants(root).filter(child=>child.className==='diff-search-hit');
  assert.equal(marks(node).length,256);assert.equal(node.textContent,text);
  assert.equal(findTextMatches(text,'a').length,4096);
  const result=filterSearchRows([{text}],'a',row=>[row.text]);
  assert.equal(result.matchCount,4096);assert.equal(result.matchingRows,1);
  const first=element(),second=element();
  appendSearchSyntax(first,text,null,'a',0,100);appendSearchSyntax(second,text,null,'a',100,text.length);
  assert.equal(marks(first).length,100);assert.equal(marks(second).length,156);
  assert.equal(first.textContent+second.textContent,text);
});

test('one render budget bounds highlights across lines and sides without limiting source or match counts',()=>{
  const rows=Array.from({length:20},(_,index)=>({before:`${index}: ${'a'.repeat(200)}`,after:`${index}: ${'A'.repeat(200)}`}));
  const budget={remaining:2048};let marks=0;
  for(const row of rows)for(const text of [row.before,row.after]){
    const node=element();appendSearchSyntax(node,text,null,'a',0,text.length,budget);
    marks+=descendants(node).filter(child=>child.className==='diff-search-hit').length;
    assert.equal(node.textContent,text);
  }
  assert.equal(marks,2048);assert.equal(budget.remaining,0);
  const result=filterSearchRows(rows,'a',row=>[row.before,row.after]);
  assert.equal(result.matchCount,8000);assert.equal(result.matchingRows,20);
  assert.deepEqual(result.rows,rows);
});
