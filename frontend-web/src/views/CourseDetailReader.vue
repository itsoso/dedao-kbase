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

    <section class="reader-workspace">
      <aside class="article-rail">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Articles</span>
            <h2>课程目录</h2>
          </div>
          <span>{{ articles.length }}</span>
        </div>

        <div class="article-list">
          <button
            v-for="article in articles"
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

        <button v-if="hasMoreArticles" class="secondary-action" type="button" :disabled="articleListLoading" @click="loadMoreArticles">
          {{ articleListLoading ? '加载中' : '加载更多' }}
        </button>

        <div v-if="!loading && !articles.length" class="empty-state">暂无课程文章。</div>
      </aside>

      <article class="article-reader">
        <div class="article-reader-head">
          <div>
            <span class="eyebrow">Markdown Reader</span>
            <h1>{{ selectedArticleTitle || '选择一篇文章' }}</h1>
          </div>
          <span class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</span>
        </div>

        <div v-if="articleError" class="error-strip">{{ articleError }}</div>
        <div v-if="articleLoading" class="empty-state">加载文章中...</div>
        <div v-else-if="renderedArticle" class="answer-markdown" v-html="renderedArticle"></div>
        <div v-else class="empty-state">从左侧选择文章开始阅读。</div>
      </article>

      <aside class="course-context">
        <span class="eyebrow">Course</span>
        <h2>{{ detail?.course.title || '课程详情' }}</h2>
        <p>{{ detail?.course.highlight || detail?.course.intro || '暂无简介' }}</p>
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
        <PageAnalysisPanel
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
import { computed, onMounted, ref } from 'vue'
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

const storageKey = 'dedao-kbase-web-settings'
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
const articlesMaxID = ref(0)
const hasMoreArticles = ref(false)

const enid = computed(() => String(route.params.enid || ''))
const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const renderedArticle = computed(() => renderMarkdown(markdown.value))
const coursePageURL = computed(() => `/course/${encodeURIComponent(enid.value)}`)
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
  try {
    await hydrateBrowserSession()
    await loadDetail()
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
    hasMoreArticles.value = Boolean(result.has_more)
    connected.value = true
    const firstArticle = articles.value[0]
    if (firstArticle) {
      await openArticle(firstArticle)
    } else {
      selectedArticleEnid.value = ''
      selectedArticleTitle.value = ''
      markdown.value = ''
    }
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const loadMoreArticles = async () => {
  articleListLoading.value = true
  errorMessage.value = ''
  try {
    const result = await client.value.listDedaoCourseArticles(enid.value, 30, articlesMaxID.value)
    articles.value = [...articles.value, ...(result.articles || [])]
    articlesMaxID.value = result.max_id || articlesMaxID.value
    hasMoreArticles.value = Boolean(result.is_more)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    articleListLoading.value = false
  }
}

const openArticle = async (article: DedaoArticle) => {
  if (!article.enid) {
    return
  }
  selectedArticleEnid.value = article.enid
  selectedArticleTitle.value = article.title
  articleLoading.value = true
  articleError.value = ''
  try {
    const result = await client.value.getDedaoArticleMarkdown(article.enid)
    markdown.value = result.markdown || ''
    selectedArticleTitle.value = result.title || article.title
  } catch (error) {
    markdown.value = ''
    articleError.value = error instanceof Error ? error.message : String(error)
  } finally {
    articleLoading.value = false
  }
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
</script>

<style scoped>
.course-detail-reader {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: calc(100vh - 156px);
  margin-top: 12px;
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
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr) 320px;
  gap: 12px;
  min-height: calc(100vh - 260px);
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
  max-height: calc(100vh - 360px);
  overflow: auto;
}

.article-row {
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  width: 100%;
  padding: 11px 0;
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

.article-row small {
  display: block;
  margin-top: 4px;
  color: var(--dedao-muted);
}

.article-reader {
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

.article-reader h1 {
  margin: 2px 0 0;
  font-size: 24px;
  line-height: 1.25;
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
  color: #666666;
  line-height: 1.6;
}

.course-context dl {
  display: grid;
  gap: 10px;
}

.course-context dt {
  color: var(--dedao-muted);
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.course-context dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
  color: #222222;
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
  font-size: 22px;
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

  .article-list {
    max-height: none;
  }
}
</style>
