<template>
  <main class="ebook-library">
    <section class="ebook-toolbar">
      <div class="toolbar-summary">
        <strong>电子书架</strong>
        <span>{{ total }} 本 · 第 {{ page }} / {{ totalPages || 1 }} 页</span>
      </div>

      <button class="primary-action" type="button" :disabled="loading" @click="reloadFromFirstPage">
        {{ loading ? '加载中' : '刷新书架' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="ebook-workspace">
      <aside class="ebook-filter-panel">
        <div class="ebook-filter-stack">
          <input v-model="query" placeholder="输入书名、作者或简介关键词" @keydown.enter="reloadFromFirstPage" />
          <select v-model.number="pageSize" @change="reloadFromFirstPage">
            <option :value="10">10/page</option>
            <option :value="15">15/page</option>
            <option :value="30">30/page</option>
            <option :value="50">50/page</option>
          </select>
          <button type="button" class="primary-action" :disabled="loading" @click="reloadFromFirstPage">搜索</button>
        </div>
        <p class="ebook-filter-summary">已载入 {{ ebooks.length }} / {{ total || ebooks.length }}</p>
      </aside>

      <section class="ebook-list-panel">
        <div class="panel-head">
          <div>
            <h2>已购电子书</h2>
          </div>
        </div>

        <div class="ebook-list">
          <article
            v-for="ebook in ebooks"
            :key="ebook.enid || ebook.id"
            class="ebook-row"
            :class="{ active: selectedKey === ebookKey(ebook) }"
            role="button"
            tabindex="0"
            @click="openEbook(ebook)"
            @keydown.enter.prevent="openEbook(ebook)"
            @keydown.space.prevent="openEbook(ebook)"
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
            <div class="ebook-action-bar" @click.stop @keydown.stop>
              <select :value="downloadTypeFor(ebook)" @change.stop="setDownloadType(ebook, $event)">
                <option v-for="option in downloadTypeOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
              <button
                type="button"
                class="secondary-action"
                :disabled="isEbookActionLoading('sync', ebook)"
                @click.stop="createEbookSyncJob(ebook)"
              >
                {{ isEbookActionLoading('sync', ebook) ? '入库中' : '加入书籍知识库' }}
              </button>
              <button
                type="button"
                class="primary-action compact"
                :disabled="isEbookActionLoading('download', ebook)"
                @click.stop="createEbookDownloadJob(ebook)"
              >
                {{ isEbookActionLoading('download', ebook) ? '下载中' : '下载' }}
              </button>
            </div>
          </article>

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
        <h2>{{ selectedEbook?.title || '选择一本电子书' }}</h2>
        <p>{{ selectedEbook?.intro || '这里显示当前书的学习摘要，可从书架直接下载或加入书籍知识库。' }}</p>
        <div v-if="selectedEbook" class="detail-action-row">
          <select :value="downloadTypeFor(selectedEbook)" @change="setDownloadType(selectedEbook, $event)">
            <option v-for="option in downloadTypeOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
          <button type="button" class="secondary-action" @click="createEbookSyncJob(selectedEbook)">加入知识库</button>
          <button type="button" class="primary-action compact" @click="createEbookDownloadJob(selectedEbook)">下载</button>
        </div>
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
        <section class="ebook-job-status">
          <div class="job-status-head">
            <div>
              <span class="eyebrow">Jobs</span>
              <strong>当前书任务</strong>
            </div>
            <button type="button" class="secondary-action tiny" :disabled="jobLoading" @click="loadJobs">
              {{ jobLoading ? '刷新中' : '刷新' }}
            </button>
          </div>
          <div v-if="selectedEbookJobs.length" class="job-status-list">
            <article v-for="job in selectedEbookJobs" :key="job.id" class="job-status-item">
              <div>
                <strong>{{ jobTypeLabel(job) }}</strong>
                <span :class="['job-pill', job.status]">{{ job.status }}</span>
              </div>
              <p v-if="job.error">{{ job.error }}</p>
              <small>{{ formatJobTime(job.updated_at) }}</small>
            </article>
          </div>
          <p v-else class="job-empty">暂无当前书任务。</p>
        </section>
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getBrowserSession, KBaseClient, type BookKnowledgeJob, type DedaoEbook } from '../api'

const storageKey = 'dedao-kbase-web-settings'
const ebookDownloadJobType = 'dedao_ebook_download'
const ebookSyncJobType = 'dedao_ebook_sync_kbase'
const downloadTypeOptions = [
  { value: 1, label: 'HTML' },
  { value: 2, label: 'PDF' },
  { value: 3, label: 'EPUB' },
]
const router = useRouter()

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const jobLoading = ref(false)
const errorMessage = ref('')
const query = ref('')
const page = ref(1)
const pageSize = ref(15)
const total = ref(0)
const totalPages = ref(0)
const isMore = ref(0)
const ebooks = ref<DedaoEbook[]>([])
const selectedKey = ref('')
const jobs = ref<BookKnowledgeJob[]>([])
const actionLoadingKey = ref('')
const downloadTypes = ref<Record<string, number>>({})

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const selectedEbook = computed(() => ebooks.value.find((ebook) => ebookKey(ebook) === selectedKey.value) || null)
const selectedEbookJobs = computed(() => {
  const ebook = selectedEbook.value
  if (!ebook) {
    return []
  }
  return jobs.value.filter((job) => jobMatchesEbook(job, ebook)).slice(0, 6)
})
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
      await loadJobs()
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
    for (const ebook of ebooks.value) {
      const key = ebookKey(ebook)
      if (key && !downloadTypes.value[key]) {
        downloadTypes.value[key] = 1
      }
    }
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
  await loadJobs()
}

const changePage = async (nextPage: number) => {
  page.value = Math.max(1, nextPage)
  await loadEbooks()
}

const selectEbook = (ebook: DedaoEbook) => {
  selectedKey.value = ebookKey(ebook)
}

const openEbook = (ebook: DedaoEbook) => {
  selectEbook(ebook)
  router.push(`/ebook/${encodeURIComponent(ebookKey(ebook))}`)
}

const loadJobs = async () => {
  if (!token.value) {
    return
  }
  jobLoading.value = true
  try {
    jobs.value = await client.value.listJobs(50)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    jobLoading.value = false
  }
}

const createEbookDownloadJob = async (ebook: DedaoEbook | null) => {
  await createEbookJob(ebook, 'download')
}

const createEbookSyncJob = async (ebook: DedaoEbook | null) => {
  await createEbookJob(ebook, 'sync')
}

const createEbookJob = async (ebook: DedaoEbook | null, action: 'download' | 'sync') => {
  if (!ebook) {
    return
  }
  await hydrateBrowserSession()
  if (!token.value) {
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  const key = ebookKey(ebook)
  if (!ebook.id || !key) {
    errorMessage.value = '当前电子书缺少 id 或 enid，无法创建任务。'
    return
  }
  selectEbook(ebook)
  actionLoadingKey.value = ebookActionKey(action, ebook)
  errorMessage.value = ''
  try {
    saveConnection()
    const job = await client.value.createJob({
      type: action === 'download' ? ebookDownloadJobType : ebookSyncJobType,
      ebook_id: ebook.id,
      ebook_enid: key,
      download_type: action === 'download' ? downloadTypeFor(ebook) : 1,
    })
    jobs.value = [job, ...jobs.value.filter((item) => item.id !== job.id)]
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    actionLoadingKey.value = ''
  }
}

const ebookKey = (ebook: DedaoEbook) => ebook.enid || String(ebook.id)
const safeProgress = (value: number) => Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
const downloadTypeFor = (ebook: DedaoEbook | null) => (ebook ? downloadTypes.value[ebookKey(ebook)] || 1 : 1)
const setDownloadType = (ebook: DedaoEbook | null, event: Event) => {
  if (!ebook) {
    return
  }
  const value = Number((event.target as HTMLSelectElement).value)
  downloadTypes.value = {
    ...downloadTypes.value,
    [ebookKey(ebook)]: value,
  }
}
const ebookActionKey = (action: 'download' | 'sync', ebook: DedaoEbook) => `${action}:${ebookKey(ebook)}`
const isEbookActionLoading = (action: 'download' | 'sync', ebook: DedaoEbook) =>
  actionLoadingKey.value === ebookActionKey(action, ebook)
const jobMatchesEbook = (job: BookKnowledgeJob, ebook: DedaoEbook) => {
  const key = ebookKey(ebook)
  const resultEbookID = Number(job.result?.ebook_id || 0)
  const resultEbookEnID = typeof job.result?.ebook_enid === 'string' ? job.result.ebook_enid : ''
  return job.ebook_id === ebook.id || resultEbookID === ebook.id || job.ebook_enid === key || resultEbookEnID === key
}
const jobTypeLabel = (job: BookKnowledgeJob) => {
  if (job.type === ebookDownloadJobType) {
    return `下载 ${downloadTypeLabel(job.download_type || Number(job.result?.download_type || 1))}`
  }
  if (job.type === ebookSyncJobType) {
    return '加入书籍知识库'
  }
  return job.type
}
const downloadTypeLabel = (value: number) => downloadTypeOptions.find((option) => option.value === value)?.label || 'HTML'
const formatJobTime = (value?: string) => {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>

<style scoped>
.ebook-library {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: calc(100vh - 80px);
  margin-top: 8px;
}

.ebook-toolbar,
.ebook-filter-panel,
.ebook-list-panel,
.ebook-detail-panel {
  border: 1px solid var(--dedao-line);
  border-radius: 10px;
  background: #ffffff;
  box-shadow: none;
}

.ebook-toolbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 112px 82px;
  gap: 10px;
  align-items: center;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 6px 0 10px;
}

.toolbar-summary {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 10px;
}

.toolbar-summary strong {
  color: #111111;
  font-size: 18px;
  line-height: 24px;
}

.toolbar-summary span {
  overflow: hidden;
  color: var(--dedao-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.ebook-workspace {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr) 260px;
  gap: 10px;
}

.ebook-filter-panel,
.ebook-list-panel,
.ebook-detail-panel {
  min-width: 0;
  padding: 10px;
}

.ebook-filter-stack {
  display: grid;
  gap: 8px;
  margin-top: 0;
}

.ebook-detail-list {
  display: grid;
  gap: 0;
  margin: 10px 0 0;
  border-top: 1px solid var(--dedao-line);
}

.ebook-detail-list dt {
  color: var(--dedao-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.ebook-detail-list dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  color: #111111;
  font-size: 14px;
  font-weight: 700;
}

.ebook-detail-list div {
  border-bottom: 1px solid var(--dedao-line);
  padding: 8px 0;
}

.ebook-filter-summary {
  margin: 8px 0 0;
  color: var(--dedao-muted);
  font-size: 12px;
}

.ebook-list {
  display: grid;
  gap: 0;
  margin-top: 4px;
}

.ebook-row {
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr) 72px;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 74px;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 10px 0;
  background: #ffffff;
  cursor: pointer;
  text-align: left;
}

.ebook-row.active {
  color: var(--dedao-orange);
  background: #fffaf6;
}

.cover-frame {
  display: grid;
  overflow: hidden;
  width: 52px;
  height: 68px;
  place-items: center;
  border: 1px solid var(--dedao-line);
  border-radius: 6px;
  background: var(--dedao-subtle);
  color: var(--dedao-muted);
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
  color: #222222;
  font-size: 15px;
}

.ebook-title-line span {
  flex: 0 0 auto;
  color: var(--dedao-orange);
  font-size: 12px;
  font-weight: 700;
}

.ebook-main p {
  margin: 5px 0 8px;
  color: var(--dedao-muted);
  font-size: 12px;
}

.progress-track {
  overflow: hidden;
  width: 100%;
  height: 6px;
  border-radius: 999px;
  background: #eeeeee;
}

.progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--dedao-orange);
}

.ebook-side {
  display: grid;
  justify-items: end;
  gap: 4px;
  color: #222222;
  font-weight: 800;
}

.ebook-side small {
  color: var(--dedao-muted);
  font-size: 11px;
  font-weight: 700;
}

.ebook-action-bar {
  display: flex;
  grid-column: 2 / 4;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.ebook-action-bar select,
.detail-action-row select {
  height: 32px;
  border: 1px solid var(--dedao-border);
  border-radius: 7px;
  padding: 0 8px;
  background: #ffffff;
  color: var(--dedao-text);
  font-size: 12px;
  font-weight: 700;
}

.secondary-action {
  min-height: 32px;
  border: 1px solid var(--dedao-border);
  border-radius: 7px;
  padding: 0 10px;
  background: var(--dedao-subtle);
  color: var(--dedao-text);
  font-size: 12px;
  font-weight: 700;
}

.secondary-action:hover {
  border-color: var(--dedao-orange);
  color: var(--dedao-orange);
}

.secondary-action:disabled,
.primary-action:disabled {
  cursor: not-allowed;
  opacity: 0.62;
}

.primary-action.compact {
  min-height: 32px;
  padding: 0 12px;
}

.secondary-action.tiny {
  min-height: 28px;
  padding: 0 8px;
}

.ebook-detail-panel h2 {
  margin: 0;
  color: #111111;
  font-size: 18px;
  line-height: 24px;
}

.ebook-detail-panel p {
  margin: 8px 0 0;
  color: var(--dedao-muted);
  font-size: 13px;
}

.detail-action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.ebook-job-status {
  margin-top: 14px;
  border-top: 1px solid var(--dedao-line);
  padding-top: 12px;
}

.job-status-head,
.job-status-item div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.job-status-head strong {
  display: block;
  margin-top: 2px;
  color: #111111;
  font-size: 14px;
}

.job-status-list {
  display: grid;
  gap: 8px;
  margin-top: 10px;
}

.job-status-item {
  border: 1px solid var(--dedao-line);
  border-radius: 8px;
  padding: 9px;
  background: var(--dedao-subtle);
}

.job-status-item strong {
  color: #222222;
  font-size: 12px;
}

.job-status-item p,
.job-empty {
  margin: 8px 0 0;
  overflow-wrap: anywhere;
  color: #8a3d33;
  font-size: 12px;
}

.job-status-item small {
  display: block;
  margin-top: 6px;
  color: var(--dedao-muted);
  font-size: 11px;
}

.job-pill {
  border: 1px solid var(--dedao-border);
  border-radius: 999px;
  padding: 3px 7px;
  color: var(--dedao-muted);
  font-size: 10px;
  font-weight: 800;
  text-transform: uppercase;
}

.job-pill.running,
.job-pill.queued {
  border-color: #b8c9e4;
  background: #eef5ff;
  color: #2f5f92;
}

.job-pill.succeeded {
  border-color: #a7d1bd;
  background: #effaf4;
  color: #257347;
}

.job-pill.failed {
  border-color: #e5b8ac;
  background: #fff4f1;
  color: #9b3f2e;
}

.empty-state {
  border: 1px dashed var(--dedao-border);
  border-radius: 10px;
  padding: 24px;
  color: var(--dedao-muted);
  text-align: center;
}
</style>
