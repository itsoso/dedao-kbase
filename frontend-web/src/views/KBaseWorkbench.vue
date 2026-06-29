<template>
  <main class="kbase-web-shell">
    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <div ref="workbenchRef" class="workbench-grid learning-layout two-pane-layout" :style="workbenchStyle">
      <aside class="book-rail library-search-panel">
        <div class="rail-controls search-only">
          <input
            v-model="combinedSearchQuery"
            class="rail-filter"
            placeholder="搜索书名、作者、claims 或 chunks"
            @keydown.enter="runLibrarySearch"
          />
          <button class="primary-action" type="button" :disabled="loading" @click="runLibrarySearch">Search</button>
        </div>

        <div class="book-list">
          <button
            v-for="book in books"
            :key="book.book_id"
            type="button"
            class="book-row"
            :class="{ active: selectedBookID === book.book_id }"
            @click="selectBook(book.book_id)"
          >
            <strong>{{ book.title || book.book_id }}</strong>
            <span>{{ book.status || 'draft' }} · {{ book.extractor || 'unknown' }}</span>
          </button>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="bookPage <= 1 || loading" @click="changeBookPage(bookPage - 1)">Prev</button>
          <span>Page {{ bookPage }} / {{ bookTotalPages || 1 }} · {{ bookTotal }} books</span>
          <button type="button" :disabled="bookPage >= (bookTotalPages || 1) || loading" @click="changeBookPage(bookPage + 1)">Next</button>
          <select v-model.number="bookPageSize" @change="resetBookPageAndLoad">
            <option :value="20">20/page</option>
            <option :value="30">30/page</option>
            <option :value="50">50/page</option>
            <option :value="100">100/page</option>
          </select>
        </div>

        <div class="search-results-block">
          <div class="result-list rail-results">
            <article v-for="result in searchResults" :key="resultKey(result)" class="result-row">
              <div class="result-meta">
                <span>{{ result.kind }}</span>
                <span>{{ result.score.toFixed(2) }}</span>
              </div>
              <h3>{{ result.title || result.book_title || result.book_id }}</h3>
              <p>{{ result.snippet }}</p>
            </article>
            <div v-if="!searchResults.length" class="empty-state compact">No results</div>
          </div>
        </div>
      </aside>

      <div class="column-resizer left-resizer" role="separator" aria-label="Resize library column" @pointerdown="beginColumnResize('left', $event)"></div>

      <section class="chat-panel study-panel">
        <div class="panel-head study-head">
          <div>
            <h2>{{ selectedPackage?.book.title || '选择一本书开始学习' }}</h2>
          </div>
          <div class="study-actions">
            <select v-model="selectedChatModel" class="model-select">
              <option v-for="model in chatModelOptions" :key="model.value" :value="model.value">
                {{ model.label }}
              </option>
            </select>
            <button type="button" :disabled="!selectedPackage" @click="openContextPanel('Overview')">详情</button>
            <button type="button" @click="openContextPanel('Jobs')">任务</button>
          </div>
        </div>

        <div class="mode-strip">
          <button
            v-for="mode in chatModes"
            :key="mode.value"
            type="button"
            :class="{ active: chatMode === mode.value }"
            @click="setChatMode(mode.value)"
          >
            {{ mode.label }}
          </button>
        </div>

        <div class="prompt-chip-grid">
          <button
            v-for="prompt in promptTemplates"
            :key="prompt.prompt_id"
            type="button"
            :class="{ active: selectedPromptID === prompt.prompt_id }"
            @click="applyPrompt(prompt)"
          >
            {{ prompt.title }}
          </button>
        </div>

        <textarea
          v-model="chatQuestion"
          rows="7"
          placeholder="围绕当前书籍提问，或选择上方模板"
          :disabled="!selectedBookID"
          @keydown.meta.enter.prevent="sendChat"
        ></textarea>

        <div class="chat-actions">
          <button type="button" :disabled="!selectedBookID" @click="clearChatDraft">Clear</button>
          <button class="primary-action" type="button" :disabled="!canSendChat" @click="sendChat">
            {{ chatLoading ? `生成中 · ${pendingChatRequests}` : 'Send' }}
          </button>
        </div>

        <div class="utility-actions">
          <button type="button" @click="openContextPanel('Chapters')">章节</button>
          <button type="button" @click="openContextPanel('Claims')">Claims</button>
          <button type="button" @click="openContextPanel('Chunks')">Chunks</button>
          <button type="button" @click="openContextPanel('System KB')">System KB</button>
          <button type="button" @click="openContextPanel('Skills/API')">Skills/API</button>
          <button type="button" @click="openContextPanel('Ops')">Ops</button>
        </div>

        <article v-if="chatResponse" class="chat-answer">
          <div class="result-meta">
            <span>{{ chatResponse.mode }} · {{ chatResponse.model }}</span>
            <span>{{ chatResponse.context_stats.chars }} chars</span>
          </div>
          <div class="answer-markdown" v-html="renderedChatAnswer"></div>
          <div class="source-chips">
            <span v-for="source in chatResponse.sources" :key="`${source.kind}:${source.id}`">
              {{ source.kind }}: {{ source.title || source.id }}
            </span>
          </div>
        </article>

        <div class="chat-history">
          <div class="history-head">
            <strong>History</strong>
            <button type="button" :disabled="!selectedBookID" @click="loadChatHistory()">Reload</button>
          </div>
          <button
            v-for="item in chatHistory"
            :key="item.id"
            type="button"
            class="history-row"
            @click="restoreChatHistory(item)"
          >
            <span>{{ item.mode }} · {{ item.created_at }}</span>
            <strong>{{ item.question || item.mode }}</strong>
          </button>
          <div v-if="!chatHistory.length" class="empty-state compact">No history</div>
        </div>
      </section>

      <section v-if="activeContextPanel" class="context-drawer detail-panel">
        <div class="panel-head detail-head">
          <div>
            <h2>{{ selectedPackage?.book.title || 'Book Details' }}</h2>
          </div>
          <div class="drawer-actions">
            <button type="button" @click="activeContextPanel = ''">关闭</button>
          </div>
          <div class="tab-strip">
            <button
              v-for="tab in tabs"
              :key="tab"
              type="button"
              :class="{ active: activeContextPanel === tab }"
              @click="activeContextPanel = tab"
            >
              {{ tab }}
            </button>
          </div>
        </div>

        <div v-if="activeContextPanel === 'Overview'" class="detail-body">
          <dl class="compact-detail-summary">
            <div><dt>Chapters</dt><dd>{{ selectedPackage?.chapters.length || 0 }}</dd></div>
            <div><dt>Claims</dt><dd>{{ selectedPackage?.claims.length || 0 }}</dd></div>
            <div><dt>Chunks</dt><dd>{{ selectedPackage?.chunks.length || 0 }}</dd></div>
          </dl>
          <p class="source-path">{{ selectedPackage?.book.source_html || 'No source HTML path' }}</p>
        </div>

        <div v-else-if="activeContextPanel === 'Chapters'" class="table-list">
          <article v-for="chapter in selectedPackage?.chapters || []" :key="chapter.chapter_id" class="table-row">
            <strong>{{ chapter.order }}. {{ chapter.title }}</strong>
            <p>{{ chapter.summary }}</p>
          </article>
        </div>

        <div v-else-if="activeContextPanel === 'Claims'" class="table-list">
          <article v-for="claim in selectedPackage?.claims || []" :key="claim.claim_id" class="table-row">
            <div class="result-meta">
              <span>{{ claim.review_status || 'draft' }}</span>
              <span>{{ claim.evidence_level || 'D' }}</span>
            </div>
            <strong>{{ claim.title }}</strong>
            <p>{{ claim.summary }}</p>
          </article>
        </div>

        <div v-else-if="activeContextPanel === 'Chunks'" class="table-list">
          <article v-for="chunk in selectedPackage?.chunks || []" :key="chunk.chunk_id" class="table-row">
            <div class="result-meta">
              <span>{{ chunk.chunk_id }}</span>
              <span>{{ chunk.tokens || 0 }} tokens</span>
            </div>
            <p>{{ chunk.text }}</p>
          </article>
        </div>

        <div v-else-if="activeContextPanel === 'Jobs'" class="jobs-panel">
          <div class="job-create-row">
            <select v-model="jobType" :disabled="jobsLoading">
              <option v-for="action in jobActions" :key="action.value" :value="action.value">
                {{ action.label }}
              </option>
            </select>
            <button type="button" class="primary-action" :disabled="!selectedBookID || jobsLoading" @click="createSelectedBookJob">
              {{ jobsLoading ? 'Running' : 'Create Job' }}
            </button>
          </div>
          <p class="job-helper">
            {{ selectedJobAction?.description || '为当前书籍创建线上处理任务' }}
          </p>
          <p v-if="jobError" class="job-error">{{ jobError }}</p>
          <div class="job-list">
            <article v-for="job in jobs" :key="job.id" class="job-row" :class="job.status">
              <div class="result-meta">
                <span>{{ job.type }}</span>
                <span>{{ jobStatusLabel(job.status) }}</span>
              </div>
              <strong>{{ job.book_id || 'system' }}{{ job.target ? ` · ${job.target}` : '' }}</strong>
              <p>{{ formatJobTime(job.updated_at || job.created_at) }}</p>
              <p v-if="job.error" class="job-error">{{ job.error }}</p>
              <pre v-if="job.result" class="job-result">{{ jobResultSummary(job) }}</pre>
            </article>
            <div v-if="!jobs.length" class="empty-state compact">No jobs</div>
          </div>
        </div>

        <div v-else-if="activeContextPanel === 'System KB'" class="system-kb-panel">
          <div class="system-actions">
            <button type="button" @click="loadSystemKBManifest">Manifest</button>
            <button type="button" @click="loadSystemKBExport">Export</button>
          </div>
          <pre>{{ formattedSystemKB }}</pre>
        </div>

        <div v-else-if="activeContextPanel === 'Skills/API'" class="interop-panel">
          <div class="endpoint-group">
            <span class="eyebrow">Public Discovery</span>
            <a v-for="route in publicDiscoveryRoutes" :key="route" :href="routeUrl(route)" target="_blank" rel="noreferrer">
              {{ route }}
            </a>
          </div>
          <div class="endpoint-group">
            <span class="eyebrow">Bearer API</span>
            <code v-for="route in protectedApiRoutes" :key="route">{{ route }}</code>
          </div>
        </div>

        <div v-else class="ops-panel">
          <dl class="ops-grid">
            <div><dt>Service</dt><dd>{{ connected ? 'online' : 'offline' }}</dd></div>
            <div><dt>Token</dt><dd>{{ token ? 'present' : 'missing' }}</dd></div>
            <div><dt>Books</dt><dd>{{ bookTotal }}</dd></div>
            <div><dt>Jobs</dt><dd>{{ jobs.length }}</dd></div>
          </dl>
          <div class="endpoint-group">
            <span class="eyebrow">Health</span>
            <a :href="routeUrl('/health')" target="_blank" rel="noreferrer">/health</a>
          </div>
          <div class="endpoint-group">
            <span class="eyebrow">Current Base</span>
            <code>{{ serviceBaseUrl }}</code>
          </div>
        </div>
      </section>
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  getBrowserSession,
  KBaseClient,
  type BookKnowledgeBook,
  type BookKnowledgeChatHistoryItem,
  type BookKnowledgeChatResponse,
  type BookKnowledgeJob,
  type BookKnowledgeJobRequest,
  type BookKnowledgePackage,
  type BookKnowledgePrompt,
  type BookKnowledgeSearchResult,
} from '../api'
import { renderMarkdown } from '../utils/markdownRender'

const storageKey = 'dedao-kbase-web-settings'
const layoutStorageKey = 'dedao-kbase-web-layout'
const tabs = ['Overview', 'Chapters', 'Claims', 'Chunks', 'Jobs', 'System KB', 'Skills/API', 'Ops']
const chatModes = [
  { value: 'chat', label: '问答' },
  { value: 'summary', label: '总结' },
  { value: 'analysis', label: '分析' },
  { value: 'actions', label: '行动' },
  { value: 'rules', label: '规则' },
]
const chatModelOptions = [
  { value: 'qwen3.7-max', label: 'Qwen-3.7-Max' },
  { value: 'MiniMax-M2.5', label: 'MiniMax-M2.5' },
  { value: 'qwen-max', label: 'Qwen-Max' },
  { value: 'deepseek-v3', label: 'DeepSeek-V3' },
]

type JobActionValue = 'notebooklm_export' | 'health_system_kb_v2' | 'quant_rule_cards'

const jobActions: Array<{ value: JobActionValue; label: string; description: string }> = [
  { value: 'notebooklm_export', label: 'NotebookLM', description: '导出当前书籍的 NotebookLM 学习资料包' },
  { value: 'health_system_kb_v2', label: 'Health KB', description: '生成 health_system_kb_v2 draft 供下游审核' },
  { value: 'quant_rule_cards', label: 'Quant Rules', description: '生成 paper-only 量化规则卡 draft' },
]

const publicDiscoveryRoutes = [
  '/.well-known/dedao-kbase-skills.json',
  '/api/skills',
  '/api/skills/dedao.book.search/manifest.json',
  '/api/skills/dedao.book.search/openapi.json',
  '/api/skills/dedao.book.search/SKILL.md',
]

const protectedApiRoutes = [
  '/api/books',
  '/api/search',
  '/api/jobs',
  '/api/system-kb/manifest',
  '/api/system-kb/export',
]

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const errorMessage = ref('')
const books = ref<BookKnowledgeBook[]>([])
const combinedSearchQuery = ref('')
const bookPage = ref(1)
const bookPageSize = ref(30)
const bookTotal = ref(0)
const bookTotalPages = ref(0)
const selectedBookID = ref('')
const selectedPackage = ref<BookKnowledgePackage | null>(null)
const searchResults = ref<BookKnowledgeSearchResult[]>([])
const activeContextPanel = ref('')
const systemKBPayload = ref<Record<string, unknown> | null>(null)
const promptTemplates = ref<BookKnowledgePrompt[]>([])
const selectedPromptID = ref('')
const chatMode = ref('chat')
const chatQuestion = ref('')
const selectedChatModel = ref('qwen3.7-max')
const pendingChatRequests = ref(0)
const chatResponse = ref<BookKnowledgeChatResponse | null>(null)
const chatHistory = ref<BookKnowledgeChatHistoryItem[]>([])
const jobType = ref<JobActionValue>('notebooklm_export')
const jobs = ref<BookKnowledgeJob[]>([])
const jobsLoading = ref(false)
const jobError = ref('')
const workbenchRef = ref<HTMLElement | null>(null)
const layoutColumns = ref({ left: 320 })
const activeResizeTarget = ref<'left' | null>(null)

const client = computed(() => new KBaseClient(baseUrl.value, token.value))

const workbenchStyle = computed(() => ({
  '--left-column': `${layoutColumns.value.left}px`,
}))

const serviceBaseUrl = computed(() => {
  return (baseUrl.value || window.location.origin).replace(/\/+$/, '')
})

const formattedSystemKB = computed(() => {
  return systemKBPayload.value ? JSON.stringify(systemKBPayload.value, null, 2) : 'No System KB payload loaded'
})

const renderedChatAnswer = computed(() => {
  return renderMarkdown(chatResponse.value?.answer || '')
})

const chatLoading = computed(() => pendingChatRequests.value > 0)

const canSendChat = computed(() => Boolean(selectedBookID.value && chatQuestion.value.trim()))

const selectedJobAction = computed(() => {
  return jobActions.find((action) => action.value === jobType.value)
})

onMounted(async () => {
  restoreConnection()
  restoreLayoutColumns()
  await hydrateBrowserSession()
  if (token.value) {
    await loadBooks()
    await loadJobs()
  }
})

onBeforeUnmount(() => {
  stopColumnResize()
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

const restoreLayoutColumns = () => {
  const raw = localStorage.getItem(layoutStorageKey)
  if (!raw) {
    return
  }
  try {
    const parsed = JSON.parse(raw) as { left?: number }
    layoutColumns.value = {
      left: clampNumber(parsed.left || layoutColumns.value.left, 260, 420),
    }
  } catch {
    localStorage.removeItem(layoutStorageKey)
  }
}

const saveLayoutColumns = () => {
  localStorage.setItem(layoutStorageKey, JSON.stringify(layoutColumns.value))
}

const hydrateBrowserSession = async () => {
  try {
    const session = await getBrowserSession()
    if (session?.token) {
      token.value = session.token
      baseUrl.value = window.location.origin
      saveConnection()
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
}

const withRequest = async (operation: () => Promise<void>) => {
  loading.value = true
  errorMessage.value = ''
  try {
    await operation()
    connected.value = true
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const loadBooks = async () => {
  await withRequest(async () => {
    const page = await client.value.listBooksPage(bookPage.value, bookPageSize.value, combinedSearchQuery.value, 'updated_at_desc')
    books.value = page.books || []
    bookPage.value = page.page || 1
    bookPageSize.value = page.page_size || bookPageSize.value
    bookTotal.value = page.total || 0
    bookTotalPages.value = page.total_pages || 0
    if (!selectedBookID.value && books.value.length) {
      await selectBook(books.value[0].book_id)
    } else if (selectedBookID.value && !books.value.some((book) => book.book_id === selectedBookID.value) && books.value.length) {
      await selectBook(books.value[0].book_id)
    }
  })
}

const resetBookPageAndLoad = async () => {
  bookPage.value = 1
  await loadBooks()
}

const changeBookPage = async (page: number) => {
  const maxPage = bookTotalPages.value || 1
  bookPage.value = Math.min(Math.max(page, 1), maxPage)
  await loadBooks()
}

const selectBook = async (bookID: string) => {
  const switchingBook = selectedBookID.value !== bookID
  selectedBookID.value = bookID
  if (switchingBook) {
    resetBookStudyState()
  }
  await withRequest(async () => {
    const pkg = await client.value.getBook(bookID)
    if (selectedBookID.value !== bookID) {
      return
    }
    selectedPackage.value = pkg
    await loadBookPrompts(bookID)
    await loadChatHistory(bookID)
  })
}

const runLibrarySearch = async () => {
  const text = combinedSearchQuery.value.trim()
  bookPage.value = 1
  await loadBooks()
  if (!text) {
    searchResults.value = []
    return
  }
  await withRequest(async () => {
    searchResults.value = await client.value.searchKnowledge(text, '', 20)
  })
}

const loadSystemKBManifest = async () => {
  await withRequest(async () => {
    systemKBPayload.value = await client.value.getSystemKBManifest()
    activeContextPanel.value = 'System KB'
  })
}

const loadSystemKBExport = async () => {
  await withRequest(async () => {
    systemKBPayload.value = await client.value.getSystemKBExport()
    activeContextPanel.value = 'System KB'
  })
}

const loadBookPrompts = async (bookID = selectedBookID.value) => {
  if (!bookID) {
    promptTemplates.value = []
    return
  }
  const prompts = await client.value.getBookPrompts(bookID)
  if (selectedBookID.value === bookID) {
    promptTemplates.value = prompts
  }
}

const loadChatHistory = async (bookID = selectedBookID.value) => {
  if (!bookID) {
    chatHistory.value = []
    return
  }
  const history = await client.value.getBookChatHistory(bookID, 20)
  if (selectedBookID.value === bookID) {
    chatHistory.value = history
  }
}

const loadJobs = async () => {
  if (!token.value) {
    jobs.value = []
    return
  }
  jobsLoading.value = true
  jobError.value = ''
  try {
    jobs.value = await client.value.listJobs(30)
    connected.value = true
  } catch (error) {
    connected.value = false
    jobError.value = error instanceof Error ? error.message : String(error)
  } finally {
    jobsLoading.value = false
  }
}

const createSelectedBookJob = async () => {
  if (!selectedBookID.value || jobsLoading.value) {
    return
  }
  jobsLoading.value = true
  jobError.value = ''
  errorMessage.value = ''
  try {
    const job = await client.value.createJob(buildJobRequest())
    upsertJob(job)
    activeContextPanel.value = 'Jobs'
    const finalJob = await waitForJob(job.id)
    if (finalJob?.status === 'failed') {
      jobError.value = finalJob.error || 'Job failed'
    }
    connected.value = true
  } catch (error) {
    connected.value = false
    jobError.value = error instanceof Error ? error.message : String(error)
  } finally {
    jobsLoading.value = false
  }
}

const buildJobRequest = (): BookKnowledgeJobRequest => {
  if (jobType.value === 'notebooklm_export') {
    return { type: 'notebooklm_export', book_id: selectedBookID.value }
  }
  return { type: 'book_export', book_id: selectedBookID.value, target: jobType.value }
}

const waitForJob = async (jobID: string): Promise<BookKnowledgeJob | null> => {
  for (let attempt = 0; attempt < 24; attempt += 1) {
    await sleep(650)
    const job = await client.value.getJob(jobID)
    upsertJob(job)
    if (job.status === 'succeeded' || job.status === 'failed') {
      return job
    }
  }
  return null
}

const upsertJob = (job: BookKnowledgeJob) => {
  jobs.value = [job, ...jobs.value.filter((item) => item.id !== job.id)]
}

const sleep = (ms: number) => {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

const jobStatusLabel = (status: string) => {
  const labels: Record<string, string> = {
    queued: 'Queued',
    running: 'Running',
    succeeded: 'Succeeded',
    failed: 'Failed',
  }
  return labels[status] || status
}

const jobResultSummary = (job: BookKnowledgeJob) => {
  return job.result ? JSON.stringify(job.result, null, 2) : ''
}

const formatJobTime = (value?: string) => {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

const routeUrl = (path: string) => {
  return `${serviceBaseUrl.value}${path}`
}

const setChatMode = (mode: string) => {
  chatMode.value = mode
}

const applyPrompt = (prompt: BookKnowledgePrompt) => {
  selectedPromptID.value = prompt.prompt_id
  chatMode.value = 'chat'
  chatQuestion.value = prompt.prompt
}

const clearChatDraft = () => {
  selectedPromptID.value = ''
  chatQuestion.value = ''
  chatResponse.value = null
}

const resetBookStudyState = () => {
  selectedPromptID.value = ''
  chatQuestion.value = ''
  chatResponse.value = null
  chatHistory.value = []
  systemKBPayload.value = null
  jobError.value = ''
  activeContextPanel.value = ''
  selectedChatModel.value = 'qwen3.7-max'
}

const sendChat = async () => {
  const requestBookID = selectedBookID.value
  const requestQuestion = chatQuestion.value.trim()
  const requestMode = chatMode.value
  const requestModel = selectedChatModel.value
  if (!requestBookID || !requestQuestion) {
    return
  }
  pendingChatRequests.value += 1
  errorMessage.value = ''
  try {
    const response = await client.value.chatWithBook(requestBookID, {
      mode: requestMode,
      question: requestQuestion,
      model: requestModel,
    })
    if (selectedBookID.value === requestBookID) {
      chatResponse.value = response
      await loadChatHistory(requestBookID)
    }
    connected.value = true
  } catch (error) {
    if (selectedBookID.value === requestBookID) {
      connected.value = false
      errorMessage.value = error instanceof Error ? error.message : String(error)
    }
  } finally {
    pendingChatRequests.value = Math.max(0, pendingChatRequests.value - 1)
  }
}

const restoreChatHistory = (item: BookKnowledgeChatHistoryItem) => {
  chatMode.value = item.mode
  chatQuestion.value = item.question
  selectedChatModel.value = item.model || selectedChatModel.value
  chatResponse.value = {
    history_id: item.id,
    answer: item.answer,
    model: item.model,
    mode: item.mode,
    sources: item.sources || [],
    context_stats: item.context_stats,
    created_at: item.created_at,
  }
}

const openContextPanel = (panel: string) => {
  activeContextPanel.value = activeContextPanel.value === panel ? '' : panel
}

const beginColumnResize = (target: 'left', event: PointerEvent) => {
  activeResizeTarget.value = target
  event.preventDefault()
  window.addEventListener('pointermove', resizeColumn)
  window.addEventListener('pointerup', stopColumnResize)
}

const resizeColumn = (event: PointerEvent) => {
  const target = activeResizeTarget.value
  const rect = workbenchRef.value?.getBoundingClientRect()
  if (!target || !rect) {
    return
  }
  if (target) {
    layoutColumns.value = {
      ...layoutColumns.value,
      left: clampNumber(event.clientX - rect.left, 260, 420),
    }
  }
}

const stopColumnResize = () => {
  if (activeResizeTarget.value) {
    saveLayoutColumns()
  }
  activeResizeTarget.value = null
  window.removeEventListener('pointermove', resizeColumn)
  window.removeEventListener('pointerup', stopColumnResize)
}

const clampNumber = (value: number, min: number, max: number) => {
  return Math.min(Math.max(value, min), max)
}

const resultKey = (result: BookKnowledgeSearchResult) => {
  return `${result.kind}:${result.book_id}:${result.chunk_id || result.claim_id || result.title}`
}
</script>
