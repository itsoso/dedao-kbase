<template>
  <main ref="readerRootRef" class="ebook-detail-reader" :class="{ 'reader-fullscreen': readerFullscreen }">
    <header class="dedao-reader-toolbar">
      <div class="reader-tool-cluster left-tools">
        <RouterLink class="reader-tool" to="/ebook">
          <span class="tool-icon">←</span>
          <small>返回</small>
        </RouterLink>
        <button class="reader-tool" type="button" :class="{ active: settingsOpen }" @click="settingsOpen = !settingsOpen">
          <span class="tool-icon">Aa</span>
          <small>设置</small>
        </button>
        <button
          v-for="mode in columnModes"
          :key="mode.value"
          class="reader-tool"
          type="button"
          :class="{ active: columnMode === mode.value }"
          @click="setColumnMode(mode.value)"
        >
          <span class="column-icon" :class="`mode-${mode.value}`">
            <i v-for="part in mode.value" :key="part"></i>
          </span>
          <small>{{ mode.label }}</small>
        </button>
      </div>

      <div class="reader-title-center">
        <strong>{{ selectedChapterTitle || detail?.title || '电子书阅读' }}</strong>
        <span>{{ detail?.classify_name || detail?.operating_title || detail?.title || '电子书' }}</span>
      </div>

      <div class="reader-tool-cluster right-tools">
        <button class="reader-tool" type="button" :class="{ active: catalogOpen }" @click="catalogOpen = !catalogOpen">
          <span class="tool-icon">☰</span>
          <small>目录</small>
        </button>
        <button class="reader-tool" type="button" :class="{ active: searchOpen }" @click="searchOpen = !searchOpen">
          <span class="tool-icon">⌕</span>
          <small>书内搜索</small>
        </button>
        <button class="reader-tool" type="button" :disabled="loading" @click="loadDetail">
          <span class="tool-icon">↻</span>
          <small>同步</small>
        </button>
        <button
          class="reader-tool"
          type="button"
          :disabled="loading || shelfAdding || !enid || detail?.is_on_bookshelf"
          @click="addCurrentEbookToBookshelf"
        >
          <span class="tool-icon">⊞</span>
          <small>{{ shelfAdding ? '加入中' : detail?.is_on_bookshelf ? '已在书架' : '加入书架' }}</small>
        </button>
        <button class="reader-tool" type="button" :class="{ active: readerFullscreen }" @click="toggleFullscreen">
          <span class="tool-icon">✣</span>
          <small>{{ readerFullscreen ? '退出全屏' : '全屏' }}</small>
        </button>
      </div>
    </header>

    <section v-if="settingsOpen" class="reader-settings-strip">
      <label>
        <span>字号</span>
        <input v-model.number="readerScale" type="range" min="0.86" max="1.18" step="0.04" @change="saveCurrentReadingProgress" />
      </label>
      <label>
        <span>每次加载</span>
        <select v-model.number="pageLoadCount" @change="reloadCurrentChapter">
          <option v-for="count in pageLoadCountOptions" :key="count" :value="count">{{ count }} 页</option>
        </select>
      </label>
      <span class="reader-state" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</span>
    </section>

    <section v-if="searchOpen" class="reader-search-strip">
      <input v-model="readerSearchQuery" placeholder="搜索当前已加载页面文本" />
      <span>{{ searchMatchCount }} matches</span>
    </section>

    <section v-if="errorMessage" class="error-strip reader-error">{{ errorMessage }}</section>

    <section
      class="dedao-reader-stage"
      :class="`columns-${columnMode}`"
      :style="{ '--reader-scale': String(readerScale) }"
      @wheel="handleReaderWheel"
    >
      <aside v-if="catalogOpen" class="reader-drawer catalog-drawer">
        <div class="drawer-head">
          <strong>目录</strong>
          <span>{{ detail?.catalog.length || 0 }}</span>
        </div>
        <div class="catalog-list">
          <button
            v-for="item in detail?.catalog || []"
            :key="catalogKey(item)"
            type="button"
            class="catalog-row"
            :class="{ active: selectedChapterID === item.chapter_id }"
            :style="{ paddingLeft: `${12 + Math.max(0, item.level - 1) * 14}px` }"
            :disabled="!item.chapter_id"
            @click="openChapter(item)"
          >
            <span>{{ item.play_order || '-' }}</span>
            <strong>{{ item.text || item.chapter_id || '未命名章节' }}</strong>
          </button>
        </div>
        <div v-if="!loading && !(detail?.catalog.length)" class="empty-state">暂无目录</div>
      </aside>

      <article class="dedao-page-spread">
        <div v-if="pageError" class="error-strip">{{ pageError }}</div>
        <div v-if="pageLoading" class="empty-state reader-loading">加载页面中...</div>
        <div v-else-if="svgFrames.length" class="ebook-pages">
          <section v-for="frame in visibleFrames" :key="frame.key" class="ebook-page-shell">
            <iframe
              :class="['ebook-page-frame', { 'ebook-page-frame-passive': frame.passive }]"
              title="ebook page"
              :sandbox="frame.sandbox"
              :srcdoc="frame.srcdoc"
            ></iframe>
            <footer>{{ frame.pageNum }} / {{ totalPageCount || '?' }}</footer>
          </section>
        </div>
        <div v-else class="empty-state reader-loading">从目录选择章节开始阅读</div>
      </article>

      <aside v-if="analysisOpen" class="reader-drawer analysis-drawer">
        <div class="drawer-head">
          <strong>AI 学习</strong>
          <button type="button" @click="analysisOpen = false">关闭</button>
        </div>
        <section class="ebook-context-summary">
          <h2>{{ detail?.title || '电子书详情' }}</h2>
          <p>{{ detail?.book_intro || detail?.author_info || '暂无简介' }}</p>
          <dl>
            <div>
              <dt>Author</dt>
              <dd>{{ detail?.book_author || detail?.author_list?.join(' / ') || '-' }}</dd>
            </div>
            <div>
              <dt>Press</dt>
              <dd>{{ detail?.press_name || '-' }}</dd>
            </div>
            <div>
              <dt>Class</dt>
              <dd>{{ detail?.classify_name || '-' }}</dd>
            </div>
          </dl>
        </section>
        <PageAnalysisPanel
          :base-url="baseUrl"
          :token="token"
          source="ebook"
          :page-title="detail?.title || '电子书页面'"
          :page-url="ebookPageURL"
          :context-sections="ebookAnalysisSections"
          :quick-prompts="ebookQuickPrompts"
          default-question="分析当前电子书页面的重点、难点和建议学习路径。"
        />
      </aside>
    </section>

    <footer v-if="pageResponse" class="reader-bottom-bar">
      <button type="button" :disabled="pageLoading || !canGoPrevious" @click="loadRelativePage(-1)">上一页</button>
      <button type="button" :disabled="pageLoading || !canGoNext" @click="loadRelativePage(1)">下一页</button>
    </footer>

    <div class="reader-floating-actions">
      <button type="button" disabled>听</button>
      <button type="button" :class="{ active: analysisOpen }" @click="analysisOpen = !analysisOpen">Ai</button>
    </div>

    <button class="reader-help" type="button" disabled>?</button>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  getBrowserSession,
  KBaseClient,
  type DedaoEbook,
  type DedaoEbookCatalogItem,
  type DedaoEbookChapterPages,
  type DedaoEbookDetail,
  type PageAnalysisSection,
} from '../api'
import PageAnalysisPanel from '../components/PageAnalysisPanel.vue'

const storageKey = 'dedao-kbase-web-settings'
const ebookReaderProgressStorageKey = 'dedao-kbase-web-ebook-progress'
const route = useRoute()
type ColumnMode = 1 | 2 | 3
type SVGContentBox = { minX: number; minY: number; maxX: number; maxY: number }
type EbookReaderProgress = {
  chapterID: string
  chapterTitle?: string
  pageIndex: number
  columnMode?: ColumnMode
  pageLoadCount?: number
  readerScale?: number
  updatedAt: string
}
type EbookReaderProgressMap = Record<string, EbookReaderProgress>

const columnModes: Array<{ value: ColumnMode; label: string }> = [
  { value: 1, label: '单栏' },
  { value: 2, label: '双栏' },
  { value: 3, label: '三栏' },
]
const defaultColumnMode: ColumnMode = 1
const defaultPageLoadCount = 8
const pageLoadCountOptions = [1, 2, 4, 6, 8]

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const pageLoading = ref(false)
const shelfAdding = ref(false)
const errorMessage = ref('')
const pageError = ref('')
const detail = ref<DedaoEbookDetail | null>(null)
const selectedChapterID = ref('')
const selectedChapterTitle = ref('')
const pageResponse = ref<DedaoEbookChapterPages | null>(null)
const catalogOpen = ref(false)
const settingsOpen = ref(false)
const searchOpen = ref(false)
const analysisOpen = ref(false)
const readerSearchQuery = ref('')
const columnMode = ref<ColumnMode>(defaultColumnMode)
const readerScale = ref(1)
const pageLoadCount = ref(defaultPageLoadCount)
const readerRootRef = ref<HTMLElement | null>(null)
const readerFullscreen = ref(false)
const autoAdvanceRunning = ref(false)
const autoAdvanceCooldownUntil = ref(0)
const wheelPagingThreshold = 180
const wheelPagingDelta = ref(0)

const enid = computed(() => String(route.params.enid || ''))
const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const ebookPageURL = computed(() => `/ebook/${encodeURIComponent(enid.value)}`)
const readableCatalogItems = computed(() => (detail.value?.catalog || []).filter((item) => Boolean(item.chapter_id)))
const dedaoCopyrightResourceName = 'Copyright.xhtml'
const nonContentCatalogIDKeywords = [
  dedaoCopyrightResourceName.toLowerCase(),
  'copyright',
  'cover',
  'toc',
  'contents',
  'titlepage',
  'nav',
]
const nonContentCatalogTextPatterns = [/^版权信息$/, /^版权$/, /^版权页$/, /^封面$/, /^目录$/, /^目\s*录$/]
const currentReadableIndex = computed(() =>
  readableCatalogItems.value.findIndex((item) => item.chapter_id === selectedChapterID.value),
)
const canGoPrevious = computed(() => {
  if (!pageResponse.value) {
    return false
  }
  return pageResponse.value.index > 0 || Boolean(adjacentReadableCatalogItem(-1))
})
const canGoNext = computed(() => {
  if (!pageResponse.value) {
    return false
  }
  return !pageResponse.value.is_end || Boolean(adjacentReadableCatalogItem(1))
})
const svgFrames = computed(() =>
  (pageResponse.value?.pages || []).map((page) => {
    const frame = svgToFrame(page.svg)
    return {
      key: `${page.page_num}-${page.begin_offset}-${page.end_offset}`,
      pageNum: page.page_num,
      ...frame,
    }
  }),
)
const visibleFrames = computed(() => svgFrames.value)
const totalPageCount = computed(() => estimateEbookTotalPageCount())
const searchMatchCount = computed(() => {
  const query = readerSearchQuery.value.trim().toLowerCase()
  if (!query) {
    return 0
  }
  const text = currentPageText().toLowerCase()
  return text.split(query).length - 1
})
const ebookQuickPrompts = [
  { label: '学习', mode: 'study', question: '分析当前电子书页面的重点、难点和建议学习路径。' },
  { label: '总结', mode: 'summary', question: '总结当前电子书和当前章节的核心内容。' },
  { label: '问题', mode: 'questions', question: '基于当前章节生成 5 个复习问题，并给出参考答案。' },
]
const ebookAnalysisSections = computed<PageAnalysisSection[]>(() => {
  const sections: PageAnalysisSection[] = []
  if (detail.value) {
    sections.push({
      title: '电子书信息',
      content: compactLines([
        `标题: ${detail.value.title || '-'}`,
        `作者: ${detail.value.book_author || detail.value.author_list?.join(' / ') || '-'}`,
        `出版社: ${detail.value.press_name || '-'}`,
        `分类: ${detail.value.classify_name || '-'}`,
        `简介: ${detail.value.book_intro || detail.value.author_info || '-'}`,
        `评分: ${detail.value.product_score || detail.value.douban_score || '-'}`,
      ]),
    })
    sections.push({
      title: '目录',
      content: detail.value.catalog
        .slice(0, 80)
        .map((item) => `${item.play_order || '-'} ${'  '.repeat(Math.max(0, item.level - 1))}${item.text || item.chapter_id || '-'}`)
        .join('\n'),
    })
  }
  if (selectedChapterID.value || selectedChapterTitle.value) {
    sections.push({
      title: '当前章节',
      content: compactLines([
        `标题: ${selectedChapterTitle.value || '-'}`,
        `chapter_id: ${selectedChapterID.value || '-'}`,
        pageResponse.value ? `页索引: ${pageResponse.value.index}, 已加载: ${pageResponse.value.pages.length}, 是否结束: ${pageResponse.value.is_end}` : '',
      ]),
    })
  }
  const pageText = currentPageText()
  if (pageText) {
    sections.push({
      title: '当前加载页文本',
      content: pageText,
    })
  }
  return sections
})

onMounted(async () => {
  document.addEventListener('fullscreenchange', syncReaderFullscreenState)
  window.addEventListener('keydown', handleReaderKeydown)
  window.addEventListener('message', handleReaderFrameMessage)
  window.addEventListener('beforeunload', handleReaderBeforeUnload)
  restoreConnection()
  try {
    await hydrateBrowserSession()
    await loadDetail()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
})

onBeforeUnmount(() => {
  saveCurrentReadingProgress()
  document.removeEventListener('fullscreenchange', syncReaderFullscreenState)
  window.removeEventListener('keydown', handleReaderKeydown)
  window.removeEventListener('message', handleReaderFrameMessage)
  window.removeEventListener('beforeunload', handleReaderBeforeUnload)
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

const readReadingProgressMap = (): EbookReaderProgressMap => {
  const raw = localStorage.getItem(ebookReaderProgressStorageKey)
  if (!raw) {
    return {}
  }
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? (parsed as EbookReaderProgressMap) : {}
  } catch (error) {
    console.warn('Failed to parse ebook reader progress', error)
    localStorage.removeItem(ebookReaderProgressStorageKey)
    return {}
  }
}

const restoreReadingProgress = () => {
  const progress = readReadingProgressMap()[enid.value]
  if (!progress) {
    return null
  }
  columnMode.value = normalizeColumnMode(progress.columnMode)
  pageLoadCount.value = normalizePageLoadCount(progress.pageLoadCount)
  readerScale.value = normalizeReaderScale(progress.readerScale)
  const item = readableCatalogItems.value.find((catalogItem) => catalogItem.chapter_id === progress.chapterID)
  if (!item || isNonContentCatalogItem(item)) {
    return null
  }
  return {
    item,
    pageIndex: normalizePageIndex(progress.pageIndex),
  }
}

const saveCurrentReadingProgress = () => {
  if (!selectedChapterID.value) {
    return
  }
  saveReadingProgress(selectedChapterID.value, pageResponse.value?.index || 0)
}

const saveReadingProgress = (chapterID: string, pageIndex: number) => {
  if (!enid.value || !chapterID) {
    return
  }
  const progressMap = readReadingProgressMap()
  progressMap[enid.value] = {
    chapterID,
    chapterTitle: selectedChapterTitle.value,
    pageIndex: normalizePageIndex(pageIndex),
    columnMode: columnMode.value,
    pageLoadCount: pageLoadCount.value,
    readerScale: readerScale.value,
    updatedAt: new Date().toISOString(),
  }
  try {
    localStorage.setItem(ebookReaderProgressStorageKey, JSON.stringify(progressMap))
  } catch (error) {
    console.warn('Failed to save ebook reader progress', error)
  }
}

const normalizeColumnMode = (value: unknown): ColumnMode => {
  const mode = Number(value)
  return mode === 1 || mode === 2 || mode === 3 ? mode : defaultColumnMode
}

const normalizePageLoadCount = (value: unknown) => {
  const count = Number(value)
  return pageLoadCountOptions.includes(count) ? count : defaultPageLoadCount
}

const normalizeReaderScale = (value: unknown) => {
  const scale = Number(value)
  if (!Number.isFinite(scale)) {
    return 1
  }
  return Math.min(1.18, Math.max(0.86, scale))
}

const normalizePageIndex = (value: unknown) => {
  const pageIndex = Number(value)
  if (!Number.isFinite(pageIndex) || pageIndex < 0) {
    return 0
  }
  return Math.floor(pageIndex)
}

const loadDetail = async () => {
  if (!token.value) {
    connected.value = false
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  loading.value = true
  errorMessage.value = ''
  pageError.value = ''
  try {
    await hydrateBrowserSession()
    saveConnection()
    detail.value = await client.value.getDedaoEbookDetail(enid.value)
    connected.value = true
    const restoredProgress = restoreReadingProgress()
    if (restoredProgress) {
      await openChapter(restoredProgress.item, restoredProgress.pageIndex)
      return
    }
    const initialCatalogItem = preferredInitialCatalogItem()
    if (initialCatalogItem) {
      await openChapter(initialCatalogItem)
    }
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const addCurrentEbookToBookshelf = async () => {
  if (!enid.value) {
    return
  }
  await hydrateBrowserSession()
  if (!token.value) {
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  shelfAdding.value = true
  errorMessage.value = ''
  try {
    saveConnection()
    const ebook = await client.value.addDedaoEbookToBookshelf(enid.value)
    markDetailOnBookshelf(ebook)
    connected.value = true
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    shelfAdding.value = false
  }
}

const markDetailOnBookshelf = (ebook: DedaoEbook) => {
  if (!detail.value) {
    return
  }
  detail.value = {
    ...detail.value,
    id: ebook.id || detail.value.id,
    title: ebook.title || detail.value.title,
    cover: ebook.icon || detail.value.cover,
    price: ebook.price || detail.value.price,
    is_buy: true,
    is_on_bookshelf: true,
    can_trial_read: ebook.can_trial_read ?? detail.value.can_trial_read,
  }
}

const openChapter = async (item: DedaoEbookCatalogItem, pageIndex = 0) => {
  if (!item.chapter_id) {
    return
  }
  selectedChapterID.value = item.chapter_id
  selectedChapterTitle.value = item.text
  catalogOpen.value = false
  await loadChapterPages(item.chapter_id, pageIndex)
  scrollReaderToTop()
}

const setColumnMode = (mode: ColumnMode) => {
  columnMode.value = mode
  saveCurrentReadingProgress()
}

const reloadCurrentChapter = async () => {
  if (!selectedChapterID.value) {
    return
  }
  await loadChapterPages(selectedChapterID.value, pageResponse.value?.index || 0)
}

const loadRelativePage = async (direction: -1 | 1) => {
  if (!selectedChapterID.value || !pageResponse.value) {
    return
  }
  if (direction > 0 && pageResponse.value.is_end) {
    const nextChapter = adjacentReadableCatalogItem(1)
    if (nextChapter) {
      await openChapter(nextChapter)
    }
    return
  }
  if (direction < 0 && pageResponse.value.index <= 0) {
    const previousChapter = adjacentReadableCatalogItem(-1)
    if (previousChapter) {
      await openChapter(previousChapter)
    }
    return
  }
  const loadedCount = Math.max(1, pageResponse.value.pages.length || pageResponse.value.count || pageLoadCount.value)
  const nextIndex = Math.max(0, pageResponse.value.index + direction * loadedCount)
  await loadChapterPages(selectedChapterID.value, nextIndex)
  scrollReaderToTop()
}

const loadChapterPages = async (chapterID: string, index: number) => {
  pageLoading.value = true
  pageError.value = ''
  try {
    pageResponse.value = await client.value.getDedaoEbookChapterPages(enid.value, chapterID, index, pageLoadCount.value, 0)
    saveReadingProgress(chapterID, pageResponse.value.index)
  } catch (error) {
    pageResponse.value = null
    pageError.value = error instanceof Error ? error.message : String(error)
  } finally {
    pageLoading.value = false
  }
}

const handleReaderBeforeUnload = () => {
  saveCurrentReadingProgress()
}

const toggleFullscreen = async () => {
  if (readerFullscreen.value) {
    await exitReaderFullscreen()
    return
  }
  await requestReaderFullscreen()
}

const requestReaderFullscreen = async () => {
  readerFullscreen.value = true
  try {
    const target = readerRootRef.value || document.documentElement
    if (target.requestFullscreen && !document.fullscreenElement) {
      await target.requestFullscreen()
    }
  } catch {
    readerFullscreen.value = true
  }
}

const exitReaderFullscreen = async () => {
  readerFullscreen.value = false
  if (document.fullscreenElement) {
    try {
      await document.exitFullscreen()
    } catch {
      readerFullscreen.value = false
    }
  }
}

const syncReaderFullscreenState = () => {
  if (!document.fullscreenElement) {
    readerFullscreen.value = false
    return
  }
  readerFullscreen.value = document.fullscreenElement === readerRootRef.value || document.fullscreenElement === document.documentElement
}

const handleReaderKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && readerFullscreen.value && !document.fullscreenElement) {
    readerFullscreen.value = false
  }
}

const handleReaderFrameMessage = (event: MessageEvent) => {
  const data = event.data as { source?: string; direction?: number } | null
  if (!data || data.source !== 'ebook-text-boundary') {
    return
  }
  const direction = data.direction && data.direction < 0 ? -1 : 1
  triggerReaderAutoAdvance(direction)
}

const handleReaderWheel = (event: WheelEvent) => {
  if (Math.abs(event.deltaY) < 1) {
    return
  }
  event.preventDefault()
  queueReaderWheelDelta(event.deltaY)
}

const queueReaderWheelDelta = (deltaY: number) => {
  if (autoAdvanceRunning.value || pageLoading.value || loading.value) {
    return
  }
  if (Date.now() < autoAdvanceCooldownUntil.value) {
    return
  }
  wheelPagingDelta.value += deltaY
  if (Math.abs(wheelPagingDelta.value) < wheelPagingThreshold) {
    return
  }
  const direction = wheelPagingDelta.value > 0 ? 1 : -1
  wheelPagingDelta.value = 0
  triggerReaderAutoAdvance(direction)
}

const triggerReaderAutoAdvance = (direction: -1 | 1) => {
  if (autoAdvanceRunning.value || pageLoading.value || loading.value) {
    return
  }
  if (Date.now() < autoAdvanceCooldownUntil.value) {
    return
  }
  if ((direction > 0 && !canGoNext.value) || (direction < 0 && !canGoPrevious.value)) {
    return
  }
  autoAdvanceRunning.value = true
  void loadRelativePage(direction).finally(() => {
    autoAdvanceRunning.value = false
  })
}

const scrollReaderToTop = () => {
  autoAdvanceCooldownUntil.value = Date.now() + 700
}

const catalogKey = (item: DedaoEbookCatalogItem) => `${item.chapter_id || item.href || item.text}-${item.play_order || 0}`

const preferredInitialCatalogItem = () =>
  readableCatalogItems.value.find((item) => !isNonContentCatalogItem(item)) || readableCatalogItems.value[0] || null

const isNonContentCatalogItem = (item: DedaoEbookCatalogItem) => {
  const resourceName = catalogResourceName(item).toLowerCase()
  const text = (item.text || '').trim()
  return (
    nonContentCatalogIDKeywords.some((keyword) => resourceName === keyword || resourceName.startsWith(`${keyword}.`)) ||
    nonContentCatalogTextPatterns.some((pattern) => pattern.test(text))
  )
}

const catalogResourceName = (item: DedaoEbookCatalogItem) => {
  const resourcePath = (item.chapter_id || item.href || '').split('#')[0].trim()
  return resourcePath.split('/').filter(Boolean).pop() || resourcePath
}

const adjacentReadableCatalogItem = (direction: -1 | 1) => {
  const currentIndex = currentReadableIndex.value
  if (currentIndex < 0) {
    return null
  }
  for (let index = currentIndex + direction; index >= 0 && index < readableCatalogItems.value.length; index += direction) {
    const item = readableCatalogItems.value[index]
    if (item.chapter_id && item.chapter_id !== selectedChapterID.value) {
      return item
    }
  }
  return null
}

const currentPageText = () => {
  const pages = pageResponse.value?.pages || []
  const text = pages
    .map((page) => {
      const extracted = extractSVGText(page.svg)
      if (extracted) {
        return `Page ${page.page_num}\n${extracted}`
      }
      return `Page ${page.page_num}: begin_offset=${page.begin_offset}, end_offset=${page.end_offset}`
    })
    .join('\n\n')
  return text.trim()
}

const extractSVGText = (svg: string) => {
  if (!svg.trim()) {
    return ''
  }
  try {
    const doc = new DOMParser().parseFromString(svg, 'image/svg+xml')
    return extractSVGTextFromDocument(doc)
  } catch {
    return svg.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()
  }
}

const extractSVGTextFromDocument = (doc: Document) => {
  const textNodes = Array.from(doc.querySelectorAll('text,tspan')).filter((node) => {
    if (node.nodeName.toLowerCase() !== 'text') {
      return true
    }
    return !node.querySelector('tspan')
  })
  const rows = new Map<number, Array<{ index: number; x: number; text: string }>>()
  textNodes.forEach((node, index) => {
    const text = node.textContent?.replace(/\s+/g, ' ').trim() || ''
    if (!text) {
      return
    }
    const x = readSVGCoordinate(node, 'x')
    const y = readSVGCoordinate(node, 'y')
    const rowKey = Math.round(y / 8) * 8 || index
    const row = rows.get(rowKey) || []
    row.push({ index, x, text })
    rows.set(rowKey, row)
  })
  return Array.from(rows.entries())
    .sort(([a], [b]) => a - b)
    .map(([, row]) =>
      row
        .sort((a, b) => a.x - b.x || a.index - b.index)
        .map((item) => item.text)
        .join(''),
    )
    .filter(Boolean)
    .join('\n')
}

const estimateEbookTotalPageCount = () => {
  const pages = pageResponse.value?.pages || []
  if (!pages.length) {
    return 0
  }
  if (!pageResponse.value?.is_end) {
    return 0
  }
  return Math.max(...pages.map((page) => page.page_num), pageResponse.value.index + pages.length)
}

const normalizeEbookSvg = (svg: string) => {
  const source = svg.trim()
  if (!source) {
    return ''
  }
  try {
    const doc = new DOMParser().parseFromString(source, 'image/svg+xml')
    const svgElement = doc.documentElement
    if (svgElement.nodeName.toLowerCase() !== 'svg') {
      return source
    }
    const width = parseSVGLength(svgElement.getAttribute('width'))
    const height = parseSVGLength(svgElement.getAttribute('height'))
    if (!svgElement.getAttribute('viewBox') && width && height) {
      svgElement.setAttribute('viewBox', `0 0 ${width} ${height}`)
    }
    svgElement.setAttribute('width', '100%')
    svgElement.setAttribute('height', '100%')
    svgElement.setAttribute('preserveAspectRatio', 'xMidYMid meet')
    return new XMLSerializer().serializeToString(svgElement)
  } catch {
    return source.replace(/<svg\b/i, '<svg preserveAspectRatio="xMidYMid meet" width="100%" height="100%"')
  }
}

const ebookSvgTextFallback = (svg: string) => {
  const source = svg.trim()
  if (!source) {
    return ''
  }
  try {
    const doc = new DOMParser().parseFromString(source, 'image/svg+xml')
    const svgElement = doc.documentElement
    if (svgElement.nodeName.toLowerCase() !== 'svg') {
      return ''
    }
    const text = extractSVGTextFromDocument(doc)
    if (text.length < 12) {
      return ''
    }
    const contentBox = computeEbookSvgContentBox(svgElement)
    const rootWidth = parseSVGLength(svgElement.getAttribute('width'))
    const rootHeight = parseSVGLength(svgElement.getAttribute('height'))
    const textNodeCount = svgElement.querySelectorAll('text,tspan').length
    if (!contentBox || !rootHeight || textNodeCount < 6) {
      return ''
    }
    const contentHeight = contentBox.maxY - contentBox.minY
    const compressedTextCanvas = contentHeight > 0 && rootHeight > 3000 && contentHeight / rootHeight < 0.08
    const textDenseHugeCanvas = rootWidth > 20000 && rootHeight > 20000 && text.length > 120 && textNodeCount > 40
    return compressedTextCanvas || textDenseHugeCanvas ? text : ''
  } catch {
    return ''
  }
}

const computeEbookSvgContentBox = (svgElement: Element): SVGContentBox | null => {
  const boxes: SVGContentBox[] = []
  svgElement.querySelectorAll('text,tspan').forEach((node) => {
    const text = node.textContent?.trim() || ''
    if (!text) {
      return
    }
    const x = readSVGCoordinate(node, 'x')
    const y = readSVGCoordinate(node, 'y')
    const fontSize = readSVGFontSize(node)
    boxes.push({
      minX: x,
      minY: Math.max(0, y - fontSize),
      maxX: x + Math.max(fontSize, text.length * fontSize * 0.6),
      maxY: y + fontSize * 0.35,
    })
  })
  svgElement.querySelectorAll('image,rect').forEach((node) => {
    const x = readSVGCoordinate(node, 'x')
    const y = readSVGCoordinate(node, 'y')
    const width = parseSVGLength(node.getAttribute('width'))
    const height = parseSVGLength(node.getAttribute('height'))
    if (width && height) {
      boxes.push({ minX: x, minY: y, maxX: x + width, maxY: y + height })
    }
  })
  if (!boxes.length) {
    return null
  }
  return boxes.reduce<SVGContentBox>(
    (bounds, box) => ({
      minX: Math.min(bounds.minX, box.minX),
      minY: Math.min(bounds.minY, box.minY),
      maxX: Math.max(bounds.maxX, box.maxX),
      maxY: Math.max(bounds.maxY, box.maxY),
    }),
    boxes[0],
  )
}

const parseSVGLength = (value: string | null) => {
  if (!value) {
    return 0
  }
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

const parseSVGNumber = (value: string | null) => {
  if (!value) {
    return 0
  }
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) ? parsed : 0
}

const readSVGCoordinate = (node: Element, attr: 'x' | 'y') => {
  let current: Element | null = node
  while (current) {
    const value = current.getAttribute(attr)
    if (value) {
      return parseSVGNumber(value)
    }
    current = current.parentElement
  }
  return 0
}

const readSVGFontSize = (node: Element) => {
  let current: Element | null = node
  while (current) {
    const attrSize = parseSVGLength(current.getAttribute('font-size'))
    if (attrSize) {
      return attrSize
    }
    const styleSize = /font-size\s*:\s*([0-9.]+)/i.exec(current.getAttribute('style') || '')?.[1]
    const parsedStyleSize = parseSVGLength(styleSize || null)
    if (parsedStyleSize) {
      return parsedStyleSize
    }
    current = current.parentElement
  }
  return 24
}

const compactLines = (lines: Array<string | number | undefined | null>) =>
  lines
    .map((line) => String(line ?? '').trim())
    .filter(Boolean)
    .join('\n')

const escapeHTML = (value: string) =>
  value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')

const textToParagraphs = (text: string) =>
  text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => `<p>${escapeHTML(line)}</p>`)
    .join('')

const svgToFrame = (svg: string) => {
  const textFallback = ebookSvgTextFallback(svg)
  if (textFallback) {
    return {
      srcdoc: textPageSrcdoc(textFallback),
      sandbox: 'allow-scripts',
      passive: false,
    }
  }
  return {
    srcdoc: svgPageSrcdoc(normalizeEbookSvg(svg)),
    sandbox: '',
    passive: true,
  }
}

const svgPageSrcdoc = (svg: string) => `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <style>
      html, body {
        margin: 0;
        width: 100%;
        height: 100%;
        overflow: hidden;
        background: transparent;
      }
      body {
        display: grid;
        place-items: center;
        padding: 0;
        box-sizing: border-box;
      }
      svg {
        display: block;
        width: 100%;
        height: 100%;
      }
    </style>
  </head>
  <body>${svg || ''}</body>
</html>`

const textPageSrcdoc = (text: string) => `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <style>
      html, body {
        margin: 0;
        width: 100%;
        height: 100%;
        overflow: hidden;
        background: transparent;
      }
      body {
        color: #3f3f3f;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
      }
      .ebook-text-page {
        box-sizing: border-box;
        width: 100%;
        height: 100%;
        max-width: 980px;
        margin: 0 auto;
        padding: 7vh min(8vw, 92px) 9vh;
        overflow: auto;
        scrollbar-width: none;
        font-size: clamp(18px, 1.22vw, 27px);
        font-weight: 500;
        line-height: 1.9;
        letter-spacing: 0;
      }
      .ebook-text-page::-webkit-scrollbar {
        width: 0;
        height: 0;
      }
      .ebook-text-page p {
        margin: 0 0 1.05em;
      }
    </style>
  </head>
  <body><main class="ebook-text-page">${textToParagraphs(text)}</main></body>
  <script>
    (() => {
      const page = document.querySelector('.ebook-text-page');
      let accumulatedDelta = 0;
      let lastSentAt = 0;
      const threshold = 120;
      const edgeSlack = 4;
      const sendBoundary = (direction) => {
        const now = Date.now();
        if (now - lastSentAt < 650) {
          return;
        }
        lastSentAt = now;
        parent.postMessage({ source: 'ebook-text-boundary', direction }, '*');
      };
      page?.addEventListener('wheel', (event) => {
        const atTop = page.scrollTop <= edgeSlack;
        const atBottom = page.scrollTop + page.clientHeight >= page.scrollHeight - edgeSlack;
        const direction = event.deltaY > 0 ? 1 : -1;
        if ((direction > 0 && atBottom) || (direction < 0 && atTop)) {
          event.preventDefault();
          accumulatedDelta += event.deltaY;
          if (Math.abs(accumulatedDelta) >= threshold) {
            accumulatedDelta = 0;
            sendBoundary(direction);
          }
        } else {
          accumulatedDelta = 0;
        }
      }, { passive: false });
    })();
  <\/script>
</html>`
</script>

<style scoped>
.ebook-detail-reader {
  --reader-page-height: calc(100vh - 228px);
  display: grid;
  grid-template-rows: auto auto auto minmax(0, 1fr);
  height: 100vh;
  margin: 0;
  overflow: hidden;
  background: #f5f5f5;
  color: #3f3f3f;
}

.ebook-detail-reader.reader-fullscreen {
  --reader-page-height: calc(100vh - 170px);
  position: fixed;
  inset: 0;
  z-index: 9999;
  width: 100vw;
  height: 100vh;
  background: #f5f5f5;
}

.ebook-detail-reader.reader-fullscreen .dedao-reader-toolbar {
  min-height: 72px;
  padding: 8px 36px;
}

.ebook-detail-reader.reader-fullscreen .dedao-reader-stage {
  padding: 18px 48px 56px;
}

.ebook-detail-reader.reader-fullscreen .reader-bottom-bar {
  z-index: 10020;
}

.ebook-detail-reader.reader-fullscreen .reader-floating-actions,
.ebook-detail-reader.reader-fullscreen .reader-help {
  z-index: 10030;
}

.dedao-reader-toolbar {
  position: relative;
  top: 0;
  z-index: 30;
  display: grid;
  grid-template-columns: minmax(330px, 1fr) minmax(260px, 420px) minmax(430px, 1fr);
  gap: 22px;
  align-items: center;
  min-height: 104px;
  border-bottom: 1px solid #e6e7e9;
  padding: 14px 48px 12px;
  background: #f1f2f4;
}

.reader-tool-cluster {
  display: flex;
  align-items: start;
  gap: 14px;
  min-width: 0;
}

.right-tools {
  justify-content: flex-end;
}

.reader-tool {
  display: inline-flex;
  flex: 0 0 auto;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  min-width: 48px;
  border: 0;
  border-radius: 8px;
  padding: 0;
  background: transparent;
  color: #686868;
  text-decoration: none;
}

.reader-tool:hover,
.reader-tool.active {
  color: var(--dedao-orange);
}

.reader-tool:disabled {
  color: #b8b8b8;
}

.tool-icon,
.column-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 34px;
  border: 1px solid #e1e2e4;
  border-radius: 4px;
  background: #f7f8f9;
  color: currentColor;
  font-size: 22px;
  line-height: 1;
}

.reader-tool small {
  color: currentColor;
  font-size: 12px;
  line-height: 16px;
}

.column-icon {
  gap: 4px;
}

.column-icon i {
  width: 10px;
  height: 18px;
  border: 2px solid currentColor;
  border-radius: 2px;
}

.reader-title-center {
  display: grid;
  justify-items: center;
  min-width: 0;
  text-align: center;
}

.reader-title-center strong {
  overflow: hidden;
  max-width: 100%;
  color: #333333;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 16px;
  font-weight: 700;
}

.reader-title-center span {
  margin-top: 7px;
  border-radius: 4px;
  padding: 5px 12px;
  background: #e9eaec;
  color: #575757;
  font-size: 13px;
  font-weight: 700;
}

.reader-settings-strip,
.reader-search-strip {
  position: relative;
  top: 0;
  z-index: 25;
  display: flex;
  gap: 16px;
  align-items: center;
  border-bottom: 1px solid #e7e7e7;
  padding: 10px 48px;
  background: rgba(245, 245, 245, 0.96);
}

.reader-settings-strip label {
  display: grid;
  grid-template-columns: auto minmax(120px, 180px);
  gap: 8px;
  align-items: center;
  color: #777777;
  font-size: 13px;
  font-weight: 700;
}

.reader-settings-strip input,
.reader-settings-strip select,
.reader-search-strip input {
  height: 34px;
  min-width: 0;
  border: 1px solid #dadada;
  border-radius: 5px;
  padding: 0 10px;
  background: #ffffff;
}

.reader-state {
  border: 1px solid #dddddd;
  border-radius: 999px;
  padding: 6px 12px;
  color: #888888;
  font-size: 12px;
  font-weight: 700;
}

.reader-state.ok {
  border-color: #d8eadf;
  color: #257347;
  background: #f5fbf7;
}

.reader-search-strip span {
  color: #888888;
  font-size: 13px;
}

.reader-error {
  margin: 12px 48px 0;
}

.dedao-reader-stage {
  display: flex;
  flex: 1;
  align-items: stretch;
  gap: 28px;
  min-height: 0;
  overflow: hidden;
  padding: 28px 48px 72px;
}

.dedao-page-spread {
  display: flex;
  flex: 1 1 auto;
  align-items: stretch;
  min-width: 0;
  min-height: 0;
  height: var(--reader-page-height);
}

.ebook-pages {
  display: grid;
  align-items: stretch;
  grid-auto-rows: minmax(0, 1fr);
  width: 100%;
  height: var(--reader-page-height);
  margin: 0 auto;
}

.columns-1 .ebook-pages {
  max-width: 880px;
  grid-template-columns: minmax(0, 1fr);
  gap: 34px;
}

.columns-2 .ebook-pages {
  max-width: 1780px;
  grid-template-columns: repeat(2, minmax(360px, 1fr));
  gap: 78px;
}

.columns-3 .ebook-pages {
  max-width: 1860px;
  grid-template-columns: repeat(3, minmax(280px, 1fr));
  gap: 36px;
}

.ebook-page-shell {
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto;
  min-width: 0;
  min-height: 0;
  height: var(--reader-page-height);
  align-content: stretch;
}

.ebook-page-frame {
  width: 100%;
  height: 100%;
  min-height: 0;
  border: 0;
  background: transparent;
  transform: scale(var(--reader-scale));
  transform-origin: top center;
}

.ebook-page-frame-passive {
  pointer-events: none;
}

.ebook-page-shell footer {
  align-self: end;
  justify-self: center;
  margin-top: 14px;
  color: #222222;
  font-size: 18px;
  line-height: 24px;
}

.reader-drawer {
  position: sticky;
  top: 126px;
  z-index: 20;
  flex: 0 0 320px;
  max-height: calc(100vh - 150px);
  overflow: auto;
  border: 1px solid #e4e4e4;
  border-radius: 8px;
  padding: 14px;
  background: rgba(255, 255, 255, 0.98);
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.08);
}

.analysis-drawer {
  flex-basis: 380px;
}

.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}

.drawer-head strong {
  color: #222222;
  font-size: 18px;
}

.drawer-head button {
  border: 1px solid #e1e1e1;
  border-radius: 4px;
  padding: 5px 10px;
  background: #ffffff;
  color: #666666;
}

.catalog-list {
  display: grid;
  gap: 0;
}

.catalog-row {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  width: 100%;
  min-height: 44px;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  background: transparent;
  color: var(--dedao-text);
  text-align: left;
  cursor: pointer;
}

.catalog-row.active {
  color: var(--dedao-orange);
  background: #fffaf6;
}

.catalog-row strong,
.ebook-context-summary h2 {
  overflow-wrap: anywhere;
}

.ebook-context-summary h2 {
  margin: 0 0 10px;
  color: #111111;
  font-size: 20px;
  line-height: 1.25;
}

.ebook-context-summary p {
  color: #666666;
  line-height: 1.6;
}

.ebook-context-summary dl {
  display: grid;
  gap: 8px;
}

.ebook-context-summary dt {
  color: var(--dedao-muted);
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.ebook-context-summary dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
  color: #222222;
}

.error-strip,
.empty-state {
  border: 1px dashed var(--dedao-border);
  border-radius: 10px;
  padding: 14px;
  color: var(--dedao-muted);
  background: var(--dedao-subtle);
}

.error-strip {
  border-color: #e2aaa2;
  color: #8a3025;
  background: #fff7f5;
}

.reader-loading {
  max-width: 520px;
  margin: 22vh auto 0;
  text-align: center;
}

.reader-bottom-bar {
  position: fixed;
  right: 0;
  bottom: 0;
  left: 0;
  z-index: 35;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 48px 14px;
  pointer-events: none;
}

.reader-bottom-bar button {
  pointer-events: auto;
}

.reader-bottom-bar button {
  border: 0;
  border-radius: 4px;
  padding: 8px 14px;
  background: #e7e7e7;
  color: #565656;
}

.reader-floating-actions {
  position: fixed;
  right: 42px;
  bottom: 76px;
  z-index: 40;
  display: grid;
  gap: 18px;
}

.reader-floating-actions button {
  width: 64px;
  height: 64px;
  border: 0;
  border-radius: 50%;
  background: #858993;
  color: #ffffff;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
  font-size: 26px;
  font-weight: 800;
}

.reader-floating-actions button.active {
  background: #6f747d;
  color: #ffffff;
}

.reader-floating-actions button:last-child::after {
  content: "";
  position: absolute;
  top: 75px;
  right: 2px;
  width: 13px;
  height: 13px;
  border-radius: 50%;
  background: var(--dedao-orange);
}

.reader-help {
  position: fixed;
  right: 16px;
  bottom: 14px;
  z-index: 40;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 50%;
  background: #767676;
  color: #ffffff;
  font-size: 18px;
  font-weight: 800;
}

button:disabled {
  opacity: 0.58;
}

@media (max-width: 1180px) {
  .dedao-reader-toolbar {
    grid-template-columns: 1fr;
    gap: 12px;
    padding: 12px 16px;
  }

  .reader-title-center {
    order: -1;
  }

  .reader-tool-cluster,
  .right-tools {
    justify-content: center;
    flex-wrap: wrap;
  }

  .dedao-reader-stage {
    flex-direction: column;
    padding: 22px 16px 82px;
  }

  .ebook-detail-reader {
    --reader-page-height: calc(100vh - 252px);
  }

  .reader-drawer {
    position: static;
    width: 100%;
    max-height: none;
  }

  .columns-2 .ebook-pages,
  .columns-3 .ebook-pages {
    grid-template-columns: 1fr;
    gap: 30px;
  }

  .reader-bottom-bar {
    padding: 10px 14px 14px;
  }

  .reader-floating-actions {
    right: 18px;
  }

  .reader-settings-strip,
  .reader-search-strip {
    top: 0;
    flex-wrap: wrap;
    padding: 10px 16px;
  }
}
</style>
