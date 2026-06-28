<template>
  <main class="account-login">
    <section class="login-toolbar">
      <div class="brand-block">
        <span class="eyebrow">Dedao Auth</span>
        <h2>扫码登录</h2>
      </div>

      <label>
        <span>Base URL</span>
        <input v-model="baseUrl" name="loginBaseUrl" placeholder="http://127.0.0.1:8719" />
      </label>

      <label>
        <span>Token</span>
        <input v-model="token" name="loginToken" type="password" placeholder="KBASE_AUTH_TOKEN" />
      </label>

      <button class="primary-action" type="button" :disabled="loading" @click="generateQRCode">
        {{ loading ? '生成中' : '生成二维码' }}
      </button>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="login-grid">
      <article class="login-card qr-card">
        <div class="qr-frame">
          <img v-if="qrPayload?.qr_code" :src="qrPayload.qr_code" alt="" />
          <span v-else>QR</span>
        </div>
        <div class="login-actions">
          <button type="button" :disabled="!qrPayload || polling" @click="startPolling">
            {{ polling ? '轮询中' : '开始轮询' }}
          </button>
          <button type="button" :disabled="!qrPayload || loading" @click="pollOnce">检查状态</button>
        </div>
      </article>

      <article class="login-card">
        <span class="eyebrow">Login State</span>
        <dl class="login-metrics">
          <div>
            <dt>status</dt>
            <dd>{{ statusLabel }}</dd>
          </div>
          <div>
            <dt>token</dt>
            <dd>{{ token ? 'present' : 'missing' }}</dd>
          </div>
          <div>
            <dt>qr_code_string</dt>
            <dd>{{ qrPayload?.qr_code_string ? 'present' : 'missing' }}</dd>
          </div>
        </dl>
      </article>

      <article class="login-card login-result">
        <span class="eyebrow">Result</span>
        <h2>{{ resultTitle }}</h2>
        <p>{{ resultMeta }}</p>
        <router-link v-if="loginResult?.status === 1" to="/user/profile">个人中心</router-link>
      </article>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getBrowserSession, KBaseClient, type DedaoLoginCheck, type DedaoLoginQRCode } from '../api'

const storageKey = 'dedao-kbase-web-settings'

const baseUrl = ref(window.location.origin)
const token = ref('')
const qrPayload = ref<DedaoLoginQRCode | null>(null)
const loginResult = ref<DedaoLoginCheck | null>(null)
const loading = ref(false)
const polling = ref(false)
const errorMessage = ref('')
let pollTimer: number | undefined

const client = computed(() => new KBaseClient(baseUrl.value, token.value))

const statusLabel = computed(() => {
  if (loginResult.value?.status === 1) {
    return 'success'
  }
  if (loginResult.value?.expired || loginResult.value?.status === 2) {
    return 'expired'
  }
  if (polling.value) {
    return 'pending'
  }
  return qrPayload.value ? 'ready' : 'idle'
})

const resultTitle = computed(() => {
  if (loginResult.value?.status === 1) {
    return loginResult.value.user?.name || '登录成功'
  }
  if (loginResult.value?.expired || loginResult.value?.status === 2) {
    return '二维码已过期'
  }
  return qrPayload.value ? '等待扫码' : '未生成二维码'
})

const resultMeta = computed(() => {
  if (loginResult.value?.status === 1) {
    return loginResult.value.user?.uid_hazy || loginResult.value.session.active_user?.uid_hazy || 'session updated'
  }
  if (loginResult.value?.expired || loginResult.value?.status === 2) {
    return 'expired'
  }
  return qrPayload.value?.token ? 'login-token-ready' : 'no-login-token'
})

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await generateQRCode()
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
})

onBeforeUnmount(() => {
  stopPolling()
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

const generateQRCode = async () => {
  loading.value = true
  errorMessage.value = ''
  loginResult.value = null
  stopPolling()
  try {
    await hydrateBrowserSession()
    saveConnection()
    qrPayload.value = await client.value.createDedaoLoginQRCode()
    startPolling()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const startPolling = () => {
  if (!qrPayload.value || polling.value) {
    return
  }
  polling.value = true
  pollTimer = window.setInterval(() => {
    void pollOnce()
  }, 2000)
}

const stopPolling = () => {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
  polling.value = false
}

const pollOnce = async () => {
  if (!qrPayload.value) {
    return
  }
  errorMessage.value = ''
  try {
    const result = await client.value.checkDedaoLogin(qrPayload.value.token, qrPayload.value.qr_code_string)
    loginResult.value = result
    if (result.status === 1 || result.expired || result.status === 2) {
      stopPolling()
    }
  } catch (error) {
    stopPolling()
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
}
</script>
