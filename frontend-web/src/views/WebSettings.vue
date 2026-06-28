<template>
  <main class="web-settings">
    <section class="settings-header">
      <div class="brand-block">
        <span class="eyebrow">Web Settings</span>
        <h2>连接设置</h2>
      </div>
      <span class="status-pill" :class="{ ok: Boolean(settingsToken) }">
        {{ settingsToken ? 'Token present' : 'Token missing' }}
      </span>
    </section>

    <section v-if="message" class="settings-message" :class="{ error: hasError }">{{ message }}</section>

    <section class="settings-grid">
      <article class="settings-card connection-card">
        <div class="panel-head">
          <div>
            <span class="eyebrow">KBase API</span>
            <h2>服务连接</h2>
          </div>
        </div>

        <label>
          <span>Base URL</span>
          <input v-model="settingsBaseUrl" name="settingsBaseUrl" placeholder="https://kbase.executor.life" />
        </label>

        <label>
          <span>Token</span>
          <input v-model="settingsToken" name="settingsToken" type="password" placeholder="KBASE_AUTH_TOKEN" />
        </label>

        <div class="settings-actions">
          <button type="button" class="secondary-action" :disabled="loading" @click="hydrateFromBrowserSession">
            {{ loading ? '读取中' : '从登录态填充' }}
          </button>
          <button type="button" class="primary-action" @click="saveSettings">保存设置</button>
        </div>
      </article>

      <article class="settings-card">
        <span class="eyebrow">Scope</span>
        <h2>生效范围</h2>
        <p>课程、电子书、书籍知识库、扫码登录和个人中心都读取同一份 Web 连接设置。</p>
        <dl>
          <div>
            <dt>Storage</dt>
            <dd>dedao-kbase-web-settings</dd>
          </div>
          <div>
            <dt>Current Base</dt>
            <dd>{{ normalizedBaseUrl }}</dd>
          </div>
          <div>
            <dt>Token</dt>
            <dd>{{ settingsToken ? 'present' : 'missing' }}</dd>
          </div>
        </dl>
      </article>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getBrowserSession } from '../api'

const storageKey = 'dedao-kbase-web-settings'

const settingsBaseUrl = ref(window.location.origin)
const settingsToken = ref('')
const loading = ref(false)
const message = ref('')
const hasError = ref(false)

const normalizedBaseUrl = computed(() => (settingsBaseUrl.value || window.location.origin).replace(/\/+$/, ''))

onMounted(() => {
  restoreSettings()
})

const restoreSettings = () => {
  const raw = localStorage.getItem(storageKey)
  if (!raw) {
    return
  }
  try {
    const parsed = JSON.parse(raw) as { baseUrl?: string; token?: string }
    settingsBaseUrl.value = parsed.baseUrl || settingsBaseUrl.value
    settingsToken.value = parsed.token || ''
  } catch {
    localStorage.removeItem(storageKey)
  }
}

const saveSettings = () => {
  localStorage.setItem(
    storageKey,
    JSON.stringify({
      baseUrl: normalizedBaseUrl.value,
      token: settingsToken.value.trim(),
    }),
  )
  hasError.value = false
  message.value = '设置已保存。'
}

const hydrateFromBrowserSession = async () => {
  loading.value = true
  message.value = ''
  hasError.value = false
  try {
    const browserSession = await getBrowserSession()
    if (!browserSession?.token) {
      hasError.value = true
      message.value = '当前浏览器会话没有可用 Token，请先完成登录。'
      return
    }
    settingsBaseUrl.value = window.location.origin
    settingsToken.value = browserSession.token
    saveSettings()
  } catch (error) {
    hasError.value = true
    message.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.web-settings {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: calc(100vh - 156px);
  margin-top: 12px;
}

.settings-header,
.settings-card {
  border: 1px solid #d3dce8;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 10px 24px rgb(37 51 74 / 8%);
}

.settings-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
}

.settings-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(320px, 0.8fr);
  gap: 12px;
}

.settings-card {
  min-width: 0;
  padding: 14px;
}

.settings-card label {
  display: grid;
  gap: 5px;
  margin-top: 12px;
}

.settings-card label span,
.settings-card dt {
  color: #607086;
  font-size: 10px;
  font-weight: 800;
  text-transform: uppercase;
}

.settings-card input {
  min-height: 38px;
  border: 1px solid #c8d2df;
  border-radius: 7px;
  padding: 0 10px;
  color: #172033;
  font-size: 14px;
}

.settings-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}

.settings-card p {
  margin: 8px 0 0;
  color: #536274;
  font-size: 13px;
}

.settings-card dl {
  display: grid;
  gap: 8px;
  margin: 14px 0 0;
}

.settings-card dl div {
  border: 1px solid #dbe2ec;
  border-radius: 7px;
  padding: 9px;
  background: #fbfcfe;
}

.settings-card dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  color: #121926;
  font-weight: 700;
}

.settings-message {
  border: 1px solid #b8d7ca;
  border-radius: 8px;
  padding: 10px 12px;
  background: #effaf4;
  color: #397367;
}

.settings-message.error {
  border-color: #e5b8ac;
  background: #fff4f1;
  color: #9b3f2e;
}

.secondary-action {
  min-height: 36px;
  border: 1px solid #c8d2df;
  border-radius: 7px;
  padding: 0 12px;
  background: #fbfcfe;
  color: #263244;
  font-weight: 700;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.62;
}

@media (max-width: 900px) {
  .settings-header,
  .settings-grid {
    grid-template-columns: 1fr;
  }

  .settings-header {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
