<template>
  <main>
    <header><h1>AI 助手回复</h1><p>头像与时间 · 共享组件视觉检查</p></header>
    <section>
      <AssistantReplyShell timestamp="10月15日 11:39">
        <p>这段视频介绍了如何把 AI 从问答工具，变成能够执行任务的学习助手。</p>
        <p>先明确目标，再补充背景与约束，最后检查结果是否符合预期。</p>
        <a href="#source">查看视频原文 02:36</a>
      </AssistantReplyShell>
      <AssistantReplyShell timestamp="10月15日 11:40">
        <p class="pending">正在整合视频知识...</p>
      </AssistantReplyShell>
      <AssistantReplyShell timestamp="10月15日 11:42">
        <p>回答生成失败，请稍后重试。</p>
      </AssistantReplyShell>
      <AssistantReplyShell data-case="missing">
        <p>学习不是填满一桶水，而是点燃一把火。</p>
        <p class="long">窄屏长内容检查：knowledge-management-and-training-workflow-with-an-unbroken-reference-identifier</p>
      </AssistantReplyShell>
    </section>
  </main>
</template>
<script setup lang="ts">
import AssistantReplyShell from '../../src/components/videohub/AssistantReplyShell.vue'
</script>
<style>
body { margin: 0; background: var(--td-bg-color-page); color: var(--td-text-color-primary); font-family: -apple-system, 'PingFang SC', sans-serif; }
main { max-width: 720px; margin: 40px auto; padding: 20px; }
h1 { margin: 0; font: var(--td-font-title-large); }
header > p { color: var(--td-text-color-secondary); font: var(--td-font-body-medium); }
section { display: grid; gap: 20px; margin-top: 32px; }
section p { margin: 0 0 10px; font: var(--td-font-body-medium); line-height: 1.7; overflow-wrap: anywhere; }
a { color: var(--td-brand-color); font: var(--td-font-body-small); }
.pending { color: var(--td-text-color-secondary); }
</style>
