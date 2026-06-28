<template>
  <main class="course-library">
    <section class="course-toolbar">
      <div class="brand-block">
        <span class="eyebrow">Dedao Courses</span>
        <h2>课程</h2>
      </div>

      <label>
        <span>Base URL</span>
        <input v-model="baseUrl" name="courseBaseUrl" placeholder="http://127.0.0.1:8719" />
      </label>

      <label>
        <span>Token</span>
        <input v-model="token" name="courseToken" type="password" placeholder="KBASE_AUTH_TOKEN" />
      </label>

      <button class="primary-action" type="button" :disabled="loading" @click="reloadFromFirstPage">
        {{ loading ? '加载中' : '刷新课程' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="course-workspace">
      <aside class="course-filter-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Search</span>
            <h2>筛选课程</h2>
          </div>
        </div>

        <div class="course-filter-stack">
          <input v-model="query" placeholder="输入课程名、讲师或简介关键词" @keydown.enter="reloadFromFirstPage" />
          <select v-model.number="pageSize" @change="reloadFromFirstPage">
            <option :value="10">10/page</option>
            <option :value="15">15/page</option>
            <option :value="30">30/page</option>
            <option :value="50">50/page</option>
          </select>
          <button type="button" class="primary-action" :disabled="loading" @click="reloadFromFirstPage">Search</button>
        </div>

        <dl class="course-metrics">
          <div>
            <dt>Total</dt>
            <dd>{{ total }}</dd>
          </div>
          <div>
            <dt>Page</dt>
            <dd>{{ page }} / {{ totalPages || 1 }}</dd>
          </div>
          <div>
            <dt>Loaded</dt>
            <dd>{{ courses.length }}</dd>
          </div>
        </dl>
      </aside>

      <section class="course-list-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Purchased Courses</span>
            <h2>已购课程</h2>
          </div>
          <span class="course-source">CourseList("bauhinia", "study")</span>
        </div>

        <div class="course-list">
          <button
            v-for="course in courses"
            :key="courseKey(course)"
            type="button"
            class="course-row"
            :class="{ active: selectedKey === courseKey(course) }"
            @click="openCourse(course)"
          >
            <div class="course-cover">
              <img v-if="course.icon" :src="course.icon" alt="" />
              <span v-else>{{ (course.title || '?').slice(0, 1) }}</span>
            </div>
            <div class="course-main">
              <div class="course-title-line">
                <strong>{{ course.title || `Course ${course.class_id || course.id}` }}</strong>
                <span>{{ course.price || '未标价' }}</span>
              </div>
              <p>{{ course.intro || course.author || '暂无简介' }}</p>
              <div class="course-progress-line">
                <div class="progress-track" aria-label="learning progress">
                  <span :style="{ width: `${safeProgress(course.progress)}%` }"></span>
                </div>
                <small>{{ course.publish_num || 0 }}/{{ course.course_num || 0 }} 讲</small>
              </div>
            </div>
            <div class="course-side">
              <span>{{ safeProgress(course.progress) }}%</span>
              <small>{{ course.last_read || '未开始' }}</small>
            </div>
          </button>

          <div v-if="!loading && !courses.length" class="empty-state">
            {{ token ? '当前页没有课程，尝试刷新、换页或重新扫码登录。' : '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。' }}
          </div>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">Prev</button>
          <span>Page {{ page }} / {{ totalPages || 1 }} · {{ total }} courses</span>
          <button type="button" :disabled="!canGoNext || loading" @click="changePage(page + 1)">Next</button>
        </div>
      </section>

      <aside class="course-detail-panel">
        <span class="eyebrow">Study Context</span>
        <h2>{{ selectedCourse?.title || '选择一门课程' }}</h2>
        <p>{{ selectedCourse?.intro || '这里显示当前课程的学习摘要。课程详情、章节列表和下载会在后续切片迁移。' }}</p>
        <dl class="course-detail-list">
          <div>
            <dt>Class ID</dt>
            <dd>{{ selectedCourse?.class_id || '-' }}</dd>
          </div>
          <div>
            <dt>ENID</dt>
            <dd>{{ selectedCourse?.enid || '-' }}</dd>
          </div>
          <div>
            <dt>Author</dt>
            <dd>{{ selectedCourse?.author || '-' }}</dd>
          </div>
          <div>
            <dt>Progress</dt>
            <dd>{{ selectedCourse ? `${safeProgress(selectedCourse.progress)}%` : '-' }}</dd>
          </div>
        </dl>
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getBrowserSession, KBaseClient, type DedaoCourse } from '../api'

const storageKey = 'dedao-kbase-web-settings'
const router = useRouter()

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const errorMessage = ref('')
const query = ref('')
const page = ref(1)
const pageSize = ref(15)
const total = ref(0)
const totalPages = ref(0)
const isMore = ref(0)
const courses = ref<DedaoCourse[]>([])
const selectedKey = ref('')

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const selectedCourse = computed(() => courses.value.find((course) => courseKey(course) === selectedKey.value) || null)
const canGoNext = computed(() => {
  if (totalPages.value > 0) {
    return page.value < totalPages.value
  }
  return isMore.value === 1 || courses.value.length >= pageSize.value
})

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await loadCourses()
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

const loadCourses = async () => {
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
    const result = await client.value.listDedaoCourses(page.value, pageSize.value, query.value)
    courses.value = result.courses || []
    page.value = result.page || page.value
    pageSize.value = result.page_size || pageSize.value
    total.value = result.total || 0
    totalPages.value = result.total_pages || 0
    isMore.value = result.is_more || 0
    connected.value = true
    if (!courses.value.some((course) => courseKey(course) === selectedKey.value)) {
      selectedKey.value = courses.value[0] ? courseKey(courses.value[0]) : ''
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
  await loadCourses()
}

const changePage = async (nextPage: number) => {
  page.value = Math.max(1, nextPage)
  await loadCourses()
}

const selectCourse = (course: DedaoCourse) => {
  selectedKey.value = courseKey(course)
}

const openCourse = (course: DedaoCourse) => {
  selectCourse(course)
  router.push(`/course/${encodeURIComponent(courseKey(course))}`)
}

const courseKey = (course: DedaoCourse) => course.enid || String(course.class_id || course.id)
const safeProgress = (value: number) => Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
</script>

<style scoped>
.course-library {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: calc(100vh - 156px);
  margin-top: 12px;
}

.course-toolbar,
.course-filter-panel,
.course-list-panel,
.course-detail-panel {
  border: 1px solid #d3dce8;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 10px 24px rgb(37 51 74 / 8%);
}

.course-toolbar {
  display: grid;
  grid-template-columns: 220px minmax(260px, 1fr) minmax(260px, 1fr) 120px 96px;
  gap: 12px;
  align-items: end;
  padding: 12px;
}

.course-workspace {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr) 300px;
  gap: 12px;
}

.course-filter-panel,
.course-list-panel,
.course-detail-panel {
  min-width: 0;
  padding: 12px;
}

.course-filter-stack {
  display: grid;
  gap: 8px;
  margin-top: 12px;
}

.course-metrics,
.course-detail-list {
  display: grid;
  gap: 8px;
  margin: 14px 0 0;
}

.course-metrics div,
.course-detail-list div {
  border: 1px solid #dbe2ec;
  border-radius: 7px;
  padding: 9px;
  background: #fbfcfe;
}

.course-metrics dt,
.course-detail-list dt {
  color: #6c7b8f;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.course-metrics dd,
.course-detail-list dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  color: #121926;
  font-size: 14px;
  font-weight: 700;
}

.course-source {
  align-self: center;
  border: 1px solid #dbe2ec;
  border-radius: 999px;
  padding: 6px 9px;
  background: #fbfcfe;
  color: #536274;
  font-size: 12px;
}

.course-list {
  display: grid;
  gap: 8px;
  margin-top: 12px;
}

.course-row {
  display: grid;
  grid-template-columns: 62px minmax(0, 1fr) 96px;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 78px;
  padding: 9px;
  text-align: left;
}

.course-row.active {
  border-color: #397367;
  background: #f1faf6;
}

.course-cover {
  display: grid;
  overflow: hidden;
  width: 62px;
  height: 62px;
  place-items: center;
  border: 1px solid #dbe2ec;
  border-radius: 6px;
  background: #f6f8fb;
  color: #607086;
  font-weight: 800;
}

.course-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.course-main {
  min-width: 0;
}

.course-title-line {
  display: flex;
  min-width: 0;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.course-title-line strong,
.course-title-line span,
.course-main p,
.course-side small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.course-title-line strong {
  color: #121926;
  font-size: 14px;
}

.course-title-line span {
  flex: 0 0 auto;
  color: #397367;
  font-size: 12px;
  font-weight: 700;
}

.course-main p {
  margin: 5px 0 8px;
  color: #536274;
  font-size: 12px;
}

.course-progress-line {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.progress-track {
  overflow: hidden;
  width: 100%;
  height: 6px;
  border-radius: 999px;
  background: #e7edf4;
}

.progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #397367;
}

.course-progress-line small,
.course-side small {
  color: #6c7b8f;
  font-size: 11px;
  font-weight: 700;
}

.course-side {
  display: grid;
  min-width: 0;
  justify-items: end;
  gap: 4px;
  color: #121926;
  font-weight: 800;
}

.course-detail-panel h2 {
  margin: 0;
  color: #121926;
  font-size: 19px;
  line-height: 26px;
}

.course-detail-panel p {
  margin: 8px 0 0;
  color: #536274;
  font-size: 13px;
}

.empty-state {
  border: 1px dashed #c8d2df;
  border-radius: 8px;
  padding: 24px;
  color: #6c7b8f;
  text-align: center;
}
</style>
