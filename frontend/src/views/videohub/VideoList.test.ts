import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./VideoList.vue', import.meta.url), 'utf8')
const menuSource = readFileSync(new URL('../../components/menu.vue', import.meta.url), 'utf8')
const uploadSource = readFileSync(new URL('../../components/videohub/UploadModal.vue', import.meta.url), 'utf8')

test('course library uses Course branding and course actions', () => {
  assert.match(source, /<h1>Course<\/h1>/)
  assert.doesNotMatch(source, /Explore the knowledge within your videos\./)
  assert.match(source, /placeholder="搜索课程"/)
  assert.ok(source.includes('上传课程'))
  assert.match(source, /没有匹配的课程/)
  assert.match(source, /还没有课程，上传第一门课程吧/)
})

test('conversation navigation appears above the course library entry', () => {
  assert.ok(menuSource.includes("const TOP_MENU_ORDER = ['ai-chat', 'home'"))
})

test('course upload dialog uses course terminology', () => {
  assert.match(uploadSource, /header="上传课程"/)
  assert.match(uploadSource, /选择课程视频/)
  assert.match(uploadSource, /课程已加入课程库/)
})
