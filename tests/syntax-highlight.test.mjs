import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';

const source=await readFile(new URL('../cmd/devjay/frontend/syntax-highlight.js',import.meta.url),'utf8');
const {syntaxLanguage,syntaxLines,appendSyntax}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const textOf=lines=>lines.map(line=>line.map(token=>token.text).join('')).join('\n');
const kindsFor=(lines,text)=>lines.flat().filter(token=>token.text===text).map(token=>token.kind);
const allPlain=lines=>lines.flat().every(token=>token.kind==='plain');

test('language detection uses known filenames/extensions and conservative executable shebangs',()=>{
  for(const [path,language] of [['main.go','go'],['main.TSX','typescript'],['client.jsx','javascript'],['x.py','python'],['worker.rs','rust'],['View.swift','swift'],['App.mm','objectivec'],['types.hpp','cpp'],['main.c','c'],['App.cs','csharp'],['App.java','java'],['config.JSON','json'],['theme.scss','css'],['index.html','html'],['image.svg','xml'],['build.sh','shell'],['Makefile','shell'],['.zshrc','shell'],['ci.yaml','yaml'],['Cargo.toml','toml'],['query.sql','sql'],['README.md','markdown'],['Dockerfile.dev','docker']]) {
    assert.equal(syntaxLanguage(path),language,path);
  }
  assert.equal(syntaxLanguage('tool','#!/usr/bin/env python3\nprint(1)'),'python');
  assert.equal(syntaxLanguage('tool','#!/usr/bin/env -S node --no-warnings\nconst x=1'),'javascript');
  assert.equal(syntaxLanguage('notes.txt','#!/bin/bash\nif true; then echo hi; fi'),'');
  assert.equal(syntaxLanguage('unknown.data','const pretend = 2'),'');
  assert.equal(syntaxLanguage('notes','prose about python and node'),'');
});

test('source is reconstructed exactly, including Unicode, HTML-looking text, CRLF and final newlines',()=>{
  for(const text of ['', '\n', '\n\n', 'package main\r\n\r\n// 漢字 😀\r\nvar s = "</script><img onerror=boom>"\r\n', 'const x = "no final newline"', 'value\u0000other\n']) {
    const lines=syntaxLines('fixture.go',text);
    assert.equal(textOf(lines),text);
    assert.equal(lines.length,text.split('\n').length);
    assert.ok(lines.flat().every(token=>!token.text.includes('\n')));
  }
});

test('Go highlights comments, strings, keywords, types, constants, calls and numbers',()=>{
  const lines=syntaxLines('main.go','package main\n// comment\nfunc main() {\n const count = 42\n var label string = "http://example.test"\n println(label, true)\n}\n');
  assert.deepEqual(kindsFor(lines,'package'),['keyword']);
  assert.deepEqual(kindsFor(lines,'func'),['keyword']);
  assert.deepEqual(kindsFor(lines,'const'),['keyword']);
  assert.deepEqual(kindsFor(lines,'string'),['type']);
  assert.deepEqual(kindsFor(lines,'42'),['number']);
  assert.deepEqual(kindsFor(lines,'true'),['constant']);
  assert.deepEqual(kindsFor(lines,'println'),['function']);
  assert.deepEqual(kindsFor(lines,'// comment'),['comment']);
  assert.deepEqual(kindsFor(lines,'"http://example.test"'),['string']);
});

test('multiline comments and raw strings retain state across lines and reset for the next file/version',()=>{
  const before=syntaxLines('main.go','/* old\nstill comment */\nvar text = `raw\n// not a comment`\nreturn text');
  assert.ok(before[0].every(token=>token.kind==='comment'));
  assert.ok(before[1].every(token=>token.kind==='comment'));
  assert.ok(before[3].every(token=>token.kind==='string'));
  assert.deepEqual(kindsFor(before,'return'),['keyword']);
  syntaxLines('old.go','/* never closed');
  assert.deepEqual(kindsFor(syntaxLines('new.go','return nil'),'return'),['keyword']);
  const python=syntaxLines('module.py','value = """first\n# still a string\nlast"""\n# comment\nreturn None');
  assert.ok(python[1].every(token=>token.kind==='string'));
  assert.deepEqual(kindsFor(python,'# comment'),['comment']);
  assert.deepEqual(kindsFor(python,'None'),['constant']);
});

test('escaped quotes do not expose string contents as comments or source code',()=>{
  const text='const text = "escaped \\\" // still string"; // real comment\nconst template = `line\n${value} // template`;';
  const lines=syntaxLines('code.js',text);
  assert.equal(textOf(lines),text);
  assert.deepEqual(kindsFor(lines,'// real comment'),['comment']);
  assert.ok(lines[0].some(token=>token.kind==='string'&&token.text.includes('// still string')));
  assert.ok(lines[2].some(token=>token.kind==='string'&&token.text.includes('// template')));
});

test('HTML/XML stays literal and distinguishes tags, attributes, strings and multiline comments',()=>{
  const text='<svg width="12">\n<!-- literal\n<script> -->\n<path data-value="<&>"/>\n</svg>';
  const lines=syntaxLines('fixture.svg',text);
  assert.equal(textOf(lines),text);
  assert.deepEqual(kindsFor(lines,'svg'),['tag','tag']);
  assert.deepEqual(kindsFor(lines,'width'),['attribute']);
  assert.deepEqual(kindsFor(lines,'data-value'),['attribute']);
  assert.deepEqual(kindsFor(lines,'"<&>"'),['string']);
  assert.ok(lines[2].every(token=>token.kind==='comment'));
});

test('JSON, CSS, YAML, TOML, SQL, shell and Markdown have basic file-specific highlighting',()=>{
  assert.deepEqual(kindsFor(syntaxLines('config.json','{"enabled": true, "count": 3}'),'"enabled"'),['property']);
  const css=syntaxLines('theme.css','.card { background-color: #ff00aa; width: 12px; /* note */ }');
  assert.deepEqual(kindsFor(css,'background-color'),['property']);
  assert.deepEqual(kindsFor(css,'#ff00aa'),['number']);
  const yaml=syntaxLines('config.yaml','service-name: https://example.test/#fragment # note\nenabled: true');
  assert.deepEqual(kindsFor(yaml,'service-name'),['property']);
  assert.deepEqual(kindsFor(yaml,'# note'),['comment']);
  assert.ok(!yaml.flat().some(token=>token.kind==='comment'&&token.text.includes('fragment')));
  assert.deepEqual(kindsFor(syntaxLines('Cargo.toml','package-name = "demo"'),'package-name'),['property']);
  assert.deepEqual(kindsFor(syntaxLines('query.sql','SELECT name FROM users WHERE id = 42 -- comment'),'SELECT'),['keyword']);
  assert.deepEqual(kindsFor(syntaxLines('build.sh','if test "$value"; then # note\n echo ok\nfi'),'if'),['keyword']);
  const markdown=syntaxLines('README.md','# Heading\n```go\nreturn value\n```\nplain');
  assert.ok(markdown[0].every(token=>token.kind==='keyword'));
  assert.ok(markdown[2].every(token=>token.kind==='string'));
  assert.ok(markdown[4].every(token=>token.kind==='plain'));
});

test('Rust lifetimes do not consume the following source as quoted strings',()=>{
  const lines=syntaxLines('main.rs',"fn name<'a>(value: &'a str) -> &'a str { return value; }");
  assert.deepEqual(kindsFor(lines,'fn'),['keyword']);
  assert.deepEqual(kindsFor(lines,'return'),['keyword']);
  assert.ok(!lines.flat().some(token=>token.kind==='string'));
});

test('unknown files and bounded-input fallbacks preserve every source character as plain text',()=>{
  for(const [path,text] of [
    ['notes.txt','return "plain"\n'],
    ['file.go','a'.repeat(8193)+'\n'],
    ['file.go',('const n = 1\n').repeat(25000)],
    ['file.js',('a+'.repeat(500)+'\n').repeat(40)],
  ]) {
    const lines=syntaxLines(path,text);
    assert.equal(textOf(lines),text);
    assert.ok(allPlain(lines),`expected bounded plain fallback for ${path} (${text.length})`);
  }
});

test('safe DOM rendering uses text nodes via textContent, fixed classes and exact range slices',()=>{
  const document={createElement(tag){assert.equal(tag,'span');return {className:'',textContent:'',set innerHTML(_){throw new Error('HTML insertion is forbidden');}};}};
  const parent=()=>({ownerDocument:document,children:[],append(node){this.children.push(node);}});
  const text='const x = "</script>😀";';
  const tokens=syntaxLines('a.js',text)[0];
  const all=parent();appendSyntax(all,text,tokens);
  assert.equal(all.children.map(node=>node.textContent).join(''),text);
  assert.ok(all.children.some(node=>node.className==='syntax-keyword'));
  const partial=parent();appendSyntax(partial,text,tokens,3,18);
  assert.equal(partial.children.map(node=>node.textContent).join(''),text.slice(3,18));
  const invalid=parent();appendSyntax(invalid,text,[{text:'different source',kind:'keyword'}]);
  assert.equal(invalid.children[0].textContent,text);assert.equal(invalid.children[0].className,'');
  const unsafe=parent();appendSyntax(unsafe,'x',[{text:'x',kind:'keyword onclick=boom'}]);
  assert.equal(unsafe.children[0].className,'');assert.equal(unsafe.children[0].textContent,'x');
});
