import test from 'node:test';
import assert from 'node:assert/strict';
import {fitPathLabel} from '../cmd/devjay/frontend/path-label.js';
const mono=text=>Array.from(new Intl.Segmenter(undefined,{granularity:'grapheme'}).segment(text)).length;
const fit=(prefix,name,width)=>fitPathLabel(prefix,name,width,mono,mono);

test('full paths stay intact and truncated paths keep their nearest directory suffix',()=>{
 assert.deepEqual(fit('~/abcxyz/','longname',17),{prefix:'~/abcxyz/',name:'longname'});
 assert.deepEqual(fit('~/abcxyz/','longname',15),{prefix:'...xyz/',name:'longname'});
 assert.deepEqual(fit('~/abcxyz/','longname',10),{prefix:'',name:'longname'});
 assert.deepEqual(fit('~/abcxyz/','longname',8),{prefix:'',name:'longname'});
});

test('a name that cannot fit loses its directory and keeps both ends',()=>{
 assert.deepEqual(fit('~/abcxyz/','extremelylongname',12),{prefix:'',name:'extre...name'});
 assert.deepEqual(fit('','extremelylongname',12),{prefix:'',name:'extre...name'});
 for(let width=0;width<25;width++){
  const result=fit('~/abcxyz/','extremelylongname',width);
  assert.ok(mono(result.prefix)+mono(result.name)<=width);
 }
});

test('measurement respects proportional fonts and distinct path/name weights',()=>{
 const prefix=text=>text.length, name=text=>[...text].reduce((n,c)=>n+(c==='W'?4:2),0);
 const result=fitPathLabel('~/directory/','WiWiWiWi',15,prefix,name);
 assert.equal(result.prefix,'');assert.match(result.name,/^W.*\.\.\..*i$/);
 assert.ok(name(result.name)<=15);
 assert.deepEqual(fitPathLabel('~/directory/','WW',8,prefix,name),{prefix:'',name:'WW'});
});

test('shortening never splits combining marks or emoji sequences',()=>{
 const name='e\u0301👩‍💻abcdefghij👨‍👩‍👧‍👦🇳🇴';
 assert.deepEqual(fit('~/项目/目录/',name,9),{prefix:'',name:'e\u0301👩‍💻a...j👨‍👩‍👧‍👦🇳🇴'});
 const shortened=fit('~/项目/目录/','项目',7);
 assert.equal(shortened.prefix,'...录/');assert.equal(shortened.name,'项目');
});
