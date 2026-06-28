<template>
  <main class="odob-library">
    <section class="odob-toolbar">
      <div class="toolbar-summary">
        <strong>听书书架</strong>
        <span>{{ total }} 本 · 第 {{ page }} / {{ totalPages || 1 }} 页</span>
      </div>

      <button class="primary-action" type="button" :disabled="loading" @click="reloadFromFirstPage">
        {{ loading ? '加载中' : '刷新听书' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="odob-workspace">
      <aside class="odob-filter-panel">
        <input v-model="query" placeholder="搜索书名、作者或简介" @keydown.enter="reloadFromFirstPage" />
        <select v-model.number="pageSize" @change="reloadFromFirstPage">
          <option :value="10">10/page</option>
          <option :value="15">15/page</option>
          <option :value="30">30/page</option>
          <option :value="50">50/page</option>
        </select>
        <button type="button" class="primary-action" :disabled="loading" @click="reloadFromFirstPage">搜索</button>
        <p>已载入 {{ odobs.length }} / {{ total || odobs.length }}</p>
      </aside>

      <section class="odob-list-panel">
        <div class="panel-head">
          <h2>已购听书</h2>
        </div>

        <div class="odob-list">
          <article
            v-for="odob in odobs"
            :key="odobKey(odob)"
            class="odob-row"
            :class="{ active: selectedKey === odobKey(odob) }"
            role="button"
            tabindex="0"
            @click="selectOdob(odob)"
            @keydown.enter.prevent="selectOdob(odob)"
            @keydown.space.prevent="selectOdob(odob)"
          >
            <div class="cover-frame">
              <img v-if="odob.icon || odob.audio_icon" :src="odob.icon || odob.audio_icon" alt="" />
              <span v-else>{{ (odob.title || '?').slice(0, 1) }}</span>
            </div>
            <div class="odob-main">
              <div class="odob-title-line">
                <strong>{{ odob.title || `Odob ${odob.id}` }}</strong>
                <span>{{ odob.price || '已购' }}</span>
              </div>
              <p>{{ odob.intro || odob.author || odob.audio_title || '暂无简介' }}</p>
              <div class="progress-track" aria-label="listening progress">
                <span :style="{ width: `${safeProgress(odob.progress)}%` }"></span>
              </div>
            </div>
            <div class="odob-side">
              <span>{{ safeProgress(odob.progress) }}%</span>
              <small>{{ formatDuration(odob.duration || odob.audio_duration) }}</small>
            </div>
            <div class="odob-action-bar" @click.stop @keydown.stop>
              <select :value="downloadTypeFor(odob)" @change.stop="setDownloadType(odob, $event)">
                <option v-for="option in downloadTypeOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
              <button type="button" class="secondary-action" :disabled="transcriptLoading" @click.stop="loadTranscript(odob)">
                文稿
              </button>
              <button
                type="button"
                class="primary-action compact"
                :disabled="isOdobActionLoading(odob)"
                @click.stop="createOdobDownloadJob(odob)"
              >
                {{ isOdobActionLoading(odob) ? '下载中' : '下载' }}
              </button>
            </div>
          </article>

          <div v-if="!loading && !odobs.length" class="empty-state">
            {{ token ? '当前页没有听书，尝试刷新、换页或重新扫码登录。' : '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。' }}
          </div>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">Prev</button>
          <span>Page {{ page }} / {{ totalPages || 1 }} · {{ total }} odobs</span>
          <button type="button" :disabled="!canGoNext || loading" @click="changePage(page + 1)">Next</button>
        </div>
      </section>

      <aside class="odob-study-panel">
        <div class="study-title">
          <div>
            <span class="eyebrow">Odob Study</span>
            <h2>{{ selectedOdob?.title || '选择一本听书' }}</h2>
          </div>
          <button type="button" class="secondary-action tiny" :disabled="detailLoading || !selectedOdob" @click="loadSelectedDetail">
            {{ detailLoading ? '加载中' : '详情' }}
          </button>
        </div>
        <p>{{ selectedSummary }}</p>

        <audio v-if="playSource" class="odob-player" controls :src="playSource"></audio>

        <div v-if="selectedOdob" class="detail-action-row">
          <select :value="downloadTypeFor(selectedOdob)" @change="setDownloadType(selectedOdob, $event)">
            <option v-for="option in downloadTypeOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
          <button type="button" class="secondary-action" :disabled="transcriptLoading" @click="loadTranscript(selectedOdob)">
            {{ transcriptLoading ? '加载文稿' : '阅读文稿' }}
          </button>
          <button type="button" class="primary-action compact" :disabled="isOdobActionLoading(selectedOdob)" @click="createOdobDownloadJob(selectedOdob)">
            下载
          </button>
        </div>

        <dl class="odob-detail-list">
          <div>
            <dt>ID</dt>
            <dd>{{ selectedOdob?.id || '-' }}</dd>
          </div>
          <div>
            <dt>ENID</dt>
            <dd>{{ selectedOdob?.enid || '-' }}</dd>
          </div>
          <div>
            <dt>Audio Alias</dt>
            <dd>{{ selectedOdob?.audio_alias_id || '-' }}</dd>
          </div>
          <div>
            <dt>Progress</dt>
            <dd>{{ selectedOdob ? `${safeProgress(selectedOdob.progress)}%` : '-' }}</dd>
          </div>
        </dl>

        <section v-if="selectedDetail" class="detail-block">
          <div class="tag-row">
            <span v-for="tag in selectedDetail.tags || []" :key="tag">{{ tag }}</span>
          </div>
          <p v-if="selectedDetail.agency?.name">出品方：{{ selectedDetail.agency.name }}</p>
          <p v-if="selectedDetail.learn_count_desc">{{ selectedDetail.learn_count_desc }}</p>
          <ul v-if="selectedDetail.topic_summary?.length">
            <li v-for="topic in selectedDetail.topic_summary" :key="topic.title">
              <strong>{{ topic.title }}</strong>
              <span>{{ topic.sub_title }}</span>
            </li>
          </ul>
        </section>

        <section v-if="transcript" class="transcript-block">
          <div class="transcript-head">
            <span class="eyebrow">Transcript</span>
            <strong>{{ transcript.title || selectedOdob?.title }}</strong>
          </div>
          <div class="transcript-markdown" v-html="renderedTranscript"></div>
        </section>

        <section class="odob-job-status">
          <div class="job-status-head">
            <div>
              <span class="eyebrow">Jobs</span>
              <strong>当前听书任务</strong>
            </div>
            <button type="button" class="secondary-action tiny" :disabled="jobLoading" @click="loadJobs">
              {{ jobLoading ? '刷新中' : '刷新' }}
            </button>
          </div>
          <div v-if="selectedOdobJobs.length" class="job-status-list">
            <article v-for="job in selectedOdobJobs" :key="job.id" class="job-status-item">
              <div>
                <strong>{{ jobTypeLabel(job) }}</strong>
                <span :class="['job-pill', job.status]">{{ job.status }}</span>
              </div>
              <p v-if="job.error">{{ job.error }}</p>
              <small>{{ formatJobTime(job.updated_at) }}</small>
            </article>
          </div>
          <p v-else class="job-empty">暂无当前听书任务。</p>
        </section>

        <PageAnalysisPanel
          :base-url="baseUrl"
          :token="token"
          source="odob"
          :page-title="selectedOdob?.title || '听书书架'"
          page-url="/odob"
          :context-sections="analysisSections"
          default-question="基于当前听书内容，提炼重点、复盘问题和后续学习建议。"
        />
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import PageAnalysisPanel from '../components/PageAnalysisPanel.vue'
import {
  getBrowserSession,
  KBaseClient,
  type BookKnowledgeJob,
  type DedaoArticleMarkdown,
  type DedaoOdob,
  type DedaoOdobDetail,
  type PageAnalysisSection,
} from '../api'
import { renderMarkdown } from '../utils/markdownRender'

const storageKey = 'dedao-kbase-web-settings'
const odobDownloadJobType = 'dedao_odob_download'
const downloadTypeOptions = [
  { value: 1, label: 'MP3' },
  { value: 2, label: 'PDF文稿' },
  { value: 3, label: 'Markdown文稿' },
]

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const detailLoading = ref(false)
const transcriptLoading = ref(false)
const jobLoading = ref(false)
const errorMessage = ref('')
const query = ref('')
const page = ref(1)
const pageSize = ref(15)
const total = ref(0)
const totalPages = ref(0)
const isMore = ref(0)
const odobs = ref<DedaoOdob[]>([])
const selectedKey = ref('')
const selectedDetail = ref<DedaoOdobDetail | null>(null)
const transcript = ref<DedaoArticleMarkdown | null>(null)
const jobs = ref<BookKnowledgeJob[]>([])
const actionLoadingKey = ref('')
const downloadTypes = ref<Record<string, number>>({})

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const selectedOdob = computed(() => odobs.value.find((odob) => odobKey(odob) === selectedKey.value) || null)
const playSource = computed(() => selectedOdob.value?.audio_play_url || '')
const selectedSummary = computed(() =>
  selectedDetail.value?.audio_summary ||
  selectedOdob.value?.intro ||
  selectedOdob.value?.audio_title ||
  '这里显示当前听书的简介、文稿和学习分析。',
)
const selectedOdobJobs = computed(() => {
  const odob = selectedOdob.value
  if (!odob) {
    return []
  }
  return jobs.value.filter((job) => jobMatchesOdob(job, odob)).slice(0, 6)
})
const canGoNext = computed(() => {
  if (totalPages.value > 0) {
    return page.value < totalPages.value
  }
  return isMore.value === 1 || odobs.value.length >= pageSize.value
})
const renderedTranscript = computed(() => renderMarkdown(transcript.value?.markdown || ''))
const analysisSections = computed<PageAnalysisSection[]>(() => {
  const odob = selectedOdob.value
  const detail = selectedDetail.value
  const sections: PageAnalysisSection[] = []
  if (odob) {
    sections.push({
      title: '听书条目',
      content: [
        `标题: ${odob.title}`,
        `作者/讲者: ${odob.author || '-'}`,
        `简介: ${odob.intro || '-'}`,
        `进度: ${safeProgress(odob.progress)}%`,
        `时长: ${formatDuration(odob.duration || odob.audio_duration)}`,
        `上次学习: ${odob.last_read || '-'}`,
      ].join('\n'),
    })
  }
  if (detail) {
    sections.push({
      title: '听书详情',
      content: [
        `摘要: ${detail.audio_summary || '-'}`,
        `出品方: ${detail.agency?.name || '-'}`,
        `标签: ${(detail.tags || []).join('、') || '-'}`,
        `主题: ${(detail.topic_summary || []).map((item) => `${item.title} ${item.sub_title || ''}`).join('；') || '-'}`,
      ].join('\n'),
    })
  }
  if (transcript.value?.markdown) {
    sections.push({ title: '听书文稿', content: transcript.value.markdown })
  }
  return sections.filter((section) => section.content.trim())
})

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await loadOdobs()
      await loadJobs()
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
})

watch(selectedOdob, async (next, previous) => {
  if (odobKey(next) === odobKey(previous)) {
    return
  }
  selectedDetail.value = null
  transcript.value = null
  if (next) {
    await loadOdobDetail(next)
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

const loadOdobs = async () => {
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
    const result = await client.value.listDedaoOdobs(page.value, pageSize.value, query.value)
    odobs.value = result.odobs || []
    page.value = result.page || page.value
    pageSize.value = result.page_size || pageSize.value
    total.value = result.total || 0
    totalPages.value = result.total_pages || 0
    isMore.value = result.is_more || 0
    for (const odob of odobs.value) {
      const key = odobKey(odob)
      if (key && !downloadTypes.value[key]) {
        downloadTypes.value[key] = 1
      }
    }
    connected.value = true
    if (!odobs.value.some((odob) => odobKey(odob) === selectedKey.value)) {
      selectedKey.value = odobs.value[0] ? odobKey(odobs.value[0]) : ''
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
  await loadOdobs()
  await loadJobs()
}

const changePage = async (nextPage: number) => {
  page.value = Math.max(1, nextPage)
  await loadOdobs()
}

const selectOdob = (odob: DedaoOdob) => {
  selectedKey.value = odobKey(odob)
}

const loadSelectedDetail = async () => {
  if (selectedOdob.value) {
    await loadOdobDetail(selectedOdob.value)
  }
}

const loadOdobDetail = async (odob: DedaoOdob) => {
  const key = odobKey(odob)
  if (!key || !token.value) {
    return
  }
  detailLoading.value = true
  try {
    const detail = await client.value.getDedaoOdobDetail(key)
    if (selectedKey.value === key) {
      selectedDetail.value = detail
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    detailLoading.value = false
  }
}

const loadTranscript = async (odob: DedaoOdob | null) => {
  if (!odob) {
    return
  }
  const aliasID = odob.audio_alias_id
  if (!aliasID) {
    errorMessage.value = '当前听书缺少 audio_alias_id，无法加载文稿。'
    return
  }
  await hydrateBrowserSession()
  if (!token.value) {
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  selectOdob(odob)
  transcriptLoading.value = true
  errorMessage.value = ''
  try {
    transcript.value = await client.value.getDedaoOdobArticleMarkdown(aliasID)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    transcriptLoading.value = false
  }
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

const createOdobDownloadJob = async (odob: DedaoOdob | null) => {
  if (!odob) {
    return
  }
  await hydrateBrowserSession()
  if (!token.value) {
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  const key = odobKey(odob)
  if (!odob.id || !key || !odob.audio_alias_id) {
    errorMessage.value = '当前听书缺少 id、enid 或 audio_alias_id，无法创建下载任务。'
    return
  }
  selectOdob(odob)
  actionLoadingKey.value = key
  errorMessage.value = ''
  try {
    saveConnection()
    const job = await client.value.createJob({
      type: odobDownloadJobType,
      odob_id: odob.id,
      odob_enid: key,
      odob_title: odob.title,
      odob_alias_id: odob.audio_alias_id,
      odob_can_play: odob.has_play_auth,
      download_type: downloadTypeFor(odob),
    })
    jobs.value = [job, ...jobs.value.filter((item) => item.id !== job.id)]
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    actionLoadingKey.value = ''
  }
}

const odobKey = (odob?: DedaoOdob | null) => odob?.enid || String(odob?.class_id || odob?.id || '')
const safeProgress = (value?: number) => Math.max(0, Math.min(100, Number.isFinite(value) ? Number(value) : 0))
const downloadTypeFor = (odob: DedaoOdob | null) => (odob ? downloadTypes.value[odobKey(odob)] || 1 : 1)
const setDownloadType = (odob: DedaoOdob | null, event: Event) => {
  if (!odob) {
    return
  }
  const value = Number((event.target as HTMLSelectElement).value)
  downloadTypes.value = {
    ...downloadTypes.value,
    [odobKey(odob)]: value,
  }
}
const isOdobActionLoading = (odob: DedaoOdob | null) => Boolean(odob && actionLoadingKey.value === odobKey(odob))
const jobMatchesOdob = (job: BookKnowledgeJob, odob: DedaoOdob) => {
  const key = odobKey(odob)
  const resultOdobID = Number(job.result?.odob_id || 0)
  const resultOdobEnID = typeof job.result?.odob_enid === 'string' ? job.result.odob_enid : ''
  const resultAliasID = typeof job.result?.odob_alias_id === 'string' ? job.result.odob_alias_id : ''
  return (
    job.odob_id === odob.id ||
    resultOdobID === odob.id ||
    job.odob_enid === key ||
    resultOdobEnID === key ||
    job.odob_alias_id === odob.audio_alias_id ||
    resultAliasID === odob.audio_alias_id
  )
}
const jobTypeLabel = (job: BookKnowledgeJob) => {
  if (job.type === odobDownloadJobType) {
    return `下载 ${downloadTypeLabel(job.download_type || Number(job.result?.download_type || 1))}`
  }
  return job.type
}
const downloadTypeLabel = (value: number) => downloadTypeOptions.find((option) => option.value === value)?.label || 'MP3'
const formatDuration = (seconds?: number) => {
  const totalSeconds = Math.max(0, Math.round(Number(seconds || 0)))
  if (!totalSeconds) {
    return '-'
  }
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }
  return `${minutes || 1}m`
}
const formatJobTime = (value?: string) => {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>

<style scoped>
.odob-library {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: calc(100vh - 80px);
  margin-top: 8px;
}

.odob-toolbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 112px 82px;
  gap: 10px;
  align-items: center;
  border-bottom: 1px solid var(--dedao-line);
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

.odob-workspace {
  display: grid;
  grid-template-columns: 220px minmax(420px, 1fr) 340px;
  gap: 10px;
}

.odob-filter-panel,
.odob-list-panel,
.odob-study-panel {
  min-width: 0;
  border: 1px solid var(--dedao-line);
  border-radius: 10px;
  padding: 10px;
  background: #ffffff;
}

.odob-filter-panel {
  display: grid;
  align-content: start;
  gap: 8px;
}

.odob-filter-panel p {
  margin: 0;
  color: var(--dedao-muted);
  font-size: 12px;
}

.panel-head h2,
.study-title h2 {
  margin: 0;
  color: #111111;
  font-size: 18px;
  line-height: 24px;
}

.odob-list {
  display: grid;
  gap: 0;
  margin-top: 4px;
}

.odob-row {
  display: grid;
  grid-template-columns: 54px minmax(0, 1fr) 70px;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 78px;
  border-bottom: 1px solid var(--dedao-line);
  padding: 10px 0;
  background: #ffffff;
  cursor: pointer;
}

.odob-row.active {
  background: #fffaf6;
}

.cover-frame {
  display: grid;
  overflow: hidden;
  width: 54px;
  height: 54px;
  place-items: center;
  border: 1px solid var(--dedao-line);
  border-radius: 7px;
  background: var(--dedao-subtle);
  color: var(--dedao-muted);
  font-weight: 800;
}

.cover-frame img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.odob-main {
  min-width: 0;
}

.odob-title-line {
  display: flex;
  min-width: 0;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.odob-title-line strong,
.odob-title-line span,
.odob-main p {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.odob-title-line strong {
  color: #222222;
  font-size: 15px;
}

.odob-title-line span {
  flex: 0 0 auto;
  color: var(--dedao-orange);
  font-size: 12px;
  font-weight: 700;
}

.odob-main p {
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

.odob-side {
  display: grid;
  justify-items: end;
  gap: 4px;
  color: #222222;
  font-weight: 800;
}

.odob-side small {
  color: var(--dedao-muted);
  font-size: 11px;
  font-weight: 700;
}

.odob-action-bar {
  display: flex;
  grid-column: 2 / 4;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.study-title,
.job-status-head,
.job-status-item div,
.transcript-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.odob-study-panel > p {
  margin: 8px 0 0;
  color: var(--dedao-muted);
  font-size: 13px;
  line-height: 1.55;
}

.odob-player {
  width: 100%;
  margin-top: 12px;
}

.detail-action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.odob-action-bar select,
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

.odob-detail-list {
  display: grid;
  gap: 0;
  margin: 10px 0 0;
  border-top: 1px solid var(--dedao-line);
}

.odob-detail-list dt {
  color: var(--dedao-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.odob-detail-list dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  color: #111111;
  font-size: 13px;
  font-weight: 700;
}

.odob-detail-list div {
  border-bottom: 1px solid var(--dedao-line);
  padding: 8px 0;
}

.detail-block,
.transcript-block,
.odob-job-status {
  margin-top: 14px;
  border-top: 1px solid var(--dedao-line);
  padding-top: 12px;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.tag-row span {
  border: 1px solid #ffe0c7;
  border-radius: 999px;
  padding: 3px 8px;
  background: #fffaf6;
  color: var(--dedao-orange);
  font-size: 11px;
  font-weight: 800;
}

.detail-block p,
.detail-block li {
  color: var(--dedao-muted);
  font-size: 12px;
  line-height: 1.55;
}

.detail-block ul {
  display: grid;
  gap: 6px;
  margin: 8px 0 0;
  padding: 0;
  list-style: none;
}

.detail-block li strong {
  display: block;
  color: #222222;
}

.transcript-head strong {
  overflow: hidden;
  color: #222222;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

.transcript-markdown {
  overflow-wrap: anywhere;
  max-height: 360px;
  margin-top: 8px;
  overflow-y: auto;
  color: #333333;
  font-size: 13px;
  line-height: 1.7;
}

.transcript-markdown :deep(h1),
.transcript-markdown :deep(h2),
.transcript-markdown :deep(h3) {
  margin: 12px 0 6px;
  color: #111111;
  font-size: 16px;
  line-height: 22px;
}

.transcript-markdown :deep(p),
.transcript-markdown :deep(ul),
.transcript-markdown :deep(ol) {
  margin: 7px 0;
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

@media (max-width: 1180px) {
  .odob-workspace {
    grid-template-columns: 190px minmax(360px, 1fr) 300px;
  }
}

@media (max-width: 900px) {
  .odob-toolbar,
  .odob-workspace {
    grid-template-columns: 1fr;
  }
}
</style>
