import assert from 'node:assert/strict';
import {test} from 'node:test';
import {mkdtemp, readFile, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {ClassroomWorkflow, classroomResult} from '../dist/classroom-workflow.js';

const response = (data, status=200) => new Response(JSON.stringify(data), {status,headers:{'content-type':'application/json'}});
async function fixture(t, {createFails=false, createError='Sensitive upstream details', syncFails=false, pending=[], extraction='idle', legacy=false, capabilityStatus=200, textSource=false, jobResponse}={}) {
  const directory=await mkdtemp(join(tmpdir(),'openmaic-test-'));t.after(()=>rm(directory,{recursive:true,force:true}));
  const journal=join(directory,'jobs.json');const calls=[];const updates=[];
  let claimed=false;
  let done;const published=new Promise(resolve=>{done=resolve});
  const remote=async (path,init={})=>{
    calls.push({path,init});
    if (path==='/api/generate-classroom/capabilities') return response({materials:{formats:[]}},legacy?404:capabilityStatus);
    if (path==='/api/materials') return response({materialId:'material-1',extraction:{status:extraction}});
    if (path==='/api/materials/material-1') return response({material:{extraction:{status:'ready'}}});
    if (path==='/api/generate-classroom') {if(JSON.parse(init.body).requirement==='')return response({errorCode:'INVALID_REQUEST',error:'Invalid pdfContent: expected { text: string; images: string[] }'},400);if(createFails)throw new Error(createError);return response({jobId:'job-1'});}
    if (path==='/api/generate-classroom/job-1') return response(jobResponse ? jobResponse() : {status:'succeeded',result:{classroomId:'class-1',url:'https://open.maic.chat/classroom/class-1'}});
    throw new Error('unexpected remote path');
  };
  const backend=async (url,init={})=>{
    if (String(url).endsWith('/claim')) {
      if(claimed)return response({error:'already_claimed'},409);claimed=true;
      return response({data:{memory_read:true,memory_count:1,requirement:'Use slow paced examples',materials:[{file_name:textSource?'course.txt':'course.pdf',mime_type:textSource?'text/plain':'application/pdf',content_base64:Buffer.from(textSource?'MCP连接模型与工具；Skills封装可复用工作流。':'%PDF-original').toString('base64')}]}});
    }
    if (String(url).endsWith('/pending'))return response({data:pending});
    if (String(url).endsWith('/status')) {updates.push(JSON.parse(init.body));done();if(syncFails)return response({},503);return response({success:true});}
    throw new Error('unexpected backend path');
  };
  const workflow=new ClassroomWorkflow(remote,async()=>{},'http://backend','secret',journal,backend);t.after(()=>workflow.stop());
  return {workflow,journal,calls,updates,published,remote,backend};
}
test('verified URL rejects pending, foreign host, query, and mismatched classroom',()=>{
  assert.equal(classroomResult({classroomId:'real',url:'https://open.maic.chat/classroom/real'}).classroom_id,'real');
  for(const value of [
    {classroomId:'pending',url:'https://open.maic.chat/classroom/pending'},
    {classroomId:'real',url:'https://evil.example/classroom/real'},
    {classroomId:'real',url:'https://open.maic.chat/classroom/real?token=secret'},
    {classroomId:'real',url:'https://open.maic.chat/classroom/other'},
  ])assert.throws(()=>classroomResult(value));
});
test('real progress survives retries and restart; only a verified link publishes 100 percent',async t=>{
  let current={status:'running',progress:75};
  const f=await fixture(t,{jobResponse:()=>current});
  await f.workflow.start('req-progress');await f.published;await f.workflow.reconcile();
  assert.equal(f.updates.at(-1).progress,75);
  const recovered=new ClassroomWorkflow(f.remote,async()=>{},'http://backend','secret',f.journal,f.backend);
  t.after(()=>recovered.stop());
  current={status:'running'};await recovered.reconcile();
  assert.equal(f.updates.at(-1).progress,75);
  for(const progress of [-1,101,1.5,'80',true]) {
    const count=f.updates.length;current={status:'running',progress};await recovered.reconcile();
    assert.equal(f.updates.length,count);
  }
  current={status:'succeeded',progress:100,result:{classroomId:'class-1',url:'https://evil.example/classroom/class-1'}};
  const count=f.updates.length;await recovered.reconcile();assert.equal(f.updates.length,count);
  assert.equal(JSON.parse(await readFile(f.journal,'utf8'))[0].progress,75);
  current={status:'succeeded',result:{classroomId:'class-1',url:'https://open.maic.chat/classroom/class-1'}};
  await recovered.reconcile();assert.equal(f.updates.at(-1).progress,100);
  assert.equal(f.updates.at(-1).generation_status,'succeeded');
});
test('prepared PDF and teaching preferences reach create; completion never guessed',async t=>{
  const f=await fixture(t);const result=await f.workflow.start('req-1');assert.equal(result.memory_read,true);
  await f.published;
  await f.workflow.reconcile();
  assert.equal(Buffer.from(f.calls.find(c=>c.path==='/api/materials').init.body).toString(),'%PDF-original');
  const create=JSON.parse(f.calls.find(c=>c.path==='/api/generate-classroom').init.body);
  assert.equal(f.calls.filter(c=>c.path==='/api/materials/material-1').length,0);
  assert.equal(create.requirement,'Use slow paced examples');assert.deepEqual(create.materialIds,['material-1']);
  assert.equal(f.updates.at(-1).classroom_url,'https://open.maic.chat/classroom/class-1');
  assert.equal(f.updates.at(-1).status,'unknown');assert.equal(f.updates.at(-1).error_code,'');
  const journal=await readFile(f.journal,'utf8');assert.ok(!journal.includes('PDF-original'));assert.ok(!journal.includes('paced examples'));
});
test('ambiguous create fails closed and never repeats POST',async t=>{
  const f=await fixture(t,{createFails:true});await f.workflow.start('req-1');await f.published;await f.workflow.reconcile();
  await assert.rejects(f.workflow.start('req-1'));
  assert.equal(f.calls.filter(c=>c.path==='/api/generate-classroom').length,1);
  assert.equal(f.updates.at(-1).error_code,'generation_outcome_unknown');assert.equal(f.updates.at(-1).generation_status,'failed');
});
test('account rejection before a remote POST keeps its actionable error instead of an ambiguous outcome',async t=>{
  for(const code of ['service_account_login_required','service_account_mismatch','service_account_not_configured','service_account_verification_unavailable']) {
    const f=await fixture(t,{createFails:true,createError:code});
    await f.workflow.start('req-account');await f.published;
    assert.equal(f.updates.at(-1).generation_status,'failed');
    assert.equal(f.updates.at(-1).error_code,code);
  }
});
test('restart recovers persisted remote ID after local publication fails',async t=>{
  const f=await fixture(t,{syncFails:true});await f.workflow.start('req-1');await f.published;
  const persisted=JSON.parse(await readFile(f.journal,'utf8'));assert.equal(persisted[0].job_id,'job-1');
  const updates=[];
  const recovered=new ClassroomWorkflow(f.remote,async()=>{},'http://backend','secret',f.journal,async (url,init)=>String(url).endsWith('/pending')?response({data:[]}):(updates.push(JSON.parse(init.body)),response({success:true})));
  t.after(()=>recovered.stop());await recovered.reconcile();
  assert.equal(updates[0].generation_status,'succeeded');assert.equal(f.calls.filter(c=>c.path==='/api/generate-classroom').length,1);
});
test('restart without a remote ID preserves ambiguous outcome instead of resubmitting',async t=>{
  const f=await fixture(t);await writeFile(f.journal,JSON.stringify([{request_id:'req-1',job_id:'',generation_status:'running',created_at:new Date().toISOString()}]));
  const recovered=new ClassroomWorkflow(f.remote,async()=>{},'http://backend','secret',f.journal,f.backend);t.after(()=>recovered.stop());
  await recovered.run();await f.published;assert.equal(f.updates.at(-1).error_code,'generation_outcome_unknown');assert.equal(f.calls.length,0);
});
test('expired generation terminates even when remote queries would fail',async t=>{
  const f=await fixture(t,{pending:[{request_id:'req-old',job_id:'job-old',generation_status:'running',created_at:new Date(Date.now()-61*60_000).toISOString()}]});
  await f.workflow.reconcile();
  assert.equal(f.calls.length,1);
  assert.equal(f.updates.at(-1).generation_status,'failed');
  assert.equal(f.updates.at(-1).error_code,'generation_timeout_unverified');
});

test('failed material extraction prevents course creation with a safe error',async t=>{
  const f=await fixture(t,{extraction:'failed'});await f.workflow.start('req-1');await f.published;
  assert.equal(f.calls.filter(c=>c.path==='/api/generate-classroom').length,0);
  assert.equal(f.updates.at(-1).error_code,'material_extraction_failed');
});
test('unknown upload extraction state fails closed',async t=>{
  const f=await fixture(t,{extraction:'unexpected'});await f.workflow.start('req-1');await f.published;
  assert.equal(f.calls.filter(c=>c.path==='/api/generate-classroom').length,0);
  assert.equal(f.updates.at(-1).error_code,'invalid_material_response');
});

test('observed hosted alias becomes the exact official HTTPS classroom URL',()=>{
  for(const origin of ['http://live-ack-prod.cogevol.com','https://live-ack-prod.cogevol.com']) {
    assert.equal(classroomResult({classroomId:'real',url:origin+'/classroom/real'}).classroom_url,'https://open.maic.chat/classroom/real');
  }
  for(const url of ['http://open.maic.chat/classroom/real','http://live-ack-prod.cogevol.com.evil.example/classroom/real','http://live-ack-prod.cogevol.com:8080/classroom/real','http://user@live-ack-prod.cogevol.com/classroom/real','http://live-ack-prod.cogevol.com/classroom/other','http://live-ack-prod.cogevol.com/classroom/real?x=1','http://live-ack-prod.cogevol.com/classroom/real#x']) assert.throws(()=>classroomResult({classroomId:'real',url}));
});

test('expired previously timed out job recovers a verified remote success without POST',async t=>{
 const f=await fixture(t,{pending:[{request_id:'req-expired',job_id:'job-1',generation_status:'failed',created_at:new Date(Date.now()-2*60*60_000).toISOString()}]});
 await f.workflow.reconcile();assert.equal(f.updates.at(-1).generation_status,'succeeded');assert.equal(f.updates.at(-1).classroom_url,'https://open.maic.chat/classroom/class-1');assert.equal(f.calls.length,1);
});

 test('legacy hosted protocol transmits actual video text without unused uploads',async t=>{
  const f=await fixture(t,{legacy:true,textSource:true});await f.workflow.start('req-video');await f.published;
  const creates=f.calls.filter(c=>c.path==='/api/generate-classroom' && JSON.parse(c.init.body).requirement);assert.equal(creates.length,1);
  const input=JSON.parse(creates[0].init.body);assert.equal(input.pdfContent.text,'MCP连接模型与工具；Skills封装可复用工作流。');
  assert.deepEqual(input.pdfContent.images,[]);assert.equal(input.materialIds,undefined);
  assert.equal(f.calls.filter(c=>c.path==='/api/materials').length,0);
  assert.ok(!(await readFile(f.journal,'utf8')).includes('MCP连接'));
});
test('legacy PDF refuses generation instead of ignoring source',async t=>{
  const f=await fixture(t,{legacy:true});await f.workflow.start('req-pdf');await f.published;
  assert.equal(f.updates.at(-1).error_code,'legacy_material_unsupported');
  assert.equal(f.calls.filter(c=>c.path==='/api/generate-classroom' && JSON.parse(c.init.body).requirement).length,0);
});
test('capability access failure never downgrades or creates a course',async t=>{
  const f=await fixture(t,{capabilityStatus:403,textSource:true});await f.workflow.start('req-denied');await f.published;
  assert.equal(f.updates.at(-1).error_code,'remote_access_denied');
  assert.equal(f.calls.filter(c=>c.path==='/api/generate-classroom').length,0);
});
