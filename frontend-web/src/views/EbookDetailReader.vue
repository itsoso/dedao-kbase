<template>
  <main class="ebook-detail-reader">
    <section class="reader-toolbar">
      <RouterLink class="back-link" to="/ebook">书架</RouterLink>
      <div class="brand-block">
        <span class="eyebrow">Ebook Study</span>
        <h2>{{ detail?.title || '电子书阅读' }}</h2>
      </div>

      <label>
        <span>Base URL</span>
        <input v-model="baseUrl" name="ebookDetailBaseUrl" placeholder="http://127.0.0.1:8719" />
      </label>

      <label>
        <span>Token</span>
        <input v-model="token" name="ebookDetailToken" type="password" placeholder="KBASE_AUTH_TOKEN" />
      </label>

      <button class="primary-action" type="button" :disabled="loading" @click="loadDetail">
        {{ loading ? '加载中' : '刷新' }}
      </button>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="reader-workspace">
      <aside class="catalog-rail">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Catalog</span>
            <h2>目录</h2>
          </div>
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

        <div v-if="!loading && !(detail?.catalog.length)" class="empty-state">暂无目录。</div>
      </aside>

      <article class="ebook-reader">
        <div class="ebook-reader-head">
          <div>
            <span class="eyebrow">SVG Reader</span>
            <h1>{{ selectedChapterTitle || detail?.title || '选择章节' }}</h1>
          </div>
          <span class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</span>
        </div>

        <div v-if="pageError" class="error-strip">{{ pageError }}</div>
        <div v-if="pageLoading" class="empty-state">加载页面中...</div>
        <div v-else-if="svgFrames.length" class="ebook-pages">
          <iframe
            v-for="frame in svgFrames"
            :key="frame.key"
            class="ebook-page-frame"
            title="ebook page"
            sandbox=""
            :srcdoc="frame.srcdoc"
          ></iframe>
        </div>
        <div v-else class="empty-state">从左侧选择章节开始阅读。</div>

        <div v-if="pageResponse" class="page-actions">
          <button type="button" class="secondary-action" :disabled="pageLoading || pageResponse.index <= 0" @click="loadRelativePage(-1)">
            Prev
          </button>
          <span>Page {{ pageResponse.index + 1 }} · {{ pageResponse.pages.length }} loaded</span>
          <button type="button" class="secondary-action" :disabled="pageLoading || pageResponse.is_end" @click="loadRelativePage(1)">
            Next
          </button>
        </div>
      </article>

      <aside class="ebook-context">
        <span class="eyebrow">Book</span>
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
          <div>
            <dt>ENID</dt>
            <dd>{{ enid }}</dd>
          </div>
        </dl>
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
  type DedaoEbookCatalogItem,
  type DedaoEbookChapterPages,
  type DedaoEbookDetail,
} from '../api'

const storageKey = 'dedao-kbase-web-settings'
const route = useRoute()

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loading = ref(false)
const pageLoading = ref(false)
const errorMessage = ref('')
const pageError = ref('')
const detail = ref<DedaoEbookDetail | null>(null)
const selectedChapterID = ref('')
const selectedChapterTitle = ref('')
const pageResponse = ref<DedaoEbookChapterPages | null>(null)

const enid = computed(() => String(route.params.enid || ''))
const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const svgFrames = computed(() =>
  (pageResponse.value?.pages || []).map((page) => ({
    key: `${page.page_num}-${page.begin_offset}-${page.end_offset}`,
    srcdoc: svgToSrcdoc(page.svg),
  })),
)

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
  pageError.value = ''
  try {
    await hydrateBrowserSession()
    saveConnection()
    detail.value = await client.value.getDedaoEbookDetail(enid.value)
    connected.value = true
    const firstReadable = detail.value.catalog.find((item) => item.chapter_id)
    if (firstReadable) {
      await openChapter(firstReadable)
    }
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

const openChapter = async (item: DedaoEbookCatalogItem) => {
  if (!item.chapter_id) {
    return
  }
  selectedChapterID.value = item.chapter_id
  selectedChapterTitle.value = item.text
  await loadChapterPages(item.chapter_id, 0)
}

const loadRelativePage = async (direction: -1 | 1) => {
  if (!selectedChapterID.value || !pageResponse.value) {
    return
  }
  const nextIndex = Math.max(0, pageResponse.value.index + direction * pageResponse.value.count)
  await loadChapterPages(selectedChapterID.value, nextIndex)
}

const loadChapterPages = async (chapterID: string, index: number) => {
  pageLoading.value = true
  pageError.value = ''
  try {
    pageResponse.value = await client.value.getDedaoEbookChapterPages(enid.value, chapterID, index, 8, 0)
  } catch (error) {
    pageResponse.value = null
    pageError.value = error instanceof Error ? error.message : String(error)
  } finally {
    pageLoading.value = false
  }
}

const catalogKey = (item: DedaoEbookCatalogItem) => `${item.chapter_id || item.href || item.text}-${item.play_order || 0}`

const svgToSrcdoc = (svg: string) => `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <style>
      html, body {
        margin: 0;
        min-height: 100%;
        background: #ffffff;
      }
      body {
        display: grid;
        place-items: start center;
        padding: 12px;
        box-sizing: border-box;
      }
      svg {
        max-width: 100%;
        height: auto;
      }
    </style>
  </head>
  <body>${svg || ''}</body>
</html>`
</script>

<style scoped>
.ebook-detail-reader {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: calc(100vh - 156px);
  margin-top: 12px;
}

.reader-toolbar,
.catalog-rail,
.ebook-reader,
.ebook-context {
  border: 1px solid #d3dce8;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 10px 24px rgb(37 51 74 / 8%);
}

.reader-toolbar {
  display: grid;
  grid-template-columns: 76px minmax(240px, 1fr) minmax(220px, 320px) minmax(220px, 320px) 88px;
  gap: 12px;
  align-items: end;
  padding: 12px;
}

.reader-workspace {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr) 260px;
  gap: 12px;
  min-height: calc(100vh - 260px);
}

.catalog-rail,
.ebook-reader,
.ebook-context {
  min-width: 0;
  padding: 12px;
}

.panel-head,
.ebook-reader-head,
.page-actions {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.catalog-list {
  display: grid;
  gap: 8px;
  max-height: calc(100vh - 340px);
  overflow: auto;
}

.catalog-row {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  width: 100%;
  min-height: 44px;
  border: 1px solid #d9e2ee;
  border-radius: 8px;
  background: #f8fafc;
  color: #172033;
  text-align: left;
  cursor: pointer;
}

.catalog-row.active {
  border-color: #3c7f73;
  background: #eaf5f1;
}

.catalog-row:disabled {
  color: #8491a6;
  cursor: not-allowed;
}

.catalog-row strong,
.ebook-reader h1,
.ebook-context h2 {
  overflow-wrap: anywhere;
}

.ebook-reader {
  overflow: auto;
}

.ebook-reader h1 {
  margin: 2px 0 0;
  color: #172033;
  font-size: 24px;
  line-height: 1.25;
}

.ebook-pages {
  display: grid;
  gap: 12px;
}

.ebook-page-frame {
  width: 100%;
  min-height: 720px;
  border: 1px solid #d9e2ee;
  border-radius: 8px;
  background: #ffffff;
}

.page-actions {
  align-items: center;
  margin: 12px 0 0;
  color: #61708a;
}

.ebook-context p {
  color: #52627a;
  line-height: 1.6;
}

.ebook-context dl {
  display: grid;
  gap: 10px;
}

.ebook-context dt {
  color: #66758a;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.ebook-context dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
  color: #172033;
}

.back-link,
.primary-action,
.secondary-action {
  min-height: 38px;
  border: 1px solid #3c7f73;
  border-radius: 8px;
  background: #3c7f73;
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
  min-width: 72px;
  padding: 0 12px;
  background: #ffffff;
  color: #275f56;
}

label {
  display: grid;
  gap: 4px;
  color: #52627a;
  font-size: 12px;
  font-weight: 700;
}

input {
  min-width: 0;
  height: 38px;
  border: 1px solid #cdd8e6;
  border-radius: 8px;
  padding: 0 10px;
  color: #172033;
}

.eyebrow {
  color: #607089;
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
}

.brand-block h2,
.panel-head h2 {
  margin: 2px 0 0;
  color: #172033;
  font-size: 22px;
  line-height: 1.15;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 28px;
  padding: 0 10px;
  border: 1px solid #d3dce8;
  border-radius: 999px;
  color: #66758a;
  font-size: 12px;
  font-weight: 700;
}

.status-pill.ok {
  border-color: #3c7f73;
  color: #275f56;
}

.error-strip,
.empty-state {
  border: 1px dashed #c9d5e5;
  border-radius: 8px;
  padding: 14px;
  color: #61708a;
  background: #fbfcfe;
}

.error-strip {
  border-color: #e2aaa2;
  color: #8a3025;
  background: #fff7f5;
}

button:disabled {
  opacity: 0.58;
}

@media (max-width: 980px) {
  .reader-toolbar,
  .reader-workspace {
    grid-template-columns: 1fr;
  }

  .catalog-list {
    max-height: none;
  }

  .ebook-page-frame {
    min-height: 560px;
  }
}
</style>
