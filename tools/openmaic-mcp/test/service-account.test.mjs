import test from 'node:test';
import assert from 'node:assert/strict';
import {AccountBoundRemote} from '../dist/service-account.js';

const response = (account, status=200) => new Response(JSON.stringify(account), {status,headers:{'content-type':'application/json'}});
test('every generation and upload uses the pinned service identity', async () => {
  const calls=[];
  const remote=new AccountBoundRemote(async (path,init)=>{
    calls.push({path,init});
    return path==='/api/credits'?response({success:true,subjectKey:'user:service-owner'}):response({success:true});
  },async ()=>'user:service-owner');
  for(const path of ['/api/materials','/api/materials/id','/api/generate-classroom','/api/generate-classroom/id','/api/generate-classroom/capabilities']) {
    await remote.fetch(path,{method:'POST',body:'course'});
    assert.deepEqual(calls.slice(-2).map(call=>call.path),['/api/credits',path]);
    assert.equal(calls.at(-1).init.body,'course');
  }
});
test('missing pin, anonymous, expired, wrong account and unavailable verification never submit a course', async () => {
  for(const scenario of [
    {expected:'',account:{subjectKey:'user:owner'},code:'service_account_not_configured'},
    {account:{subjectKey:'anonymous:guest'},code:'service_account_login_required'},
    {account:{success:false,subjectKey:'user:owner'},code:'service_account_login_required'},
    {status:401,account:{},code:'service_account_login_required'},
    {account:{subjectKey:'user:other'},code:'service_account_mismatch'},
    {status:503,account:{},code:'service_account_verification_unavailable'},
    {throws:true,code:'service_account_verification_unavailable'},
    {account:null,code:'service_account_verification_unavailable'},
  ]) {
    const calls=[];
    const remote=new AccountBoundRemote(async path=>{
      calls.push(path);
      if(scenario.throws) throw Error('secret upstream body');
      return response(scenario.account,scenario.status);
    },async ()=>scenario.expected??'user:owner');
    await assert.rejects(remote.fetch('/api/generate-classroom',{method:'POST'}),{message:scenario.code});
    assert.ok(!calls.includes('/api/generate-classroom'));
  }
});
test('account expiration after an upload blocks the generation request',async () => {
  let account={subjectKey:'user:owner'};
  const calls=[];
  const remote=new AccountBoundRemote(async path=>{calls.push(path);return response(path==='/api/credits'?account:{success:true});},async ()=>'user:owner');
  await remote.fetch('/api/materials',{method:'POST'});
  account={subjectKey:'anonymous:guest'};
  await assert.rejects(remote.fetch('/api/generate-classroom',{method:'POST'}),{message:'service_account_login_required'});
  assert.equal(calls.filter(path=>path==='/api/generate-classroom').length,0);
});
test('health remains available when service login needs repair',async () => {
  const calls=[];
  const remote=new AccountBoundRemote(async path=>{calls.push(path);return response({success:true});},async ()=>{throw Error('pin absent');});
  await remote.fetch('/api/health');assert.deepEqual(calls,['/api/health']);
});
