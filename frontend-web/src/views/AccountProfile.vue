<template>
  <main class="account-profile">
    <section class="account-toolbar">
      <div class="brand-block">
        <span class="eyebrow">Personal Center</span>
        <h2>得到账户状态</h2>
      </div>

      <label>
        <span>Base URL</span>
        <input v-model="baseUrl" name="accountBaseUrl" placeholder="http://127.0.0.1:8719" />
      </label>

      <label>
        <span>Token</span>
        <input v-model="token" name="accountToken" type="password" placeholder="KBASE_AUTH_TOKEN" />
      </label>

      <button class="primary-action" type="button" :disabled="loading" @click="refreshSession">
        {{ loading ? '刷新中' : '刷新' }}
      </button>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="account-grid">
      <article class="account-card account-identity">
        <div class="account-avatar" :class="{ empty: !avatarUrl }">
          <img v-if="avatarUrl" :src="avatarUrl" alt="" />
          <span v-else>{{ avatarInitial }}</span>
        </div>
        <div>
          <span class="status-pill" :class="{ ok: session?.logged_in }">{{ statusLabel }}</span>
          <h2>{{ displayName }}</h2>
          <p>{{ uidLabel }}</p>
        </div>
      </article>

      <article class="account-card">
        <span class="eyebrow">Server Session</span>
        <dl class="account-metrics">
          <div>
            <dt>logged_in</dt>
            <dd>{{ session?.logged_in ? 'true' : 'false' }}</dd>
          </div>
          <div>
            <dt>user_count</dt>
            <dd>{{ session?.user_count ?? 0 }}</dd>
          </div>
          <div>
            <dt>token</dt>
            <dd>{{ token ? 'present' : 'missing' }}</dd>
          </div>
        </dl>
      </article>

      <article class="account-card account-note">
        <span class="eyebrow">Safety Boundary</span>
        <p>
          Web 端只读取服务端会话摘要，不展示 Cookie、TokenPlan 凭据或本地配置文件内容。登录、扫码和账号切换会在后续切片单独加闸实现。
        </p>
      </article>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getBrowserSession, KBaseClient, type DedaoSession } from '../api'

const storageKey = 'dedao-kbase-web-settings'

const baseUrl = ref(window.location.origin)
const token = ref('')
const session = ref<DedaoSession | null>(null)
const loading = ref(false)
const errorMessage = ref('')

const client = computed(() => new KBaseClient(baseUrl.value, token.value))

const displayName = computed(() => session.value?.active_user?.name || '未登录')
const avatarUrl = computed(() => session.value?.active_user?.avatar || '')
const uidLabel = computed(() => session.value?.active_user?.uid_hazy || 'No active UID')
const statusLabel = computed(() => (session.value?.logged_in ? '已登录' : '未登录'))
const avatarInitial = computed(() => displayName.value.slice(0, 1).toUpperCase() || '?')

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await loadSession()
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
})

const restoreConnection = () => {
  const raw = localStorage.getItem(storageKey)
  if (!raw) {
    return
  }
  try {
    const parsed = JSON.parse(raw) as { baseUrl?: string; token?: string }
    baseUrl.value = parsed.baseUrl || baseUrl.value
    token.value = parsed.token || ''
  } catch {
    localStorage.removeItem(storageKey)
  }
}

const saveConnection = () => {
  localStorage.setItem(storageKey, JSON.stringify({ baseUrl: baseUrl.value, token: token.value }))
}

const hydrateBrowserSession = async () => {
  const browserSession = await getBrowserSession()
  if (browserSession?.token) {
    token.value = browserSession.token
    baseUrl.value = window.location.origin
    saveConnection()
  }
}

const loadSession = async () => {
  if (!token.value) {
    session.value = { logged_in: false, user_count: 0 }
    return
  }
  session.value = await client.value.getDedaoSession()
}

const refreshSession = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    await hydrateBrowserSession()
    saveConnection()
    await loadSession()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}
</script>
