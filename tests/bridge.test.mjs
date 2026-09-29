import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source = await readFile(new URL('../cmd/devjay/frontend/bridge.js', import.meta.url), 'utf8');
const {createBridge} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

test('bridge failures surface and a successful retry clears the message', async () => {
 const reports=[];
 let fail=true;
 const bridge=createBridge(async()=>{if(fail)throw new Error('offline');},(...args)=>reports.push(args),30);
 assert.equal(await bridge.emit('hello'),false);
 assert.deepEqual(reports.at(-1),['offline',true]);
 fail=false;
 assert.equal(await bridge.emit('hello'),true);
 assert.deepEqual(reports.at(-1),['',false]);
});

test('actions wait for acknowledgement and show backend rejections',async()=>{
 const reports=[];
 let sent, bridge=createBridge(async(name,data)=>{sent=[name,data];},(...args)=>reports.push(args),100);
 const pending=bridge.emit('action-confirm','token');
 await Promise.resolve();await Promise.resolve();
 assert.equal(sent[0],'devjay:action-confirm');assert.equal(sent[1].token,'token');
 bridge.reply({requestID:sent[1].requestID,error:'Site is not ready'});
 assert.equal(await pending,false);assert.deepEqual(reports.at(-1),['Site is not ready',true]);
 const retry=bridge.emit('action-prepare',{siteID:'site'});
 await Promise.resolve();await Promise.resolve();
 bridge.reply({requestID:sent[1].requestID});
 assert.equal(await retry,true);
});

test('missing acknowledgement and hung transport time out without retrying actions',async()=>{
 for (const send of [()=>Promise.resolve(),()=>new Promise(()=>{})]) {
  const reports=[];
  const bridge=createBridge(send,(...args)=>reports.push(args),5);
  assert.equal(await bridge.emit('action-confirm','token'),false);
  assert.match(reports.at(-1)[0],/timed out/);
 }
});

test('branch chooser opening waits for the Site-bound backend acknowledgement',async()=>{
 const reports=[];
 let sent;
 const bridge=createBridge(async(name,data)=>{sent=[name,data];},(...args)=>reports.push(args),100);
 const opened=bridge.emit('branches-open',{siteID:'site-one'});
 await Promise.resolve();await Promise.resolve();
 assert.equal(sent[0],'devjay:branches-open');
 assert.equal(sent[1].siteID,'site-one');
 assert.ok(sent[1].requestID);
 bridge.reply({requestID:sent[1].requestID,error:'Open branch switching from this Site\'s window'});
 assert.equal(await opened,false);
 assert.match(reports.at(-1)[0],/Site's window/);
});
