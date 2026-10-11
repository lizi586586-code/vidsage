import test from 'node:test';
import assert from 'node:assert/strict';
import {loadedTeachingSkills, narrationReferences} from '../dist/teaching-evidence.js';

const read = {type:'message_end',data:{message:{role:'toolResult',toolName:'read',isError:false,details:{skill:'feynman-learning',path:'/app/skills/agent-runtime/feynman-learning/SKILL.md',offset:1,lines:153,totalLines:153,sourceHash:'af7d9eaca12896a4'}}}};
test('only a successful complete durable skill read proves loading', () => {
  assert.equal(loadedTeachingSkills([read,read],['feynman-learning']).length,1);
  for (const mutate of [x=>x.type='message_start',x=>x.data.message.isError=true,x=>x.data.message.details.lines=10,x=>{x.data.message.details.lines=-1;x.data.message.details.totalLines=-1},x=>x.data.message.details.offset=2,x=>x.data.message.details.sourceHash='',x=>x.data.message.role='assistant']) {
    const event=structuredClone(read);mutate(event);
    assert.throws(()=>loadedTeachingSkills([event],['feynman-learning']),/skill_loading_unverified/);
  }
  assert.throws(()=>loadedTeachingSkills([read],['unknown']),/invalid_teaching_plan/);
});
const doc = {stage:{id:'course-1'},scenes:[{stageId:'course-1',actions:[{type:'speech',text:'中文讲解',audioUrl:'/api/classroom-media/course-1/tts-1.mp3'}]}]};
test('all teaching scenes require narration and same-course audio references',()=>{
  assert.deepEqual(narrationReferences(doc,'course-1'),['/api/classroom-media/course-1/tts-1.mp3']);
  const missing=structuredClone(doc);delete missing.scenes[0].actions[0].audioUrl;
  assert.throws(()=>narrationReferences(missing,'course-1'),/narration_incomplete/);
  for(const url of ['https://[','https://evil.example/audio.mp3','/api/classroom-media/other/tts.mp3','/api/classroom-media/course-1/a.mp3?token=secret']) {
    const foreign=structuredClone(doc);foreign.scenes[0].actions[0].audioUrl=url;
    assert.throws(()=>narrationReferences(foreign,'course-1'),/invalid_audio_reference/);
  }
  assert.throws(()=>narrationReferences(doc,'other'),/invalid_generation_result/);
  for (const bad of [null, 3, [], 'bad']) {
    assert.throws(()=>narrationReferences({stage:{id:'course-1'},scenes:[bad]},'course-1'),/invalid_generation_result/);
    assert.throws(()=>narrationReferences({stage:{id:'course-1'},scenes:[{stageId:'course-1',actions:[bad]}]},'course-1'),/invalid_generation_result/);
  }
  const invalidSpeech=structuredClone(doc);invalidSpeech.scenes[0].actions.push({type:'speech',text:17});
  assert.throws(()=>narrationReferences(invalidSpeech,'course-1'),/invalid_generation_result/);
});
