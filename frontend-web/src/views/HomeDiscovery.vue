<template>
  <main class="home-discovery">
    <section class="home-hero">
      <div class="hero-copy">
        <span class="eyebrow">Dedao Web GUI</span>
        <h1>{{ greeting }}</h1>
        <p>{{ heroSubtitle }}</p>
      </div>
      <div class="hero-actions">
        <button class="primary-action" type="button" :disabled="loading" @click="loadHome">
          {{ loading ? '刷新中' : '刷新首页' }}
        </button>
        <RouterLink class="secondary-action" to="/user/profile">账号</RouterLink>
      </div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="home-metrics" aria-label="content overview">
      <RouterLink v-for="metric in metrics" :key="metric.label" class="metric-card" :to="metric.to">
        <span>{{ metric.label }}</span>
        <strong>{{ metric.value }}</strong>
        <small>{{ metric.helper }}</small>
      </RouterLink>
    </section>

    <section class="home-layout">
      <section class="today-panel">
        <div class="section-head">
          <div>
            <span class="eyebrow">Continue</span>
            <h2>继续学习</h2>
          </div>
          <RouterLink to="/book-knowledge">书籍知识库</RouterLink>
        </div>

        <article v-if="heroItem" class="focus-card" role="button" tabindex="0" @click="openLearningItem(heroItem)" @keydown.enter.prevent="openLearningItem(heroItem)">
          <div class="focus-cover">
            <img v-if="heroItem.icon" :src="heroItem.icon" alt="" />
            <span v-else>{{ heroItem.title.slice(0, 1) }}</span>
          </div>
          <div class="focus-main">
            <span>{{ heroItem.kindLabel }}</span>
            <h3>{{ heroItem.title }}</h3>
            <p>{{ heroItem.summary }}</p>
            <div class="progress-track">
              <span :style="{ width: `${heroItem.progress}%` }"></span>
            </div>
          </div>
          <strong>{{ heroItem.progress }}%</strong>
        </article>

        <div class="learning-list">
          <button v-for="item in continueItems" :key="item.key" type="button" class="learning-row" @click="openLearningItem(item)">
            <span>{{ item.kindLabel }}</span>
            <strong>{{ item.title }}</strong>
            <small>{{ item.meta }}</small>
          </button>
          <div v-if="!loading && !continueItems.length" class="empty-state">
            {{ token ? '暂未载入学习内容，尝试刷新或检查得到登录状态。' : '缺少 KBASE_AUTH_TOKEN，登录后首页会自动填充。' }}
          </div>
        </div>
      </section>

      <aside class="home-side">
        <section class="side-card account-card">
          <span class="eyebrow">Account</span>
          <div class="account-row">
            <div class="avatar">
              <img v-if="session?.active_user?.avatar" :src="session.active_user.avatar" alt="" />
              <span v-else>{{ accountName.slice(0, 1) }}</span>
            </div>
            <div>
              <h2>{{ accountName }}</h2>
              <p>{{ session?.logged_in ? '得到账号已登录' : '需要扫码登录' }}</p>
            </div>
          </div>
          <RouterLink class="secondary-action full" :to="session?.logged_in ? '/user/profile' : '/user/login'">
            {{ session?.logged_in ? '查看个人中心' : '扫码登录' }}
          </RouterLink>
        </section>

        <section class="side-card">
          <span class="eyebrow">Workbench</span>
          <h2>学习工作台</h2>
          <p>围绕书籍、课程和听书继续阅读，并把重要内容沉淀到知识库。</p>
          <div class="shortcut-grid">
            <RouterLink to="/course">课程</RouterLink>
            <RouterLink to="/ebook">电子书</RouterLink>
            <RouterLink to="/odob">听书</RouterLink>
            <RouterLink to="/book-knowledge">KBase</RouterLink>
          </div>
        </section>

        <section class="side-card">
          <span class="eyebrow">Jobs</span>
          <h2>最近任务</h2>
          <div class="job-list">
            <article v-for="job in jobs.slice(0, 4)" :key="job.id" class="job-chip">
              <span :class="job.status">{{ job.status }}</span>
              <strong>{{ job.type }}</strong>
            </article>
            <p v-if="!jobs.length">暂无线上任务。</p>
          </div>
        </section>
      </aside>
    </section>

    <section class="content-bands">
      <article class="content-band">
        <div class="section-head">
          <h2>课程</h2>
          <RouterLink to="/course">全部课程</RouterLink>
        </div>
        <div class="content-row">
          <button v-for="course in courses.slice(0, 4)" :key="courseKey(course)" type="button" class="content-card" @click="router.push(`/course/${encodeURIComponent(courseKey(course))}`)">
            <img v-if="course.icon" :src="course.icon" alt="" />
            <strong>{{ course.title }}</strong>
            <span>{{ course.author || course.intro || '继续学习' }}</span>
          </button>
        </div>
      </article>

      <article class="content-band">
        <div class="section-head">
          <h2>电子书</h2>
          <RouterLink to="/ebook">全部电子书</RouterLink>
        </div>
        <div class="content-row">
          <button v-for="ebook in ebooks.slice(0, 4)" :key="ebookKey(ebook)" type="button" class="content-card" @click="router.push(`/ebook/${encodeURIComponent(ebookKey(ebook))}`)">
            <img v-if="ebook.icon" :src="ebook.icon" alt="" />
            <strong>{{ ebook.title }}</strong>
            <span>{{ ebook.author || ebook.intro || '打开阅读器' }}</span>
          </button>
        </div>
      </article>

      <article class="content-band">
        <div class="section-head">
          <h2>听书</h2>
          <RouterLink to="/odob">全部听书</RouterLink>
        </div>
        <div class="content-row">
          <button v-for="odob in odobs.slice(0, 4)" :key="odobKey(odob)" type="button" class="content-card" @click="router.push('/odob')">
            <img v-if="odob.icon || odob.audio_icon" :src="odob.icon || odob.audio_icon" alt="" />
            <strong>{{ odob.title }}</strong>
            <span>{{ odob.author || odob.audio_title || '收听与沉淀' }}</span>
          </button>
        </div>
      </article>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import {
  getBrowserSession,
  KBaseClient,
  type BookKnowledgeJob,
  type DedaoCourse,
  type DedaoEbook,
  type DedaoOdob,
  type DedaoSession,
} from '../api'

type LearningItem = {
  key: string
  kind: 'course' | 'ebook' | 'odob'
  kindLabel: string
  title: string
  summary: string
  meta: string
  icon?: string
  progress: number
  route: string
}

const storageKey = 'dedao-kbase-web-settings'
const router = useRouter()

const baseUrl = ref(window.location.origin)
const token = ref('')
const loading = ref(false)
const errorMessage = ref('')
const session = ref<DedaoSession | null>(null)
const courses = ref<DedaoCourse[]>([])
const ebooks = ref<DedaoEbook[]>([])
const odobs = ref<DedaoOdob[]>([])
const jobs = ref<BookKnowledgeJob[]>([])
const totals = ref({ courses: 0, ebooks: 0, odobs: 0 })

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const accountName = computed(() => session.value?.active_user?.name || '得到学习者')
const greeting = computed(() => `${accountName.value}，继续学习`)
const heroSubtitle = computed(() => {
  if (!token.value) {
    return '登录后展示你的课程、电子书、听书和知识库任务。'
  }
  return '从最近内容继续，边学边沉淀到书籍知识库。'
})
const metrics = computed(() => [
  { label: '课程', value: totals.value.courses || courses.value.length, helper: `${courses.value.length} loaded`, to: '/course' },
  { label: '电子书', value: totals.value.ebooks || ebooks.value.length, helper: `${ebooks.value.length} loaded`, to: '/ebook' },
  { label: '听书', value: totals.value.odobs || odobs.value.length, helper: `${odobs.value.length} loaded`, to: '/odob' },
  { label: '任务', value: jobs.value.length, helper: 'recent jobs', to: '/book-knowledge' },
])
const continueItems = computed<LearningItem[]>(() => [
  ...courses.value.slice(0, 4).map(courseToLearningItem),
  ...ebooks.value.slice(0, 4).map(ebookToLearningItem),
  ...odobs.value.slice(0, 4).map(odobToLearningItem),
].sort((first, second) => second.progress - first.progress).slice(0, 8))
const heroItem = computed(() => continueItems.value[0] || null)

onMounted(async () => {
  restoreConnection()
  await hydrateBrowserSession()
  if (token.value) {
    await loadHome()
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

const loadHome = async () => {
  if (!token.value) {
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    await hydrateBrowserSession()
    saveConnection()
    const [sessionResult, courseResult, ebookResult, odobResult, jobResult] = await Promise.allSettled([
      client.value.getDedaoSession(),
      client.value.listDedaoCourses(1, 8, ''),
      client.value.listDedaoEbooks(1, 8, ''),
      client.value.listDedaoOdobs(1, 8, ''),
      client.value.listJobs(8),
    ])

    if (sessionResult.status === 'fulfilled') {
      session.value = sessionResult.value
    }
    if (courseResult.status === 'fulfilled') {
      courses.value = courseResult.value.courses || []
      totals.value.courses = courseResult.value.total || courses.value.length
    }
    if (ebookResult.status === 'fulfilled') {
      ebooks.value = ebookResult.value.ebooks || []
      totals.value.ebooks = ebookResult.value.total || ebooks.value.length
    }
    if (odobResult.status === 'fulfilled') {
      odobs.value = odobResult.value.odobs || []
      totals.value.odobs = odobResult.value.total || odobs.value.length
    }
    if (jobResult.status === 'fulfilled') {
      jobs.value = jobResult.value || []
    }

    const failed = [sessionResult, courseResult, ebookResult, odobResult, jobResult].find((result) => result.status === 'rejected')
    if (failed?.status === 'rejected') {
      errorMessage.value = failed.reason instanceof Error ? failed.reason.message : String(failed.reason)
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const courseToLearningItem = (course: DedaoCourse): LearningItem => ({
  key: `course:${courseKey(course)}`,
  kind: 'course',
  kindLabel: '课程',
  title: course.title || '未命名课程',
  summary: course.intro || course.author || '继续学习课程内容',
  meta: course.last_read || `${course.publish_num || 0}/${course.course_num || 0} 讲`,
  icon: course.icon,
  progress: safeProgress(course.progress),
  route: `/course/${encodeURIComponent(courseKey(course))}`,
})

const ebookToLearningItem = (ebook: DedaoEbook): LearningItem => ({
  key: `ebook:${ebookKey(ebook)}`,
  kind: 'ebook',
  kindLabel: '电子书',
  title: ebook.title || '未命名电子书',
  summary: ebook.intro || ebook.author || '打开得到式阅读器',
  meta: ebook.last_read || `${ebook.publish_num || 0} 篇`,
  icon: ebook.icon,
  progress: safeProgress(ebook.progress),
  route: `/ebook/${encodeURIComponent(ebookKey(ebook))}`,
})

const odobToLearningItem = (odob: DedaoOdob): LearningItem => ({
  key: `odob:${odobKey(odob)}`,
  kind: 'odob',
  kindLabel: '听书',
  title: odob.title || '未命名听书',
  summary: odob.intro || odob.author || odob.audio_title || '继续收听并沉淀',
  meta: formatDuration(odob.duration || odob.audio_duration),
  icon: odob.icon || odob.audio_icon,
  progress: safeProgress(odob.progress),
  route: '/odob',
})

const openLearningItem = (item: LearningItem) => {
  router.push(item.route)
}

const courseKey = (course: DedaoCourse) => course.enid || String(course.class_id || course.id)
const ebookKey = (ebook: DedaoEbook) => ebook.enid || String(ebook.id)
const odobKey = (odob?: DedaoOdob | null) => odob?.enid || String(odob?.class_id || odob?.id || '')
const safeProgress = (value: number) => Math.max(0, Math.min(100, Number.isFinite(value) ? Math.round(value) : 0))
const formatDuration = (seconds?: number) => {
  const value = Number(seconds || 0)
  if (!Number.isFinite(value) || value <= 0) {
    return '未开始'
  }
  const minutes = Math.round(value / 60)
  if (minutes < 60) {
    return `${minutes} 分钟`
  }
  return `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分钟`
}
</script>

<style scoped>
.home-discovery {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: calc(100vh - 70px);
  padding-top: 10px;
}

.home-hero,
.today-panel,
.side-card,
.content-band {
  border: 1px solid var(--dedao-line);
  border-radius: 8px;
  background: #ffffff;
}

.home-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 20px;
  align-items: center;
  min-height: 132px;
  padding: 24px 28px;
}

.hero-copy h1 {
  margin: 4px 0 6px;
  color: #111111;
  font-size: 34px;
  line-height: 42px;
}

.hero-copy p,
.side-card p {
  margin: 0;
  color: var(--dedao-muted);
  font-size: 14px;
  line-height: 22px;
}

.hero-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.primary-action,
.secondary-action {
  min-height: 38px;
  border: 1px solid var(--dedao-orange);
  border-radius: 6px;
  padding: 0 14px;
  background: var(--dedao-orange);
  color: #ffffff;
  font-weight: 700;
  text-decoration: none;
}

.secondary-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #ffffff;
  color: var(--dedao-orange);
}

.secondary-action.full {
  width: 100%;
  margin-top: 12px;
}

.home-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.metric-card {
  border: 1px solid var(--dedao-line);
  border-radius: 8px;
  padding: 14px;
  background: #ffffff;
  color: var(--dedao-text);
  text-decoration: none;
}

.metric-card span,
.metric-card small {
  display: block;
  color: var(--dedao-muted);
  font-size: 12px;
  font-weight: 700;
}

.metric-card strong {
  display: block;
  margin: 4px 0;
  color: #111111;
  font-size: 28px;
  line-height: 34px;
}

.home-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 14px;
  align-items: start;
}

.today-panel,
.side-card,
.content-band {
  padding: 16px;
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.section-head h2,
.side-card h2 {
  margin: 0;
  color: #111111;
  font-size: 21px;
  line-height: 28px;
}

.section-head a,
.shortcut-grid a {
  color: var(--dedao-orange);
  text-decoration: none;
  font-size: 13px;
  font-weight: 700;
}

.focus-card {
  display: grid;
  grid-template-columns: 116px minmax(0, 1fr) 74px;
  gap: 16px;
  align-items: center;
  border: 1px solid #ffe0c7;
  border-radius: 8px;
  padding: 14px;
  background: #fffaf6;
  cursor: pointer;
}

.focus-cover,
.content-card img,
.avatar {
  overflow: hidden;
  background: var(--dedao-subtle);
}

.focus-cover {
  display: grid;
  width: 116px;
  height: 116px;
  place-items: center;
  border-radius: 8px;
  color: var(--dedao-orange);
  font-size: 36px;
  font-weight: 800;
}

.focus-cover img,
.content-card img,
.avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.focus-main {
  min-width: 0;
}

.focus-main span,
.eyebrow {
  color: var(--dedao-muted);
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
}

.focus-main h3 {
  margin: 4px 0 6px;
  color: #111111;
  font-size: 24px;
  line-height: 30px;
}

.focus-main p {
  margin: 0 0 12px;
  color: #666666;
  line-height: 22px;
}

.progress-track {
  overflow: hidden;
  height: 7px;
  border-radius: 999px;
  background: #eeeeee;
}

.progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--dedao-orange);
}

.focus-card > strong {
  justify-self: end;
  color: #111111;
  font-size: 28px;
}

.learning-list {
  display: grid;
  gap: 0;
  margin-top: 8px;
}

.learning-row {
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr) 110px;
  gap: 10px;
  align-items: center;
  min-height: 54px;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 8px 0;
  background: #ffffff;
  text-align: left;
}

.learning-row span {
  color: var(--dedao-orange);
  font-size: 12px;
  font-weight: 800;
}

.learning-row strong,
.content-card strong,
.job-chip strong {
  overflow: hidden;
  color: #222222;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.learning-row small {
  overflow: hidden;
  color: var(--dedao-muted);
  text-align: right;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.home-side {
  display: grid;
  gap: 12px;
}

.account-row {
  display: grid;
  grid-template-columns: 56px minmax(0, 1fr);
  gap: 12px;
  align-items: center;
  margin-top: 8px;
}

.avatar {
  display: grid;
  width: 56px;
  height: 56px;
  place-items: center;
  border-radius: 8px;
  color: var(--dedao-orange);
  font-size: 22px;
  font-weight: 800;
}

.account-row h2 {
  margin: 0;
  font-size: 18px;
}

.shortcut-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin-top: 12px;
}

.shortcut-grid a {
  border: 1px solid var(--dedao-line);
  border-radius: 6px;
  padding: 9px;
  background: var(--dedao-subtle);
  color: var(--dedao-text);
  text-align: center;
}

.job-list {
  display: grid;
  gap: 8px;
  margin-top: 10px;
}

.job-chip {
  display: grid;
  grid-template-columns: 74px minmax(0, 1fr);
  gap: 8px;
  align-items: center;
}

.job-chip span {
  border: 1px solid var(--dedao-border);
  border-radius: 999px;
  padding: 3px 6px;
  color: var(--dedao-muted);
  text-align: center;
  font-size: 10px;
  font-weight: 800;
  text-transform: uppercase;
}

.job-chip span.running,
.job-chip span.queued {
  border-color: #b8c9e4;
  background: #eef5ff;
  color: #2f5f92;
}

.job-chip span.succeeded {
  border-color: #a7d1bd;
  background: #effaf4;
  color: #257347;
}

.job-chip span.failed {
  border-color: #e5b8ac;
  background: #fff4f1;
  color: #9b3f2e;
}

.content-bands {
  display: grid;
  gap: 14px;
}

.content-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.content-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  border: 0;
  border-radius: 8px;
  padding: 0;
  background: transparent;
  text-align: left;
}

.content-card img {
  width: 100%;
  aspect-ratio: 16 / 9;
  border: 1px solid var(--dedao-line);
  border-radius: 8px;
}

.content-card span {
  overflow: hidden;
  color: var(--dedao-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.empty-state {
  border: 1px dashed var(--dedao-border);
  border-radius: 8px;
  padding: 22px;
  color: var(--dedao-muted);
  text-align: center;
}

@media (max-width: 980px) {
  .home-hero,
  .home-layout,
  .home-metrics,
  .content-row {
    grid-template-columns: 1fr;
  }

  .focus-card,
  .learning-row {
    grid-template-columns: 1fr;
  }

  .learning-row small {
    text-align: left;
  }
}
</style>
