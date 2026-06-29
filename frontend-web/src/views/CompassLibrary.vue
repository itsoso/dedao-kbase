<template>
  <main class="compass-library">
    <section class="compass-toolbar">
      <div class="toolbar-summary">
        <strong>锦囊</strong>
        <span>{{ total }} 个 · 第 {{ page }} / {{ totalPages || 1 }} 页</span>
      </div>
      <button class="primary-action" type="button" :disabled="loading" @click="reloadFromFirstPage">
        {{ loading ? '加载中' : '刷新' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="compass-workspace">
      <aside class="compass-filter-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Search</span>
            <h2>筛选锦囊</h2>
          </div>
        </div>
        <div class="filter-stack">
          <input v-model="query" placeholder="输入问题、主题或答复人" @keydown.enter="reloadFromFirstPage" />
          <select v-model.number="pageSize" @change="reloadFromFirstPage">
            <option :value="10">10/page</option>
            <option :value="15">15/page</option>
            <option :value="30">30/page</option>
          </select>
          <button type="button" class="primary-action" :disabled="loading" @click="reloadFromFirstPage">Search</button>
        </div>
        <dl class="stat-grid">
          <div>
            <dt>Total</dt>
            <dd>{{ total || compasses.length }}</dd>
          </div>
          <div>
            <dt>Loaded</dt>
            <dd>{{ compasses.length }}</dd>
          </div>
        </dl>
      </aside>

      <section class="compass-list-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Compass</span>
            <h2>问题解决锦囊</h2>
          </div>
        </div>

        <div class="compass-list">
          <button
            v-for="item in compasses"
            :key="courseKey(item)"
            type="button"
            class="compass-row"
            :class="{ active: selectedKey === courseKey(item) }"
            @click="selectCompass(item)"
            @dblclick="openCompass(item)"
          >
            <div class="compass-cover">
              <img v-if="item.icon" :src="item.icon" alt="" />
              <span v-else>{{ (item.title || '?').slice(0, 1) }}</span>
            </div>
            <div class="compass-main">
              <strong>{{ item.title || `Compass ${item.class_id || item.id}` }}</strong>
              <p>{{ item.intro || item.author || '暂无简介' }}</p>
              <div class="compass-meta">
                <span>{{ item.author || '得到锦囊' }}</span>
                <small>{{ item.price || '已购买' }}</small>
              </div>
            </div>
            <div class="compass-side">
              <span>{{ safeProgress(item.progress) }}%</span>
              <small>{{ item.last_read || '未开始' }}</small>
            </div>
          </button>

          <div v-if="!loading && !compasses.length" class="empty-state">
            {{ token ? '当前页没有锦囊，尝试刷新、换页或重新扫码登录。' : '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。' }}
          </div>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">Prev</button>
          <span>Page {{ page }} / {{ totalPages || 1 }} · {{ total }} compass</span>
          <button type="button" :disabled="!canGoNext || loading" @click="changePage(page + 1)">Next</button>
        </div>
      </section>

      <aside class="compass-detail-panel">
        <span class="eyebrow">Detail</span>
        <h2>{{ selectedCompass?.title || '选择一个锦囊' }}</h2>
        <p>{{ selectedCompass?.intro || '锦囊用于围绕具体问题快速学习和行动。' }}</p>
        <dl class="detail-list">
          <div>
            <dt>ID</dt>
            <dd>{{ selectedCompass?.class_id || selectedCompass?.id || '-' }}</dd>
          </div>
          <div>
            <dt>ENID</dt>
            <dd>{{ selectedCompass?.enid || '-' }}</dd>
          </div>
          <div>
            <dt>答复人</dt>
            <dd>{{ selectedCompass?.author || '-' }}</dd>
          </div>
          <div>
            <dt>进度</dt>
            <dd>{{ selectedCompass ? `${safeProgress(selectedCompass.progress)}%` : '-' }}</dd>
          </div>
        </dl>
        <button class="primary-action full" type="button" :disabled="!selectedCompass" @click="selectedCompass && openCompass(selectedCompass)">
          打开学习
        </button>
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getBrowserSession, KBaseClient, type DedaoCourse } from '../api'

const storageKey = 'dedao-kbase-web-settings'
const router = useRouter()

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const errorMessage = ref('')
const query = ref('')
const page = ref(1)
const pageSize = ref(15)
const total = ref(0)
const totalPages = ref(0)
const isMore = ref(0)
const compasses = ref<DedaoCourse[]>([])
const selectedKey = ref('')

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const selectedCompass = computed(() => compasses.value.find((item) => courseKey(item) === selectedKey.value) || null)
const canGoNext = computed(() => {
  if (totalPages.value > 0) {
    return page.value < totalPages.value
  }
  return isMore.value === 1 || compasses.value.length >= pageSize.value
})

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await loadCompasses()
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

const loadCompasses = async () => {
  if (!token.value) {
    connected.value = false
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    await hydrateBrowserSession()
    saveConnection()
    const result = await client.value.listDedaoCourses(page.value, pageSize.value, query.value, { category: 'compass' })
    compasses.value = result.courses || []
    page.value = result.page || page.value
    pageSize.value = result.page_size || pageSize.value
    total.value = result.total || 0
    totalPages.value = result.total_pages || 0
    isMore.value = result.is_more || 0
    connected.value = true
    if (!compasses.value.some((item) => courseKey(item) === selectedKey.value)) {
      selectedKey.value = compasses.value[0] ? courseKey(compasses.value[0]) : ''
    }
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const reloadFromFirstPage = async () => {
  page.value = 1
  await loadCompasses()
}

const changePage = async (nextPage: number) => {
  page.value = Math.max(1, nextPage)
  await loadCompasses()
}

const selectCompass = (item: DedaoCourse) => {
  selectedKey.value = courseKey(item)
}

const openCompass = (item: DedaoCourse) => {
  selectCompass(item)
  router.push(`/course/${encodeURIComponent(courseKey(item))}`)
}

const courseKey = (course: DedaoCourse) => course.enid || String(course.class_id || course.id)
const safeProgress = (value: number) => Math.max(0, Math.min(100, Number.isFinite(value) ? Math.round(value) : 0))
</script>

<style scoped>
.compass-library {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: calc(100vh - 80px);
  margin-top: 8px;
}

.compass-toolbar,
.compass-filter-panel,
.compass-list-panel,
.compass-detail-panel {
  border: 1px solid var(--dedao-line);
  border-radius: 10px;
  background: #ffffff;
}

.compass-toolbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 92px 82px;
  gap: 10px;
  align-items: center;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 6px 0 10px;
}

.toolbar-summary {
  display: grid;
  gap: 2px;
}

.toolbar-summary strong {
  color: #111111;
  font-size: 22px;
}

.toolbar-summary span,
.compass-filter-panel dt,
.compass-row small,
.compass-meta,
.detail-list dt {
  color: var(--dedao-muted);
  font-size: 12px;
}

.compass-workspace {
  display: grid;
  grid-template-columns: minmax(210px, 260px) minmax(0, 1fr) minmax(220px, 300px);
  gap: 10px;
}

.compass-filter-panel,
.compass-list-panel,
.compass-detail-panel {
  min-width: 0;
  padding: 14px;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.panel-head h2,
.compass-detail-panel h2 {
  margin: 0;
  color: #111111;
  font-size: 22px;
  line-height: 30px;
}

.eyebrow {
  color: var(--dedao-muted);
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
}

.filter-stack {
  display: grid;
  gap: 8px;
}

.filter-stack input,
.filter-stack select {
  min-height: 40px;
  border: 1px solid var(--dedao-border);
  border-radius: 8px;
  padding: 0 10px;
  color: var(--dedao-text);
  font-size: 14px;
}

.stat-grid {
  display: grid;
  gap: 8px;
  margin: 12px 0 0;
}

.stat-grid div,
.detail-list div {
  border-radius: 8px;
  padding: 10px;
  background: var(--dedao-subtle);
}

.stat-grid dd,
.detail-list dd {
  margin: 3px 0 0;
  color: #111111;
  font-weight: 800;
}

.compass-list {
  display: grid;
  gap: 0;
}

.compass-row {
  display: grid;
  grid-template-columns: 74px minmax(0, 1fr) 74px;
  gap: 12px;
  align-items: center;
  min-height: 96px;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 12px 0;
  background: #ffffff;
  text-align: left;
}

.compass-row.active {
  background: #fff8f2;
}

.compass-cover {
  display: grid;
  overflow: hidden;
  width: 74px;
  height: 74px;
  place-items: center;
  border-radius: 8px;
  background: var(--dedao-subtle);
  color: var(--dedao-orange);
  font-size: 24px;
  font-weight: 800;
}

.compass-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.compass-main {
  min-width: 0;
}

.compass-main strong {
  display: block;
  overflow: hidden;
  color: #222222;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 18px;
}

.compass-main p,
.compass-detail-panel p {
  margin: 6px 0;
  color: #666666;
  line-height: 22px;
}

.compass-meta {
  display: flex;
  justify-content: space-between;
  gap: 10px;
}

.compass-side {
  display: grid;
  justify-items: end;
  gap: 4px;
}

.compass-side span {
  color: #111111;
  font-size: 22px;
  font-weight: 800;
}

.detail-list {
  display: grid;
  gap: 8px;
  margin: 14px 0;
}

.primary-action.full {
  width: 100%;
}

.empty-state {
  border: 1px dashed var(--dedao-border);
  border-radius: 8px;
  padding: 22px;
  color: var(--dedao-muted);
  text-align: center;
}

@media (max-width: 1080px) {
  .compass-toolbar,
  .compass-workspace {
    grid-template-columns: 1fr;
  }
}
</style>
