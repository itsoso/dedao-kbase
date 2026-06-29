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
          <button type="button" @click="openContextPanel('Projects')">项目知识</button>
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
              @click="setContextPanel(tab)"
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

        <div v-else-if="activeContextPanel === 'Projects'" class="project-hub">
          <div class="project-tabs">
            <button
              v-for="project in projects"
              :key="project.project_id"
              type="button"
              :class="{ active: selectedProjectID === project.project_id }"
              @click="selectProject(project.project_id)"
            >
              {{ project.project_id === 'health' ? '阿衡' : project.name }}
            </button>
          </div>

          <p v-if="projectError" class="job-error">{{ projectError }}</p>
          <div v-if="projectLoading" class="empty-state compact">Loading project knowledge...</div>

          <section v-if="projectHub.preview" class="project-summary">
            <div>
              <span>Export</span>
              <strong>{{ projectHub.preview.export_type }}</strong>
            </div>
            <div>
              <span>Books</span>
              <strong>{{ projectHub.preview.book_count }}</strong>
            </div>
            <div>
              <span>Claims</span>
              <strong>{{ projectHub.preview.claim_count }}</strong>
            </div>
          </section>

          <section v-if="projectHub.verification" class="verification-summary">
            <div>
              <span>Auto</span>
              <strong>{{ verificationTierCount('auto_usable') }}</strong>
            </div>
            <div>
              <span>Assist</span>
              <strong>{{ verificationTierCount('assistive_only') }}</strong>
            </div>
            <div>
              <span>Review</span>
              <strong>{{ verificationTierCount('needs_human') }}</strong>
            </div>
            <div>
              <span>Blocked</span>
              <strong>{{ verificationTierCount('blocked') }}</strong>
            </div>
          </section>

          <section v-if="selectedProject" class="project-policy">
            <div class="project-policy-head">
              <div>
                <strong>{{ selectedProject.name }}</strong>
                <p>{{ selectedProject.description }}</p>
              </div>
              <button
                type="button"
                class="primary-action"
                :disabled="projectLoading || projectCollectionLoading"
                @click="refreshProjectCollection"
              >
                {{ projectCollectionLoading ? '生成中' : '生成集合' }}
              </button>
            </div>
            <div class="source-chips">
              <span>{{ selectedProject.target_system }}</span>
              <span>{{ selectedProject.source_policy }}</span>
              <span>{{ selectedProject.requires_review ? 'requires review' : 'read only' }}</span>
              <span v-if="projectHub.verification">{{ projectHub.verification.human_loop }}</span>
              <span v-if="projectCollection">{{ projectCollection.source }}</span>
            </div>
          </section>

          <section v-if="projectCollection" class="project-summary project-collection-summary">
            <div>
              <span>Collection</span>
              <strong>{{ projectCollection.collection_id }}</strong>
            </div>
            <div>
              <span>Items</span>
              <strong>{{ projectCollection.item_count }}</strong>
            </div>
            <div>
              <span>Async Audit</span>
              <strong>{{ projectCollection.audit_count }}</strong>
            </div>
          </section>
          <div v-else-if="!projectLoading" class="empty-state compact">
            No persisted collection. Generate collection to materialize the latest verified project snapshot.
          </div>

          <div v-if="verificationReport" class="table-list project-verification-list">
            <article
              v-for="item in verificationReport.items || []"
              :key="`${item.project_id}:verify:${item.claim_id}`"
              class="table-row"
            >
              <div class="result-meta">
                <span>{{ riskTierLabel(item.risk_tier) }}</span>
                <span>{{ item.decision }} · {{ formatScore(item.verification_score) }}</span>
              </div>
              <strong>{{ item.title || item.book_title }}</strong>
              <p>{{ item.summary }}</p>
              <div class="source-chips">
                <span>verification_score: {{ formatScore(item.verification_score) }}</span>
                <span>risk_tier: {{ item.risk_tier }}</span>
                <span v-for="use in item.allowed_uses || []" :key="use">{{ use }}</span>
              </div>
              <p v-if="(item.failure_reasons || []).length" class="job-error">
                {{ (item.failure_reasons || []).join(', ') }}
              </p>
            </article>
          </div>

          <div v-if="projectAuditQueue" class="table-list project-audit-list">
            <article
              v-for="item in projectAuditQueue.audit_items || []"
              :key="item.audit_id"
              class="table-row"
            >
              <div class="result-meta">
                <span>{{ item.review_status || 'pending_async_audit' }}</span>
                <span>{{ item.sample_reason || 'async_audit' }}</span>
              </div>
              <strong>{{ item.title || item.book_title }}</strong>
              <p>{{ item.summary }}</p>
              <div class="source-chips">
                <span>{{ item.book_id }} · {{ item.claim_id }}</span>
                <span>source_hash: {{ item.source_hash }}</span>
                <span v-for="reason in item.failure_reasons || []" :key="reason">{{ reason }}</span>
              </div>
            </article>
            <div v-if="!projectLoading && !(projectAuditQueue.audit_items || []).length" class="empty-state compact">
              No pending_async_audit items
            </div>
          </div>

          <div class="table-list project-review-list">
            <article v-for="item in reviewQueue?.items || []" :key="`${item.project_id}:${item.claim_id}`" class="table-row">
              <div class="result-meta">
                <span>{{ item.review_status }}</span>
                <span>{{ item.book_id }} · {{ item.claim_id }}</span>
              </div>
              <strong>{{ item.title || item.book_title }}</strong>
              <p>{{ item.summary }}</p>
              <div class="source-chips">
                <span v-for="flag in item.risk_flags || []" :key="flag">{{ flag }}</span>
              </div>
            </article>
            <div v-if="!projectLoading && !(reviewQueue?.items || []).length" class="empty-state compact">
              No review queue items
            </div>
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
  type BookKnowledgeProject,
  type BookKnowledgeProjectAuditQueue,
  type BookKnowledgeProjectCollection,
  type BookKnowledgeProjectExportPreview,
  type BookKnowledgeProjectReviewQueue,
  type BookKnowledgeProjectVerificationReport,
  type BookKnowledgeSearchResult,
} from '../api'
import { renderMarkdown } from '../utils/markdownRender'

const storageKey = 'dedao-kbase-web-settings'
const layoutStorageKey = 'dedao-kbase-web-layout'
const tabs = ['Overview', 'Chapters', 'Claims', 'Chunks', 'Jobs', 'Projects', 'System KB', 'Skills/API', 'Ops']
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
  '/api/projects',
  '/api/projects/health/review-queue',
  '/api/projects/proofroom/export-preview',
  '/api/projects/health/verification-report',
  '/api/projects/health/collection/refresh',
  '/api/projects/health/audit-queue',
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
const selectedPromptCategory = ref('chat')
const chatQuestion = ref('')
const selectedChatModel = ref('qwen3.7-max')
const pendingChatRequests = ref(0)
const chatResponse = ref<BookKnowledgeChatResponse | null>(null)
const chatHistory = ref<BookKnowledgeChatHistoryItem[]>([])
const jobType = ref<JobActionValue>('notebooklm_export')
const jobs = ref<BookKnowledgeJob[]>([])
const jobsLoading = ref(false)
const jobError = ref('')
const projects = ref<BookKnowledgeProject[]>([])
const selectedProjectID = ref('health')
const reviewQueue = ref<BookKnowledgeProjectReviewQueue | null>(null)
const projectExportPreview = ref<BookKnowledgeProjectExportPreview | null>(null)
const verificationReport = ref<BookKnowledgeProjectVerificationReport | null>(null)
const projectCollection = ref<BookKnowledgeProjectCollection | null>(null)
const projectAuditQueue = ref<BookKnowledgeProjectAuditQueue | null>(null)
const projectLoading = ref(false)
const projectCollectionLoading = ref(false)
const projectError = ref('')
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

const selectedProject = computed(() => {
  return projects.value.find((project) => project.project_id === selectedProjectID.value) || reviewQueue.value?.project || null
})

const projectHub = computed(() => ({
  project: selectedProject.value,
  queue: reviewQueue.value,
  preview: projectExportPreview.value,
  verification: verificationReport.value,
  collection: projectCollection.value,
  auditQueue: projectAuditQueue.value,
}))

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

const loadProjectHub = async () => {
  if (!token.value || projectLoading.value) {
    return
  }
  const projectID = selectedProjectID.value || 'health'
  projectLoading.value = true
  projectError.value = ''
  try {
    if (!projects.value.length) {
      projects.value = await client.value.listProjects()
    }
    if (!selectedProjectID.value && projects.value.length) {
      selectedProjectID.value = projects.value[0].project_id
    }
    const [queue, preview, verification, collectionState] = await Promise.all([
      client.value.getProjectReviewQueue(projectID, 20),
      client.value.getProjectExportPreview(projectID, 20),
      client.value.getProjectVerificationReport(projectID, 20),
      loadProjectCollectionState(projectID),
    ])
    if (selectedProjectID.value === projectID) {
      reviewQueue.value = queue
      projectExportPreview.value = preview
      verificationReport.value = verification
      projectCollection.value = collectionState.collection
      projectAuditQueue.value = collectionState.auditQueue
    }
    connected.value = true
  } catch (error) {
    connected.value = false
    projectError.value = error instanceof Error ? error.message : String(error)
  } finally {
    projectLoading.value = false
  }
}

const selectProject = async (projectID: string) => {
  selectedProjectID.value = projectID
  reviewQueue.value = null
  projectExportPreview.value = null
  verificationReport.value = null
  projectCollection.value = null
  projectAuditQueue.value = null
  await loadProjectHub()
}

const loadProjectCollectionState = async (projectID: string) => {
  try {
    const [collection, auditQueue] = await Promise.all([
      client.value.getProjectCollection(projectID),
      client.value.getProjectAuditQueue(projectID, 20),
    ])
    return { collection, auditQueue }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    if (message.includes('HTTP 404')) {
      return { collection: null, auditQueue: null }
    }
    throw error
  }
}

const refreshProjectCollection = async () => {
  if (!token.value || projectCollectionLoading.value) {
    return
  }
  const projectID = selectedProjectID.value || 'health'
  projectCollectionLoading.value = true
  projectError.value = ''
  try {
    const collection = await client.value.refreshProjectCollection(projectID, 25)
    const auditQueue = await client.value.getProjectAuditQueue(projectID, 25)
    if (selectedProjectID.value === projectID) {
      projectCollection.value = collection
      projectAuditQueue.value = auditQueue
      verificationReport.value = await client.value.getProjectVerificationReport(projectID, 25)
    }
    connected.value = true
  } catch (error) {
    connected.value = false
    projectError.value = error instanceof Error ? error.message : String(error)
  } finally {
    projectCollectionLoading.value = false
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

const verificationTierCount = (riskTier: string) => {
  return verificationReport.value?.tier_counts?.[riskTier] || 0
}

const formatScore = (score?: number) => {
  if (typeof score !== 'number' || Number.isNaN(score)) {
    return '0%'
  }
  return `${Math.round(score * 100)}%`
}

const riskTierLabel = (riskTier: string) => {
  const labels: Record<string, string> = {
    auto_usable: 'Auto usable',
    assistive_only: 'Assistive',
    needs_human: 'Needs review',
    blocked: 'Blocked',
  }
  return labels[riskTier] || riskTier
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

const applyPrompt = (prompt: BookKnowledgePrompt) => {
  selectedPromptID.value = prompt.prompt_id
  selectedPromptCategory.value = prompt.category || 'chat'
  chatQuestion.value = prompt.prompt
}

const clearChatDraft = () => {
  selectedPromptID.value = ''
  chatQuestion.value = ''
  chatResponse.value = null
}

const resetBookStudyState = () => {
  selectedPromptID.value = ''
  selectedPromptCategory.value = 'chat'
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
  const requestMode = selectedPromptCategory.value || 'chat'
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
  selectedPromptCategory.value = item.mode || 'chat'
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

const setContextPanel = (panel: string) => {
  activeContextPanel.value = panel
  if (panel === 'Projects') {
    void loadProjectHub()
  }
}

const openContextPanel = (panel: string) => {
  if (activeContextPanel.value === panel) {
    activeContextPanel.value = ''
    return
  }
  setContextPanel(panel)
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
