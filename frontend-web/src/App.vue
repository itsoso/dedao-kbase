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

    <div class="workbench-grid">
      <aside class="book-rail">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Library</span>
            <h2>Books</h2>
          </div>
          <button type="button" @click="loadBooks">Refresh</button>
        </div>
        <input v-model="bookFilter" class="rail-filter" placeholder="Filter books" />
        <div class="book-list">
          <button
            v-for="book in filteredBooks"
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
      </aside>

      <section class="search-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Search</span>
            <h2>检索</h2>
          </div>
          <select v-model="searchScope">
            <option value="selected">Current Book</option>
            <option value="all">All Books</option>
          </select>
        </div>
        <div class="search-row">
          <input v-model="query" placeholder="输入关键词" @keydown.enter="runSearch" />
          <button class="primary-action" type="button" @click="runSearch">Search</button>
        </div>
        <div class="result-list">
          <article v-for="result in searchResults" :key="resultKey(result)" class="result-row">
            <div class="result-meta">
              <span>{{ result.kind }}</span>
              <span>{{ result.score.toFixed(2) }}</span>
            </div>
            <h3>{{ result.title || result.book_title || result.book_id }}</h3>
            <p>{{ result.snippet }}</p>
          </article>
          <div v-if="!searchResults.length" class="empty-state">No results</div>
        </div>
      </section>

      <section class="detail-panel">
        <div class="panel-head">
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
          <dl class="metric-grid">
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
import { computed, onMounted, ref } from 'vue'
import {
  KBaseClient,
  type BookKnowledgeBook,
  type BookKnowledgePackage,
  type BookKnowledgeSearchResult,
} from './api'

const storageKey = 'dedao-kbase-web-settings'
const tabs = ['Overview', 'Chapters', 'Claims', 'Chunks', 'System KB']

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const errorMessage = ref('')
const books = ref<BookKnowledgeBook[]>([])
const bookFilter = ref('')
const selectedBookID = ref('')
const selectedPackage = ref<BookKnowledgePackage | null>(null)
const query = ref('')
const searchScope = ref<'selected' | 'all'>('selected')
const searchResults = ref<BookKnowledgeSearchResult[]>([])
const activeTab = ref('Overview')
const systemKBPayload = ref<Record<string, unknown> | null>(null)

const client = computed(() => new KBaseClient(baseUrl.value, token.value))

const filteredBooks = computed(() => {
  const term = bookFilter.value.trim().toLowerCase()
  if (!term) {
    return books.value
  }
  return books.value.filter((book) => `${book.book_id} ${book.title}`.toLowerCase().includes(term))
})

const formattedSystemKB = computed(() => {
  return systemKBPayload.value ? JSON.stringify(systemKBPayload.value, null, 2) : 'No System KB payload loaded'
})

onMounted(() => {
  restoreConnection()
  if (token.value) {
    loadBooks()
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
    books.value = await client.value.listBooks()
    if (!selectedBookID.value && books.value.length) {
      await selectBook(books.value[0].book_id)
    }
  })
}

const selectBook = async (bookID: string) => {
  selectedBookID.value = bookID
  selectedPackage.value = await client.value.getBook(bookID)
  activeTab.value = 'Overview'
}

const runSearch = async () => {
  const text = query.value.trim()
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

const resultKey = (result: BookKnowledgeSearchResult) => {
  return `${result.kind}:${result.book_id}:${result.chunk_id || result.claim_id || result.title}`
}
</script>
