<template>
  <main class="kbase-web-shell">
    <section class="connection-bar">
      <div class="brand-block">
        <span class="eyebrow">Dedao KBase</span>
        <h1>书籍知识库</h1>
      </div>
      <label>
        <span>Base URL</span>
        <input v-model="baseUrl" name="baseUrl" placeholder="http://127.0.0.1:8719" />
      </label>
      <label>
        <span>Token</span>
        <input v-model="token" name="token" type="password" placeholder="KBASE_AUTH_TOKEN" />
      </label>
      <button class="primary-action" type="button" :disabled="loading" @click="connectAndRefresh">
        {{ loading ? '刷新中' : '连接' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <div ref="workbenchRef" class="workbench-grid learning-layout" :style="workbenchStyle">
      <aside class="book-rail library-search-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Library Search</span>
            <h2>找书与检索</h2>
          </div>
          <button type="button" @click="loadBooks">Refresh</button>
        </div>

        <div class="rail-controls stacked">
          <input
            v-model="combinedSearchQuery"
            class="rail-filter"
            placeholder="搜索书名、作者、claims 或 chunks"
            @keydown.enter="runLibrarySearch"
          />
          <div class="rail-control-row">
            <select v-model="searchScope">
              <option value="selected">Current Book</option>
              <option value="all">All Books</option>
            </select>
            <select v-model="bookSort" @change="resetBookPageAndLoad">
              <option value="updated_at_desc">Updated</option>
              <option value="title_asc">Title A-Z</option>
            </select>
          </div>
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
          <div class="history-head">
            <strong>Search Results</strong>
            <span>{{ searchResults.length }}</span>
          </div>
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
            <span class="eyebrow">TokenPlan Study</span>
            <h2>{{ selectedPackage?.book.title || '选择一本书开始学习' }}</h2>
          </div>
          <select v-model="selectedChatModel" class="model-select">
            <option v-for="model in chatModelOptions" :key="model.value" :value="model.value">
              {{ model.label }}
            </option>
          </select>
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

        <select v-model="selectedPromptID" class="prompt-select" @change="applySelectedPrompt">
          <option value="">Prompt templates</option>
          <option v-for="prompt in promptTemplates" :key="prompt.prompt_id" :value="prompt.prompt_id">
            {{ prompt.category }} · {{ prompt.title }}
          </option>
        </select>

        <textarea
          v-model="chatQuestion"
          rows="7"
          placeholder="围绕当前书籍提问，或选择上方模板"
          :disabled="!selectedBookID || chatLoading"
          @keydown.meta.enter.prevent="sendChat"
        ></textarea>

        <div class="chat-actions">
          <button type="button" :disabled="!selectedBookID || chatLoading" @click="clearChatDraft">Clear</button>
          <button class="primary-action" type="button" :disabled="!selectedBookID || chatLoading" @click="sendChat">
            {{ chatLoading ? '生成中' : 'Send' }}
          </button>
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
            <button type="button" :disabled="!selectedBookID || chatLoading" @click="loadChatHistory">Reload</button>
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

      <div class="column-resizer right-resizer" role="separator" aria-label="Resize detail column" @pointerdown="beginColumnResize('right', $event)"></div>

      <section class="detail-panel compact-reference-panel">
        <div class="panel-head detail-head">
          <div>
            <span class="eyebrow">Details</span>
            <h2>{{ selectedPackage?.book.title || 'Book Details' }}</h2>
          </div>
          <div class="tab-strip">
            <button
              v-for="tab in tabs"
              :key="tab"
              type="button"
              :class="{ active: activeTab === tab }"
              @click="activeTab = tab"
            >
              {{ tab }}
            </button>
          </div>
        </div>

        <div v-if="activeTab === 'Overview'" class="detail-body">
          <dl class="compact-detail-summary">
            <div><dt>Chapters</dt><dd>{{ selectedPackage?.chapters.length || 0 }}</dd></div>
            <div><dt>Claims</dt><dd>{{ selectedPackage?.claims.length || 0 }}</dd></div>
            <div><dt>Chunks</dt><dd>{{ selectedPackage?.chunks.length || 0 }}</dd></div>
          </dl>
          <p class="source-path">{{ selectedPackage?.book.source_html || 'No source HTML path' }}</p>
        </div>

        <div v-else-if="activeTab === 'Chapters'" class="table-list">
          <article v-for="chapter in selectedPackage?.chapters || []" :key="chapter.chapter_id" class="table-row">
            <strong>{{ chapter.order }}. {{ chapter.title }}</strong>
            <p>{{ chapter.summary }}</p>
          </article>
        </div>

        <div v-else-if="activeTab === 'Claims'" class="table-list">
          <article v-for="claim in selectedPackage?.claims || []" :key="claim.claim_id" class="table-row">
            <div class="result-meta">
              <span>{{ claim.review_status || 'draft' }}</span>
              <span>{{ claim.evidence_level || 'D' }}</span>
            </div>
            <strong>{{ claim.title }}</strong>
            <p>{{ claim.summary }}</p>
          </article>
        </div>

        <div v-else-if="activeTab === 'Chunks'" class="table-list">
          <article v-for="chunk in selectedPackage?.chunks || []" :key="chunk.chunk_id" class="table-row">
            <div class="result-meta">
              <span>{{ chunk.chunk_id }}</span>
              <span>{{ chunk.tokens || 0 }} tokens</span>
            </div>
            <p>{{ chunk.text }}</p>
          </article>
        </div>

        <div v-else class="system-kb-panel">
          <div class="system-actions">
            <button type="button" @click="loadSystemKBManifest">Manifest</button>
            <button type="button" @click="loadSystemKBExport">Export</button>
          </div>
          <pre>{{ formattedSystemKB }}</pre>
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
  type BookKnowledgePackage,
  type BookKnowledgePrompt,
  type BookKnowledgeSearchResult,
} from './api'
import { renderMarkdown } from './utils/markdownRender'

const storageKey = 'dedao-kbase-web-settings'
const layoutStorageKey = 'dedao-kbase-web-layout'
const tabs = ['Overview', 'Chapters', 'Claims', 'Chunks', 'System KB']
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
const bookSort = ref('updated_at_desc')
const selectedBookID = ref('')
const selectedPackage = ref<BookKnowledgePackage | null>(null)
const searchScope = ref<'selected' | 'all'>('selected')
const searchResults = ref<BookKnowledgeSearchResult[]>([])
const activeTab = ref('Overview')
const systemKBPayload = ref<Record<string, unknown> | null>(null)
const promptTemplates = ref<BookKnowledgePrompt[]>([])
const selectedPromptID = ref('')
const chatMode = ref('chat')
const chatQuestion = ref('')
const selectedChatModel = ref('qwen3.7-max')
const chatLoading = ref(false)
const chatResponse = ref<BookKnowledgeChatResponse | null>(null)
const chatHistory = ref<BookKnowledgeChatHistoryItem[]>([])
const workbenchRef = ref<HTMLElement | null>(null)
const layoutColumns = ref({ left: 340, right: 320 })
const activeResizeTarget = ref<'left' | 'right' | null>(null)

const client = computed(() => new KBaseClient(baseUrl.value, token.value))

const workbenchStyle = computed(() => ({
  '--left-column': `${layoutColumns.value.left}px`,
  '--right-column': `${layoutColumns.value.right}px`,
}))

const formattedSystemKB = computed(() => {
  return systemKBPayload.value ? JSON.stringify(systemKBPayload.value, null, 2) : 'No System KB payload loaded'
})

const renderedChatAnswer = computed(() => {
  return renderMarkdown(chatResponse.value?.answer || '')
})

onMounted(async () => {
  restoreConnection()
  restoreLayoutColumns()
  await hydrateBrowserSession()
  if (token.value) {
    loadBooks()
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
    const parsed = JSON.parse(raw) as { left?: number; right?: number }
    layoutColumns.value = {
      left: clampNumber(parsed.left || layoutColumns.value.left, 280, 460),
      right: clampNumber(parsed.right || layoutColumns.value.right, 240, 520),
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

const connectAndRefresh = async () => {
  saveConnection()
  await loadBooks()
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
    const page = await client.value.listBooksPage(bookPage.value, bookPageSize.value, combinedSearchQuery.value, bookSort.value)
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
  selectedBookID.value = bookID
  await withRequest(async () => {
    selectedPackage.value = await client.value.getBook(bookID)
    activeTab.value = 'Overview'
    await loadBookPrompts()
    await loadChatHistory()
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
    const bookID = searchScope.value === 'selected' ? selectedBookID.value : ''
    searchResults.value = await client.value.searchKnowledge(text, bookID, 20)
  })
}

const loadSystemKBManifest = async () => {
  await withRequest(async () => {
    systemKBPayload.value = await client.value.getSystemKBManifest()
    activeTab.value = 'System KB'
  })
}

const loadSystemKBExport = async () => {
  await withRequest(async () => {
    systemKBPayload.value = await client.value.getSystemKBExport()
    activeTab.value = 'System KB'
  })
}

const loadBookPrompts = async () => {
  if (!selectedBookID.value) {
    promptTemplates.value = []
    return
  }
  promptTemplates.value = await client.value.getBookPrompts(selectedBookID.value)
}

const loadChatHistory = async () => {
  if (!selectedBookID.value) {
    chatHistory.value = []
    return
  }
  chatHistory.value = await client.value.getBookChatHistory(selectedBookID.value, 20)
}

const setChatMode = (mode: string) => {
  chatMode.value = mode
}

const applySelectedPrompt = () => {
  const prompt = promptTemplates.value.find((item) => item.prompt_id === selectedPromptID.value)
  if (!prompt) {
    return
  }
  chatMode.value = 'chat'
  chatQuestion.value = prompt.prompt
}

const clearChatDraft = () => {
  selectedPromptID.value = ''
  chatQuestion.value = ''
  chatResponse.value = null
}

const sendChat = async () => {
  if (!selectedBookID.value || chatLoading.value) {
    return
  }
  chatLoading.value = true
  errorMessage.value = ''
  try {
    chatResponse.value = await client.value.chatWithBook(selectedBookID.value, {
      mode: chatMode.value,
      question: chatQuestion.value,
      model: selectedChatModel.value,
    })
    await loadChatHistory()
    connected.value = true
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    chatLoading.value = false
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

const beginColumnResize = (target: 'left' | 'right', event: PointerEvent) => {
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
  if (target === 'left') {
    layoutColumns.value = {
      ...layoutColumns.value,
      left: clampNumber(event.clientX - rect.left, 280, 460),
    }
  } else {
    layoutColumns.value = {
      ...layoutColumns.value,
      right: clampNumber(rect.right - event.clientX, 240, 520),
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
