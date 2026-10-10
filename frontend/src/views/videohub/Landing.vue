<template>
  <iframe ref="landingFrame" class="landing-page" title="Vidsage 产品首页" :srcdoc="landingDocument" @load="bindLoginActions" />
  <t-dialog :visible="route.path === '/login'" :header="false" :footer="false"
    width="min(480px, calc(100vw - 32px))" :close-on-overlay-click="false"
    @close="closeLogin">
    <Login v-if="route.path === '/login'" embedded />
  </t-dialog>
</template>

<script setup lang="ts">
import landingHtml from '@/assets/vidsage-landing.html?raw'
import themeCss from '@/assets/theme/theme.css?raw'
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Login from '@/views/auth/Login.vue'

const route = useRoute()
const router = useRouter()
const landingFrame = ref<HTMLIFrameElement>()

function closeLogin() {
  router.replace('/')
}

function bindLoginActions() {
  // Capture the reviewed document's buttons before its standalone navigation handler.
  landingFrame.value?.contentDocument?.addEventListener('click', (event) => {
    const target = event.target as Element | null
    if (!target?.closest('[data-action]')) return
    event.preventDefault()
    event.stopImmediatePropagation()
    router.push('/login')
  }, true)
}

// Keep the reviewed design document as the single source for the public homepage.
const landingDocument = landingHtml.replace(
  '<link rel="stylesheet" href="../../vidsage/frontend/src/assets/theme/theme.css">',
  `<style>${themeCss}</style>`,
)
</script>

<style scoped>
.landing-page {
  display: block;
  width: 100%;
  height: 100dvh;
  border: 0;
  background: var(--td-bg-color-container);
}
</style>
