<template>
  <main class="invite-landing">
    <div class="invite-orb invite-orb--top" aria-hidden="true" />
    <div class="invite-orb invite-orb--bottom" aria-hidden="true" />

    <header class="invite-header">
      <div class="invite-brand">
        <div class="invite-brand__mark"><t-icon name="star" /></div>
        <div>
          <strong>{{ pageTitle }}</strong>
          <span>专属 AI 智能交互课堂</span>
        </div>
      </div>
      <div class="invite-header__actions">
        <t-button theme="default" variant="outline" class="invite-header__button" @click="openLogin">
          加入学习空间
        </t-button>
        <t-button theme="primary" variant="text" tag="a" href="http://127.0.0.1/" class="invite-header__about">
          了解Vidsage
        </t-button>
      </div>
    </header>

    <section v-if="loading" class="invite-state" aria-live="polite">
      <t-loading size="28px" />
      <span>正在加载学习空间…</span>
    </section>

    <section v-else-if="error" class="invite-state invite-state--error">
      <t-icon name="link" />
      <h1>邀请链接无效或已失效</h1>
      <p>{{ error }}</p>
      <t-button theme="primary" @click="router.replace('/login')">前往登录</t-button>
    </section>

    <template v-else>
      <section class="invite-hero">
        <div class="invite-badge"><t-icon name="user-checked" /> {{ ownerDisplay }}邀请你加入学习空间</div>
        <h1>{{ lookup?.tenant_name || '学习空间' }}</h1>
        <p v-if="description">{{ description }}</p>
        <div class="invite-actions">
          <t-button theme="primary" size="large" class="invite-primary" @click="openLogin">
            加入学习空间 <t-icon name="arrow-right" />
          </t-button>
          <span class="invite-note"><t-icon name="secured" /> 登录即注册，加入后即可开始学习</span>
        </div>
      </section>

      <section class="invite-features" aria-label="学习空间能力">
        <div class="invite-features__heading">
          <h2>AI 学习空间核心能力</h2>
          <p>全方位赋能课后复习、答疑与知识构建</p>
        </div>
        <article v-for="feature in features" :key="feature.title" class="invite-card">
          <div class="invite-card__icon"><t-icon :name="feature.icon" /></div>
          <h2>{{ feature.title }}</h2>
          <p>{{ feature.description }}</p>
        </article>
      </section>

      <section class="invite-preview invite-card">
        <div>
          <span class="invite-badge">沉浸式学习体验</span>
          <h2>随时随地，与 AI 导师同行</h2>
          <p>进入空间后，你不仅可以与 AI 助教随时对话答疑，还能够体验原视频片段定位、互动测验、知识图谱漫游等功能。</p>
          <t-button theme="primary" size="large" @click="openLogin">立即进入学习空间 <t-icon name="arrow-right" /></t-button>
        </div>
        <img :src="dashboardPreview" alt="学习空间界面预览" />
      </section>
    </template>

    <footer class="invite-footer">© {{ new Date().getFullYear() }} {{ pageTitle }} · AI 智能赋能教育</footer>

    <t-dialog v-model:visible="loginVisible" header="登录并加入学习空间" :footer="false"
      width="min(480px, calc(100vw - 32px))" :close-on-overlay-click="false">
      <Login v-if="loginVisible" embedded invite-login-first />
    </t-dialog>
  </main>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import Login from './Login.vue'
import { getInvitationByToken, type InviteLookup } from '@/api/auth'
import dashboardPreview from '@/assets/img/invite-learning-preview.jpg'

const route = useRoute()
const router = useRouter()
const lookup = ref<InviteLookup | null>(null)
const loading = ref(true)
const error = ref('')
const loginVisible = ref(false)
const token = computed(() => String(route.query.token || '').trim())

const ownerDisplay = computed(() => lookup.value?.owner_username ? `${lookup.value.owner_username}老师` : '老师')
const pageTitle = computed(() => `${ownerDisplay.value}的学习空间`)
const description = computed(() => lookup.value?.tenant_description || '')
const features = [
  { icon: 'help-circle', title: '遇到疑问，及时解答', description: '结合课程回答你的问题，并精准定位到原视频相应时间戳，方便随时回看老师的详细讲解。' },
  { icon: 'compass', title: '明确方向，掌握重点', description: '智能制定个性化学习计划，系统整理复习核心重点，让你清晰知道接下来学什么、怎么高效复习。' },
  { icon: 'search', title: '查找内容，快速定位', description: '通过章节与主题智能导航，一键找到你需要的知识点和对应课程精彩片段，告别大海捞针。' },
  { icon: 'book-open', title: '边学边练，巩固理解', description: '结合老师的教学方法与考点，提供定制化互动练习与复习材料，随时检验理解并巩固所学。' },
  { icon: 'share', title: '串联知识，融会贯通', description: '通过直观的知识图谱，清晰看清不同课程与模块之间的知识关系，把零散的学习内容串成完整的知识体系。' },
]

function openLogin() {
  if (!lookup.value) {
    MessagePlugin.warning(error.value || '邀请链接正在加载，请稍候')
    return
  }
  loginVisible.value = true
}

let lookupVersion = 0
watch(token, async (currentToken) => {
  const version = ++lookupVersion
  loginVisible.value = false
  lookup.value = null
  error.value = ''
  loading.value = true
  if (!currentToken) {
    error.value = '请使用完整的邀请链接打开学习空间。'
    loading.value = false
    return
  }
  try {
    const response = await getInvitationByToken(currentToken)
    if (version !== lookupVersion) return
    if (!response.success || !response.data) {
      error.value = response.message || '请联系邀请人重新发送链接。'
      return
    }
    lookup.value = response.data
  } catch {
    if (version !== lookupVersion) return
    error.value = '请联系邀请人重新发送链接。'
  } finally {
    if (version === lookupVersion) loading.value = false
  }
}, { immediate: true })
</script>

<style scoped lang="less">
.invite-landing {
  min-height: 100dvh;
  position: relative;
  overflow: hidden;
  color: var(--td-text-color-primary);
  background: var(--td-bg-color-container);
  padding: 0 6vw;
}

.invite-orb { position: absolute; border-radius: 50%; pointer-events: none; filter: blur(90px); opacity: .48; }
.invite-orb--top { width: 420px; height: 420px; top: -180px; left: -110px; background: var(--td-brand-color-light); }
.invite-orb--bottom { width: 520px; height: 520px; right: -190px; bottom: -230px; background: var(--td-success-color-light); }

.invite-header, .invite-hero, .invite-features, .invite-footer { position: relative; z-index: 1; max-width: 1180px; margin: 0 auto; }
.invite-header { display: flex; align-items: center; justify-content: space-between; padding: 28px 0; }
.invite-header__actions { display: flex; align-items: center; gap: 8px; }
.invite-brand { display: flex; align-items: center; gap: 12px; }
.invite-brand__mark { display: grid; place-items: center; width: 42px; height: 42px; border-radius: var(--td-radius-medium); color: var(--td-text-color-anti); background: var(--td-brand-color); }
.invite-brand strong, .invite-brand span { display: block; }
.invite-brand strong { font-size: 18px; letter-spacing: -.02em; }
.invite-brand span { margin-top: 3px; color: var(--td-text-color-secondary); font-size: 12px; }
.invite-header__button { border-radius: var(--td-radius-medium); }
.invite-hero { padding: 72px 20px 92px; text-align: center; }
.invite-badge, .invite-note { display: inline-flex; align-items: center; gap: 7px; color: var(--td-brand-color); font-size: 13px; }
.invite-badge { padding: 8px 14px; border: 1px solid var(--td-brand-color-light); border-radius: var(--td-radius-round); background: color-mix(in srgb, var(--td-brand-color-light) 35%, transparent); }
.invite-hero h1 { max-width: 820px; margin: 22px auto 18px; font-size: clamp(34px, 5vw, 62px); line-height: 1.12; letter-spacing: -.04em; }
.invite-hero p { max-width: 680px; margin: 0 auto; color: var(--td-text-color-secondary); font-size: 16px; line-height: 1.85; }
.invite-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 18px; margin-top: 34px; }
.invite-primary { border-radius: var(--td-radius-medium); box-shadow: 0 8px 24px color-mix(in srgb, var(--td-brand-color) 20%, transparent); }
.invite-features { display: grid; grid-template-columns: repeat(3, 1fr); gap: 18px; padding-bottom: 72px; }
.invite-features__heading { grid-column: 1 / -1; text-align: center; margin-bottom: 18px; }
.invite-features__heading h2 { margin: 0 0 8px; font-size: 24px; }
.invite-features__heading p { margin: 0; font-size: 14px; color: var(--td-text-color-secondary); }
.invite-features .invite-card:last-child { grid-column: span 2; }
.invite-preview { position: relative; z-index: 1; max-width: 1180px; margin: 0 auto 64px; display: grid; grid-template-columns: 1fr 1fr; align-items: center; gap: 36px; }
.invite-preview h2 { font-size: 28px; margin: 20px 0 16px; }
.invite-preview p { margin-bottom: 24px; }
.invite-preview img { width: 100%; border-radius: var(--td-radius-large); }
.invite-card { padding: 28px; border: 1px solid var(--td-border-level-1-color); border-radius: var(--td-radius-large); background: color-mix(in srgb, var(--td-bg-color-container) 72%, transparent); backdrop-filter: blur(24px) saturate(112%); }
.invite-card__icon { display: grid; place-items: center; width: 44px; height: 44px; margin-bottom: 18px; border-radius: var(--td-radius-medium); color: var(--td-brand-color); background: var(--td-brand-color-light); }
.invite-card h2 { margin: 0 0 8px; font-size: 17px; }
.invite-card p { margin: 0; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.7; }
.invite-footer { padding: 24px 0 30px; color: var(--td-text-color-placeholder); border-top: 1px solid var(--td-border-level-1-color); font-size: 12px; }
.invite-state { position: relative; z-index: 1; display: grid; place-items: center; gap: 14px; min-height: 65vh; color: var(--td-text-color-secondary); text-align: center; }
.invite-state h1 { margin: 0; color: var(--td-text-color-primary); font-size: 26px; }
.invite-state p { margin: 0; }
.invite-state--error > .t-icon { color: var(--td-error-color); font-size: 30px; }
@media (max-width: 720px) { .invite-landing { padding: 0 20px; } .invite-header__actions { display: none; } .invite-hero { padding: 54px 0 64px; } .invite-features { grid-template-columns: 1fr; padding-bottom: 48px; } .invite-features .invite-card:last-child { grid-column: auto; } .invite-preview { grid-template-columns: 1fr; } }
</style>
