<template>
  <main class="ebook-library">
    <section class="ebook-toolbar">
      <div class="brand-block">
        <span class="eyebrow">Dedao Ebooks</span>
        <h2>电子书架</h2>
      </div>

      <label>
        <span>Base URL</span>
        <input v-model="baseUrl" name="ebookBaseUrl" placeholder="http://127.0.0.1:8719" />
      </label>

      <label>
        <span>Token</span>
        <input v-model="token" name="ebookToken" type="password" placeholder="KBASE_AUTH_TOKEN" />
      </label>

      <button class="primary-action" type="button" :disabled="loading" @click="reloadFromFirstPage">
        {{ loading ? '加载中' : '刷新书架' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="ebook-workspace">
      <aside class="ebook-filter-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Search</span>
            <h2>筛选书架</h2>
          </div>
        </div>

        <div class="ebook-filter-stack">
          <input v-model="query" placeholder="输入书名、作者或简介关键词" @keydown.enter="reloadFromFirstPage" />
          <select v-model.number="pageSize" @change="reloadFromFirstPage">
            <option :value="10">10/page</option>
            <option :value="15">15/page</option>
            <option :value="30">30/page</option>
            <option :value="50">50/page</option>
          </select>
          <button type="button" class="primary-action" :disabled="loading" @click="reloadFromFirstPage">Search</button>
        </div>

        <dl class="ebook-metrics">
          <div>
            <dt>Total</dt>
            <dd>{{ total }}</dd>
          </div>
          <div>
            <dt>Page</dt>
            <dd>{{ page }} / {{ totalPages || 1 }}</dd>
          </div>
          <div>
            <dt>Loaded</dt>
            <dd>{{ ebooks.length }}</dd>
          </div>
        </dl>
      </aside>

      <section class="ebook-list-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Bookshelf</span>
            <h2>已购电子书</h2>
          </div>
          <span class="ebook-source">CourseList("ebook", "study")</span>
        </div>

        <div class="ebook-list">
          <button
            v-for="ebook in ebooks"
            :key="ebook.enid || ebook.id"
            type="button"
            class="ebook-row"
            :class="{ active: selectedKey === ebookKey(ebook) }"
            @click="selectEbook(ebook)"
          >
            <div class="cover-frame">
              <img v-if="ebook.icon" :src="ebook.icon" alt="" />
              <span v-else>{{ (ebook.title || '?').slice(0, 1) }}</span>
            </div>
            <div class="ebook-main">
              <div class="ebook-title-line">
                <strong>{{ ebook.title || `Ebook ${ebook.id}` }}</strong>
                <span>{{ ebook.price || '未标价' }}</span>
              </div>
              <p>{{ ebook.intro || ebook.author || '暂无简介' }}</p>
              <div class="progress-track" aria-label="reading progress">
                <span :style="{ width: `${safeProgress(ebook.progress)}%` }"></span>
              </div>
            </div>
            <div class="ebook-side">
              <span>{{ safeProgress(ebook.progress) }}%</span>
              <small>{{ ebook.publish_num || 0 }} 篇</small>
            </div>
          </button>

          <div v-if="!loading && !ebooks.length" class="empty-state">
            {{ token ? '当前页没有电子书，尝试刷新、换页或重新扫码登录。' : '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。' }}
          </div>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">Prev</button>
          <span>Page {{ page }} / {{ totalPages || 1 }} · {{ total }} ebooks</span>
          <button type="button" :disabled="!canGoNext || loading" @click="changePage(page + 1)">Next</button>
        </div>
      </section>

      <aside class="ebook-detail-panel">
        <span class="eyebrow">Study Context</span>
        <h2>{{ selectedEbook?.title || '选择一本电子书' }}</h2>
        <p>{{ selectedEbook?.intro || '这里显示当前书的学习摘要。详情、书评和下载会在后续切片迁移。' }}</p>
        <dl class="ebook-detail-list">
          <div>
            <dt>ID</dt>
            <dd>{{ selectedEbook?.id || '-' }}</dd>
          </div>
          <div>
            <dt>ENID</dt>
            <dd>{{ selectedEbook?.enid || '-' }}</dd>
          </div>
          <div>
            <dt>Author</dt>
            <dd>{{ selectedEbook?.author || '-' }}</dd>
          </div>
          <div>
            <dt>Last Read</dt>
            <dd>{{ selectedEbook?.last_read || '-' }}</dd>
          </div>
        </dl>
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getBrowserSession, KBaseClient, type DedaoEbook } from '../api'

const storageKey = 'dedao-kbase-web-settings'

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
const ebooks = ref<DedaoEbook[]>([])
const selectedKey = ref('')

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const selectedEbook = computed(() => ebooks.value.find((ebook) => ebookKey(ebook) === selectedKey.value) || null)
const canGoNext = computed(() => {
  if (totalPages.value > 0) {
    return page.value < totalPages.value
  }
  return isMore.value === 1 || ebooks.value.length >= pageSize.value
})

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await loadEbooks()
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

const loadEbooks = async () => {
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
    const result = await client.value.listDedaoEbooks(page.value, pageSize.value, query.value)
    ebooks.value = result.ebooks || []
    page.value = result.page || page.value
    pageSize.value = result.page_size || pageSize.value
    total.value = result.total || 0
    totalPages.value = result.total_pages || 0
    isMore.value = result.is_more || 0
    connected.value = true
    if (!ebooks.value.some((ebook) => ebookKey(ebook) === selectedKey.value)) {
      selectedKey.value = ebooks.value[0] ? ebookKey(ebooks.value[0]) : ''
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
  await loadEbooks()
}

const changePage = async (nextPage: number) => {
  page.value = Math.max(1, nextPage)
  await loadEbooks()
}

const selectEbook = (ebook: DedaoEbook) => {
  selectedKey.value = ebookKey(ebook)
}

const ebookKey = (ebook: DedaoEbook) => ebook.enid || String(ebook.id)
const safeProgress = (value: number) => Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
</script>

<style scoped>
.ebook-library {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: calc(100vh - 156px);
  margin-top: 12px;
}

.ebook-toolbar,
.ebook-filter-panel,
.ebook-list-panel,
.ebook-detail-panel {
  border: 1px solid #d3dce8;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 10px 24px rgb(37 51 74 / 8%);
}

.ebook-toolbar {
  display: grid;
  grid-template-columns: 220px minmax(260px, 1fr) minmax(260px, 1fr) 120px 96px;
  gap: 12px;
  align-items: end;
  padding: 12px;
}

.ebook-workspace {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr) 300px;
  gap: 12px;
}

.ebook-filter-panel,
.ebook-list-panel,
.ebook-detail-panel {
  min-width: 0;
  padding: 12px;
}

.ebook-filter-stack {
  display: grid;
  gap: 8px;
  margin-top: 12px;
}

.ebook-metrics,
.ebook-detail-list {
  display: grid;
  gap: 8px;
  margin: 14px 0 0;
}

.ebook-metrics div,
.ebook-detail-list div {
  border: 1px solid #dbe2ec;
  border-radius: 7px;
  padding: 9px;
  background: #fbfcfe;
}

.ebook-metrics dt,
.ebook-detail-list dt {
  color: #6c7b8f;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.ebook-metrics dd,
.ebook-detail-list dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  color: #121926;
  font-size: 14px;
  font-weight: 700;
}

.ebook-source {
  align-self: center;
  border: 1px solid #dbe2ec;
  border-radius: 999px;
  padding: 6px 9px;
  background: #fbfcfe;
  color: #536274;
  font-size: 12px;
}

.ebook-list {
  display: grid;
  gap: 8px;
  margin-top: 12px;
}

.ebook-row {
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr) 72px;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 76px;
  padding: 9px;
  text-align: left;
}

.ebook-row.active {
  border-color: #397367;
  background: #f1faf6;
}

.cover-frame {
  display: grid;
  overflow: hidden;
  width: 52px;
  height: 68px;
  place-items: center;
  border: 1px solid #dbe2ec;
  border-radius: 6px;
  background: #f6f8fb;
  color: #607086;
  font-weight: 800;
}

.cover-frame img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.ebook-main {
  min-width: 0;
}

.ebook-title-line {
  display: flex;
  min-width: 0;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.ebook-title-line strong,
.ebook-title-line span,
.ebook-main p {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ebook-title-line strong {
  color: #121926;
  font-size: 14px;
}

.ebook-title-line span {
  flex: 0 0 auto;
  color: #397367;
  font-size: 12px;
  font-weight: 700;
}

.ebook-main p {
  margin: 5px 0 8px;
  color: #536274;
  font-size: 12px;
}

.progress-track {
  overflow: hidden;
  width: 100%;
  height: 6px;
  border-radius: 999px;
  background: #e7edf4;
}

.progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #397367;
}

.ebook-side {
  display: grid;
  justify-items: end;
  gap: 4px;
  color: #121926;
  font-weight: 800;
}

.ebook-side small {
  color: #6c7b8f;
  font-size: 11px;
  font-weight: 700;
}

.ebook-detail-panel h2 {
  margin: 0;
  color: #121926;
  font-size: 19px;
  line-height: 26px;
}

.ebook-detail-panel p {
  margin: 8px 0 0;
  color: #536274;
  font-size: 13px;
}

.empty-state {
  border: 1px dashed #c8d2df;
  border-radius: 8px;
  padding: 24px;
  color: #6c7b8f;
  text-align: center;
}
</style>
