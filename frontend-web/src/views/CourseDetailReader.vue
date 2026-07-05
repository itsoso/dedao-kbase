<template>
  <main class="course-detail-reader">
    <section class="reader-toolbar">
      <RouterLink class="back-link" to="/course">课程</RouterLink>
      <div class="brand-block">
        <span class="eyebrow">Course Study</span>
        <h2>{{ detail?.course.title || '课程阅读' }}</h2>
      </div>

      <button class="primary-action" type="button" :disabled="loading" @click="loadDetail">
        {{ loading ? '加载中' : '刷新' }}
      </button>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section ref="readerWorkspaceRef" class="reader-workspace draggable-layout course-two-pane-layout" :style="readerWorkspaceStyle">
      <aside class="article-rail">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Articles</span>
            <h2>课程目录</h2>
          </div>
          <span>{{ articleCountLabel }}</span>
        </div>

        <div class="article-search">
          <input
            v-model="articleSearchQuery"
            type="search"
            placeholder="搜索本课程标题、摘要"
            @keyup.enter="searchCourseArticles"
          />
          <button type="button" :disabled="articleSearchLoading || articleListLoading" @click="searchCourseArticles">
            {{ articleSearchLoading ? '搜索中' : '搜索' }}
          </button>
        </div>

        <div class="article-list-meta">
          <span>{{ filteredArticleCountLabel }}</span>
          <span v-if="articleSearchStatus">{{ articleSearchStatus }}</span>
        </div>

        <div ref="articleListRef" class="article-list" @scroll="handleArticleListScroll">
          <button
            v-for="article in visibleArticles"
            :key="article.enid || article.id"
            type="button"
            class="article-row"
            :class="{ active: selectedArticleEnid === article.enid }"
            @click="openArticle(article)"
          >
            <span>{{ article.order_num || article.id }}</span>
            <div>
              <strong>{{ article.title || `Article ${article.id}` }}</strong>
              <small>{{ formatPublishTime(article.publish_time) }}</small>
            </div>
          </button>
        </div>

        <div v-if="filteredArticles.length || hasMoreArticles" class="article-pagination">
          <button type="button" :disabled="articlePage <= 1" @click="goToPreviousArticlePage">上一页</button>
          <span>{{ articlePage }} / {{ articleTotalPages }}</span>
          <button
            type="button"
            :disabled="articleListLoading || !canGoNextArticlePage"
            @click="goToNextArticlePage"
          >
            下一页
          </button>
        </div>

        <button v-if="hasMoreArticles" class="secondary-action" type="button" :disabled="articleListLoading" @click="loadMoreArticles">
          {{ articleListLoading ? '加载中' : '加载更多' }}
        </button>

        <div v-if="!loading && articles.length && !filteredArticles.length" class="empty-state">没有匹配的课程文章。</div>
        <div v-if="!loading && !articles.length" class="empty-state">暂无课程文章。</div>
      </aside>

      <div
        class="column-resizer left-resizer"
        role="separator"
        aria-label="调整课程目录宽度"
        @pointerdown="beginColumnResize('left', $event)"
      ></div>

      <article ref="articleReaderRef" class="article-reader" @scroll="handleArticleReaderScroll">
        <div class="article-reader-head">
          <div>
            <span class="eyebrow">Markdown Reader</span>
            <h1>{{ selectedArticleTitle || '选择一篇文章' }}</h1>
          </div>
          <div class="course-reader-actions">
            <span class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</span>
            <button type="button" :disabled="!detail" @click="openContextPanel('Course')">课程信息</button>
            <button type="button" :disabled="!detail" @click="openContextPanel('Analysis')">AI 分析</button>
          </div>
        </div>

        <div v-if="articleError && !readingEntries.length" class="error-strip">{{ articleError }}</div>
        <div v-if="articleLoading && !readingEntries.length" class="empty-state">加载文章中...</div>
        <div v-else-if="readingEntries.length" class="continuous-article-stream">
          <section
            v-for="entry in renderedReadingEntries"
            :key="entry.enid"
            class="reading-entry"
            :class="{ active: selectedArticleEnid === entry.enid }"
          >
            <div class="reading-entry-head">
              <span>{{ entry.orderNum || entry.id || 'Article' }}</span>
              <h2>{{ entry.title || '课程文章' }}</h2>
            </div>
            <div v-if="entry.error" class="error-strip">{{ entry.error }}</div>
            <div v-else-if="entry.loading" class="empty-state">加载文章中...</div>
            <div v-else class="answer-markdown" v-html="entry.html"></div>
          </section>
          <div v-if="continuousArticleLoading" class="empty-state">正在加载下一篇...</div>
          <div v-else-if="!hasNextReadingArticle" class="reader-end-state">已读完已载入课程内容</div>
        </div>
        <div v-else class="empty-state">从左侧选择文章开始阅读。</div>
      </article>

      <aside v-if="activeContextPanel" class="course-context course-context-drawer">
        <div class="context-head">
          <div>
            <span class="eyebrow">Course</span>
            <h2>{{ detail?.course.title || '课程详情' }}</h2>
          </div>
          <button class="text-action" type="button" @click="activeContextPanel = ''">关闭</button>
        </div>
        <div class="context-tabs">
          <button type="button" :class="{ active: activeContextPanel === 'Course' }" @click="activeContextPanel = 'Course'">课程</button>
          <button type="button" :class="{ active: activeContextPanel === 'Analysis' }" @click="activeContextPanel = 'Analysis'">AI 分析</button>
        </div>

        <template v-if="activeContextPanel === 'Course'">
          <p v-if="hasCourseIntro" class="course-intro">{{ courseIntro }}</p>
          <p v-else class="course-intro muted">暂无简介</p>
          <dl>
            <div>
              <dt>Lecturer</dt>
              <dd>{{ detail?.course.lecturer_name || '-' }}</dd>
            </div>
            <div>
              <dt>Articles</dt>
              <dd>{{ detail?.course.article_count || articles.length || '-' }}</dd>
            </div>
            <div>
              <dt>Students</dt>
              <dd>{{ detail?.course.learn_user_count || '-' }}</dd>
            </div>
            <div>
              <dt>ENID</dt>
              <dd>{{ enid }}</dd>
            </div>
          </dl>
        </template>

        <PageAnalysisPanel
          v-else
          :base-url="baseUrl"
          :token="token"
          source="course"
          :page-title="detail?.course.title || '课程页面'"
          :page-url="coursePageURL"
          :context-sections="courseAnalysisSections"
          :quick-prompts="courseQuickPrompts"
          default-question="分析当前课程页面的重点、难点和建议学习路径。"
        />
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  getBrowserSession,
  KBaseClient,
  type DedaoArticle,
  type DedaoCourseDetail,
  type PageAnalysisSection,
} from '../api'
import PageAnalysisPanel from '../components/PageAnalysisPanel.vue'
import { renderMarkdown } from '../utils/markdownRender'

interface CourseReadingEntry {
  enid: string
  id?: number
  orderNum?: number
  title: string
  markdown: string
  loading: boolean
  error: string
}

const storageKey = 'dedao-kbase-web-settings'
const layoutStorageKey = 'dedao-course-reader-layout'
const route = useRoute()

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const articleLoading = ref(false)
const articleListLoading = ref(false)
const errorMessage = ref('')
const articleError = ref('')
const detail = ref<DedaoCourseDetail | null>(null)
const articles = ref<DedaoArticle[]>([])
const selectedArticleEnid = ref('')
const selectedArticleTitle = ref('')
const markdown = ref('')
const readingEntries = ref<CourseReadingEntry[]>([])
const articlesMaxID = ref(0)
const hasMoreArticles = ref(false)
const continuousArticleLoading = ref(false)
const articleSearchQuery = ref('')
const articleSearchStatus = ref('')
const articleSearchLoading = ref(false)
const articlePage = ref(1)
const articlePageSize = ref(30)
const activeContextPanel = ref<'Course' | 'Analysis' | ''>('')
const readerWorkspaceRef = ref<HTMLElement | null>(null)
const articleReaderRef = ref<HTMLElement | null>(null)
const articleListRef = ref<HTMLElement | null>(null)
const layoutColumns = ref({ left: 248 })
const activeResizeTarget = ref<'left' | null>(null)

const enid = computed(() => String(route.params.enid || ''))
const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const renderedReadingEntries = computed(() =>
  readingEntries.value.map((entry) => ({
    ...entry,
    html: renderMarkdown(entry.markdown),
  })),
)
const coursePageURL = computed(() => `/course/${encodeURIComponent(enid.value)}`)
const readerWorkspaceStyle = computed(() => ({
  '--course-left-column': `${layoutColumns.value.left}px`,
}))
const normalizedArticleSearch = computed(() => articleSearchQuery.value.trim().toLowerCase())
const filteredArticles = computed(() => {
  const query = normalizedArticleSearch.value
  if (!query) {
    return articles.value
  }
  const terms = query.split(/\s+/).filter(Boolean)
  return articles.value.filter((article) => {
    const haystack = [article.title, article.summary, article.id, article.order_num]
      .map((value) => String(value ?? '').toLowerCase())
      .join('\n')
    return terms.every((term) => haystack.includes(term))
  })
})
const loadedArticlePages = computed(() => Math.max(1, Math.ceil(filteredArticles.value.length / articlePageSize.value)))
const knownTotalArticlePages = computed(() => {
  const total = detail.value?.course.article_count || 0
  if (normalizedArticleSearch.value || total <= 0) {
    return loadedArticlePages.value
  }
  return Math.max(loadedArticlePages.value, Math.ceil(total / articlePageSize.value))
})
const articleTotalPages = computed(() => Math.max(loadedArticlePages.value, knownTotalArticlePages.value))
const visibleArticles = computed(() => {
  const start = (articlePage.value - 1) * articlePageSize.value
  return filteredArticles.value.slice(start, start + articlePageSize.value)
})
const canGoNextLoadedArticlePage = computed(() => articlePage.value < loadedArticlePages.value)
const canGoNextArticlePage = computed(() => canGoNextLoadedArticlePage.value || hasMoreArticles.value)
const nextReadingArticle = computed(() => {
  const lastEntry = readingEntries.value[readingEntries.value.length - 1]
  if (!lastEntry) {
    return articles.value[0]
  }
  const currentIndex = findArticleIndex(lastEntry.enid)
  if (currentIndex >= 0 && currentIndex + 1 < articles.value.length) {
    return articles.value[currentIndex + 1]
  }
  return undefined
})
const hasNextReadingArticle = computed(() => Boolean(nextReadingArticle.value || hasMoreArticles.value))
const articleCountLabel = computed(() => {
  const total = detail.value?.course.article_count || 0
  return total > articles.value.length ? `${articles.value.length}/${total}` : String(articles.value.length)
})
const filteredArticleCountLabel = computed(() => {
  if (normalizedArticleSearch.value) {
    return `命中 ${filteredArticles.value.length} 条 · 已载入 ${articles.value.length} 条`
  }
  return `已载入 ${articles.value.length} 条`
})
const courseIntro = computed(() => {
  const course = detail.value?.course
  return String(course?.highlight || course?.intro || '').trim()
})
const hasCourseIntro = computed(() => Boolean(courseIntro.value))
const courseQuickPrompts = [
  { label: '学习', mode: 'study', question: '分析当前课程页面的重点、难点和建议学习路径。' },
  { label: '总结', mode: 'summary', question: '总结当前课程和当前文章的核心内容。' },
  { label: '复习题', mode: 'questions', question: '基于当前文章生成 5 个复习问题，并给出参考答案。' },
]
const courseAnalysisSections = computed<PageAnalysisSection[]>(() => {
  const sections: PageAnalysisSection[] = []
  const course = detail.value?.course
  if (course) {
    sections.push({
      title: '课程信息',
      content: compactLines([
        `标题: ${course.title || '-'}`,
        `讲师: ${course.lecturer_name || '-'}`,
        `讲师头衔: ${course.lecturer_title || '-'}`,
        `简介: ${course.intro || '-'}`,
        `亮点: ${course.highlight || '-'}`,
        `文章数: ${course.article_count || articles.value.length || 0}`,
        `学习人数: ${course.learn_user_count || '-'}`,
      ]),
    })
  }
  if (articles.value.length) {
    sections.push({
      title: '课程目录',
      content: articles.value
        .slice(0, 60)
        .map((article) => `${article.order_num || article.id}. ${article.title || `Article ${article.id}`} - ${article.summary || ''}`)
        .join('\n'),
    })
  }
  if (selectedArticleTitle.value || markdown.value) {
    sections.push({
      title: '当前文章',
      content: compactLines([
        `标题: ${selectedArticleTitle.value || '-'}`,
        `ENID: ${selectedArticleEnid.value || '-'}`,
        markdown.value,
      ]),
    })
  }
  return sections
})

onMounted(async () => {
  restoreConnection()
  restoreLayoutColumns()
  try {
    await hydrateBrowserSession()
    await loadDetail()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
})

onBeforeUnmount(() => {
  stopColumnResize()
})

watch(articleSearchQuery, () => {
  articlePage.value = 1
  articleSearchStatus.value = ''
})

watch(enid, async (next, previous) => {
  if (next && next !== previous) {
    await loadDetail()
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

const restoreLayoutColumns = () => {
  const raw = localStorage.getItem(layoutStorageKey)
  if (!raw) {
    return
  }
  try {
    const parsed = JSON.parse(raw) as { left?: number }
    layoutColumns.value = {
      left: clampNumber(parsed.left || layoutColumns.value.left, 196, 360),
    }
  } catch {
    localStorage.removeItem(layoutStorageKey)
  }
}

const saveLayoutColumns = () => {
  localStorage.setItem(layoutStorageKey, JSON.stringify(layoutColumns.value))
}

const hydrateBrowserSession = async () => {
  const browserSession = await getBrowserSession()
  if (browserSession?.token) {
    token.value = browserSession.token
    baseUrl.value = window.location.origin
    saveConnection()
  }
}

const loadDetail = async () => {
  if (!token.value) {
    connected.value = false
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  loading.value = true
  errorMessage.value = ''
  articleError.value = ''
  try {
    await hydrateBrowserSession()
    saveConnection()
    const result = await client.value.getDedaoCourseDetail(enid.value)
    detail.value = result
    articles.value = result.articles || []
    setArticleCursorFromArticles(articles.value)
    refreshHasMoreArticles(Boolean(result.has_more))
    articlePage.value = 1
    articleSearchQuery.value = ''
    articleSearchStatus.value = ''
    readingEntries.value = []
    connected.value = true
    const firstArticle = articles.value[0]
    if (firstArticle) {
      await openArticle(firstArticle)
    } else {
      selectedArticleEnid.value = ''
      selectedArticleTitle.value = ''
      markdown.value = ''
      readingEntries.value = []
    }
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const loadMoreArticles = async () => {
  if (articleListLoading.value || !hasMoreArticles.value) {
    return
  }
  articleListLoading.value = true
  errorMessage.value = ''
  try {
    const result = await client.value.listDedaoCourseArticles(enid.value, articlePageSize.value, articlesMaxID.value)
    const nextArticles = result.articles || []
    articles.value = mergeArticles(articles.value, nextArticles)
    setArticleCursorFromArticles(articles.value, result.max_id)
    refreshHasMoreArticles(Boolean(result.is_more), nextArticles.length)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    articleListLoading.value = false
  }
}

const handleArticleListScroll = () => {
  if (isNearScrollBottom(articleListRef.value, 120)) {
    void loadMoreArticles()
  }
}

const handleArticleReaderScroll = () => {
  if (isNearScrollBottom(articleReaderRef.value, 260)) {
    void appendNextReadingArticle()
  }
}

const searchCourseArticles = async () => {
  articlePage.value = 1
  articleSearchStatus.value = ''
  if (!normalizedArticleSearch.value) {
    return
  }
  articleSearchLoading.value = true
  try {
    await loadAllArticlePages()
    articleSearchStatus.value = hasMoreArticles.value ? '已搜索已载入目录' : '已搜索全部目录'
  } finally {
    articleSearchLoading.value = false
  }
}

const loadAllArticlePages = async () => {
  let guard = 0
  while (hasMoreArticles.value && guard < 80) {
    guard += 1
    const beforeCount = articles.value.length
    await loadMoreArticles()
    if (articles.value.length === beforeCount) {
      break
    }
  }
}

const shouldKeepLoadingArticles = () => {
  const total = detail.value?.course.article_count || 0
  return total > 0 && articles.value.length > 0 && articles.value.length < total
}

const refreshHasMoreArticles = (serverHasMore: boolean, lastLoadedCount = articles.value.length) => {
  hasMoreArticles.value = Boolean(serverHasMore || (lastLoadedCount > 0 && shouldKeepLoadingArticles()))
}

const goToPreviousArticlePage = () => {
  articlePage.value = Math.max(1, articlePage.value - 1)
}

const goToNextArticlePage = async () => {
  if (canGoNextLoadedArticlePage.value) {
    articlePage.value += 1
    return
  }
  if (!hasMoreArticles.value) {
    return
  }
  const previousLoadedPages = loadedArticlePages.value
  await loadMoreArticles()
  if (loadedArticlePages.value > previousLoadedPages || canGoNextLoadedArticlePage.value) {
    articlePage.value += 1
  }
}

const appendNextReadingArticle = async () => {
  if (articleLoading.value || continuousArticleLoading.value) {
    return
  }
  let nextArticle = nextReadingArticle.value
  if (!nextArticle && hasMoreArticles.value) {
    const beforeCount = articles.value.length
    await loadMoreArticles()
    if (articles.value.length > beforeCount) {
      nextArticle = nextReadingArticle.value
    }
  }
  const nextArticleEnid = nextArticle?.enid || ''
  if (!nextArticle || !nextArticleEnid || readingEntries.value.some((entry) => entry.enid === nextArticleEnid)) {
    return
  }
  continuousArticleLoading.value = true
  try {
    await appendArticleToReadingStream(nextArticle)
  } finally {
    continuousArticleLoading.value = false
    await fillReaderIfNeeded()
  }
}

const setArticleCursorFromArticles = (nextArticles: DedaoArticle[] = articles.value, explicitMaxID = 0) => {
  if (explicitMaxID > 0) {
    articlesMaxID.value = explicitMaxID
    return
  }
  const lastArticle = [...nextArticles].reverse().find((article) => article.id > 0)
  articlesMaxID.value = lastArticle?.id || 0
}

const mergeArticles = (currentArticles: DedaoArticle[], nextArticles: DedaoArticle[]) => {
  const seen = new Set<string>()
  const merged: DedaoArticle[] = []
  for (const article of [...currentArticles, ...nextArticles]) {
    const key = article.enid || String(article.id)
    if (!key || seen.has(key)) {
      continue
    }
    seen.add(key)
    merged.push(article)
  }
  return merged
}

const openArticle = async (article: DedaoArticle) => {
  if (!article.enid) {
    return
  }
  await startReadingFromArticle(article)
}

const startReadingFromArticle = async (article: DedaoArticle) => {
  selectedArticleEnid.value = article.enid
  selectedArticleTitle.value = article.title
  markdown.value = ''
  readingEntries.value = []
  await nextTick()
  articleReaderRef.value?.scrollTo({ top: 0 })
  await appendArticleToReadingStream(article, true)
  await fillReaderIfNeeded()
}

const appendArticleToReadingStream = async (article: DedaoArticle, replace = false) => {
  if (!article.enid) {
    return
  }
  const loadingEntry = createReadingEntry(article, true)
  if (replace) {
    readingEntries.value = [loadingEntry]
  } else {
    readingEntries.value = [...readingEntries.value, loadingEntry]
  }
  selectedArticleEnid.value = article.enid
  selectedArticleTitle.value = article.title
  articleLoading.value = true
  articleError.value = ''
  try {
    const result = await client.value.getDedaoArticleMarkdown(article.enid)
    const resolvedTitle = result.title || article.title
    markdown.value = result.markdown || ''
    selectedArticleTitle.value = resolvedTitle
    updateReadingEntry(article.enid, {
      title: resolvedTitle,
      markdown: result.markdown || '',
      loading: false,
      error: '',
    })
  } catch (error) {
    markdown.value = ''
    const message = error instanceof Error ? error.message : String(error)
    articleError.value = message
    updateReadingEntry(article.enid, {
      loading: false,
      error: message,
    })
  } finally {
    articleLoading.value = false
  }
}

const fillReaderIfNeeded = async () => {
  await nextTick()
  if (isNearScrollBottom(articleReaderRef.value, 260)) {
    void appendNextReadingArticle()
  }
}

const createReadingEntry = (article: DedaoArticle, loading = false): CourseReadingEntry => ({
  enid: article.enid,
  id: article.id,
  orderNum: article.order_num,
  title: article.title || `Article ${article.id}`,
  markdown: '',
  loading,
  error: '',
})

const updateReadingEntry = (entryEnid: string, patch: Partial<CourseReadingEntry>) => {
  readingEntries.value = readingEntries.value.map((entry) => (
    entry.enid === entryEnid ? { ...entry, ...patch } : entry
  ))
}

const findArticleIndex = (articleEnid: string) => articles.value.findIndex((article) => article.enid === articleEnid)

const isNearScrollBottom = (
  element: { scrollTop: number; clientHeight: number; scrollHeight: number } | null,
  threshold: number,
) => {
  if (!element) {
    return false
  }
  return element.scrollTop + element.clientHeight >= element.scrollHeight - threshold
}

const formatPublishTime = (value?: number) => {
  if (!value) {
    return '未发布'
  }
  return new Date(value * 1000).toLocaleDateString()
}

const compactLines = (lines: Array<string | number | undefined | null>) =>
  lines
    .map((line) => String(line ?? '').trim())
    .filter(Boolean)
    .join('\n')

const openContextPanel = (panel: 'Course' | 'Analysis') => {
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
  const rect = readerWorkspaceRef.value?.getBoundingClientRect()
  if (!target || !rect) {
    return
  }
  if (target === 'left') {
    layoutColumns.value = {
      ...layoutColumns.value,
      left: clampNumber(event.clientX - rect.left, 196, 360),
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
</script>

<style scoped>
.course-detail-reader {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: calc(100vh - 92px);
  margin-top: 8px;
}

.reader-toolbar,
.article-rail,
.article-reader,
.course-context {
  border: 1px solid var(--dedao-line);
  border-radius: 10px;
  background: #ffffff;
  box-shadow: none;
}

.reader-toolbar {
  display: grid;
  grid-template-columns: 76px minmax(240px, 1fr) 88px;
  gap: 12px;
  align-items: center;
  padding: 12px;
}

.reader-workspace {
  position: relative;
  display: grid;
  grid-template-columns: var(--course-left-column, 248px) 8px minmax(760px, 1fr);
  gap: 0;
  min-height: calc(100vh - 196px);
}

.article-rail,
.article-reader,
.course-context {
  min-width: 0;
  padding: 12px;
}

.article-list {
  display: grid;
  gap: 0;
  max-height: calc(100vh - 390px);
  overflow: auto;
}

.article-search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 62px;
  gap: 8px;
  margin-bottom: 8px;
}

.article-search button,
.article-pagination button {
  min-height: 36px;
  border: 1px solid var(--dedao-line);
  border-radius: 6px;
  background: #ffffff;
  color: var(--dedao-text);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}

.article-search button {
  border-color: var(--dedao-orange);
  background: var(--dedao-orange);
  color: #ffffff;
}

.article-list-meta,
.article-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
  color: var(--dedao-muted);
  font-size: 12px;
}

.article-pagination {
  margin: 10px 0 0;
}

.article-pagination span {
  color: var(--dedao-text);
  font-weight: 700;
}

.article-row {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  width: 100%;
  padding: 10px 0;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  background: #ffffff;
  color: var(--dedao-text);
  text-align: left;
  cursor: pointer;
}

.article-row.active {
  color: var(--dedao-orange);
  background: #fffaf6;
}

.article-row strong,
.article-reader h1,
.course-context h2 {
  overflow-wrap: anywhere;
}

.article-row strong {
  font-size: 13px;
  line-height: 18px;
}

.article-row small {
  display: block;
  margin-top: 3px;
  color: var(--dedao-muted);
  font-size: 11px;
}

.article-reader {
  margin-left: 10px;
  max-height: calc(100vh - 196px);
  overflow: auto;
}

.article-reader-head,
.panel-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: start;
  margin-bottom: 12px;
}

.course-reader-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  min-width: 240px;
}

.course-reader-actions button {
  min-height: 30px;
  border: 1px solid var(--dedao-line);
  border-radius: 6px;
  padding: 0 10px;
  background: #ffffff;
  color: var(--dedao-text);
  font-size: 12px;
  font-weight: 700;
}

.course-reader-actions button:hover,
.course-reader-actions button.active {
  border-color: var(--dedao-orange);
  color: var(--dedao-orange);
}

.article-reader h1 {
  margin: 2px 0 0;
  font-size: 24px;
  line-height: 1.25;
}

.continuous-article-stream {
  display: grid;
  gap: 22px;
}

.reading-entry {
  border-bottom: 1px solid var(--dedao-line);
  padding-bottom: 18px;
}

.reading-entry:last-child {
  border-bottom: 0;
}

.reading-entry-head {
  display: grid;
  grid-template-columns: 42px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  margin: 0 0 12px;
}

.reading-entry-head span {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 28px;
  border: 1px solid #ffd0ad;
  border-radius: 999px;
  color: var(--dedao-orange);
  font-size: 12px;
  font-weight: 800;
}

.reading-entry-head h2 {
  margin: 0;
  color: #111111;
  font-size: 24px;
  line-height: 1.28;
}

.reader-end-state {
  border-top: 1px solid var(--dedao-line);
  padding: 18px 0 8px;
  color: var(--dedao-muted);
  text-align: center;
}

.answer-markdown {
  color: #222222;
  line-height: 1.72;
}

.answer-markdown :deep(h1),
.answer-markdown :deep(h2),
.answer-markdown :deep(h3) {
  margin: 18px 0 8px;
  color: #111111;
  line-height: 1.25;
}

.answer-markdown :deep(p) {
  margin: 10px 0;
}

.course-context p {
  margin: 10px 0 0;
  color: #666666;
  line-height: 1.6;
}

.course-context-drawer {
  position: absolute;
  top: 0;
  right: 0;
  z-index: 20;
  width: min(400px, calc(100vw - var(--course-left-column, 248px) - 48px));
  max-height: calc(100vh - 112px);
  overflow: auto;
  box-shadow: 0 18px 48px rgb(0 0 0 / 12%);
}

.context-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.course-intro.muted {
  color: var(--dedao-muted);
}

.context-tabs {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 12px 0;
}

.context-tabs button {
  min-height: 34px;
  border: 1px solid var(--dedao-line);
  border-radius: 6px;
  background: #ffffff;
  color: var(--dedao-text);
  font-weight: 700;
}

.context-tabs button.active,
.context-tabs button:hover {
  border-color: var(--dedao-orange);
  color: var(--dedao-orange);
}

.course-context dl {
  display: grid;
  gap: 0;
  margin: 12px 0 0;
  border-top: 1px solid var(--dedao-line);
}

.course-context dt {
  color: var(--dedao-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.course-context dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
  color: #222222;
}

.course-context dl div {
  border-bottom: 1px solid var(--dedao-line);
  padding: 8px 0;
}

.back-link,
.primary-action,
.secondary-action {
  min-height: 38px;
  border: 1px solid var(--dedao-orange);
  border-radius: 6px;
  background: var(--dedao-orange);
  color: #ffffff;
  font-weight: 700;
  text-decoration: none;
  cursor: pointer;
}

.back-link,
.secondary-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #ffffff;
  color: var(--dedao-orange);
}

.secondary-action {
  width: 100%;
  margin-top: 10px;
}

.text-action {
  min-height: 28px;
  border: 1px solid #ffd0ad;
  border-radius: 999px;
  padding: 0 10px;
  background: #ffffff;
  color: var(--dedao-orange);
  font-size: 12px;
  font-weight: 700;
}

label {
  display: grid;
  gap: 4px;
  color: var(--dedao-muted);
  font-size: 12px;
  font-weight: 700;
}

input {
  min-width: 0;
  height: 38px;
  border: 1px solid var(--dedao-border);
  border-radius: 6px;
  padding: 0 10px;
  color: var(--dedao-text);
}

.eyebrow {
  color: var(--dedao-muted);
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
}

.brand-block h2,
.panel-head h2 {
  margin: 2px 0 0;
  color: #111111;
  font-size: 20px;
  line-height: 1.15;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 28px;
  padding: 0 10px;
  border: 1px solid var(--dedao-border);
  border-radius: 999px;
  color: var(--dedao-muted);
  font-size: 12px;
  font-weight: 700;
}

.status-pill.ok {
  border-color: #ffd0ad;
  color: var(--dedao-orange);
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

button:disabled {
  opacity: 0.58;
  cursor: not-allowed;
}

@media (max-width: 980px) {
  .reader-toolbar,
  .reader-workspace {
    grid-template-columns: 1fr;
  }

  .column-resizer {
    display: none;
  }

  .article-reader {
    margin: 0;
  }

  .course-context-drawer {
    position: static;
    width: auto;
    max-height: none;
  }

  .article-list {
    max-height: none;
  }
}
</style>
