<template>
  <main class="knowledge-city">
    <section class="knowledge-toolbar">
      <div class="toolbar-summary">
        <strong>知识城邦</strong>
        <span>{{ topics.length }} 个话题 · {{ selectedTopic?.notes_count || 0 }} 条讨论</span>
      </div>
      <button class="primary-action" type="button" :disabled="loadingTopics" @click="reloadTopics">
        {{ loadingTopics ? '加载中' : '刷新' }}
      </button>
      <div class="status-pill" :class="{ ok: connected }">{{ connected ? '已连接' : '未连接' }}</div>
    </section>

    <section v-if="errorMessage" class="error-strip">{{ errorMessage }}</section>

    <section class="knowledge-workspace">
      <aside class="topic-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Topics</span>
            <h2>推荐话题</h2>
          </div>
        </div>
        <input v-model="query" placeholder="搜索话题名称或介绍" />

        <div class="topic-list">
          <button
            v-for="topic in filteredTopics"
            :key="topic.topic_id_hazy"
            type="button"
            class="topic-row"
            :class="{ active: selectedTopicID === topic.topic_id_hazy }"
            @click="selectTopic(topic.topic_id_hazy)"
          >
            <div>
              <strong># {{ topic.name }}</strong>
              <span v-if="topic.tag" class="topic-tag">{{ topic.tag === 2 ? '热' : '新' }}</span>
            </div>
            <p>{{ topic.intro || '暂无介绍' }}</p>
            <small>{{ formatCount(topic.view_count) }} 阅读 · {{ formatCount(topic.notes_count) }} 讨论</small>
          </button>

          <div v-if="!loadingTopics && !filteredTopics.length" class="empty-state">
            {{ token ? '暂无话题，尝试刷新或调整关键词。' : '缺少 KBASE_AUTH_TOKEN，登录后会自动填充。' }}
          </div>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="topicPage <= 1 || loadingTopics" @click="changeTopicPage(topicPage - 1)">Prev</button>
          <span>Page {{ topicPage }}</span>
          <button type="button" :disabled="!topicHasMore || loadingTopics" @click="changeTopicPage(topicPage + 1)">Next</button>
        </div>
      </aside>

      <section class="notes-panel">
        <div class="panel-head">
          <div>
            <span class="eyebrow">Discussion</span>
            <h2>{{ selectedTopic?.name || '选择话题' }}</h2>
          </div>
          <div class="segmented-control">
            <button type="button" :class="{ active: elected }" @click="switchElected(true)">精选</button>
            <button type="button" :class="{ active: !elected }" @click="switchElected(false)">最新</button>
          </div>
        </div>

        <div class="note-list">
          <article v-for="note in notes" :key="note.note_id_hazy" class="topic-note-card">
            <div class="note-author">
              <div class="avatar">
                <img v-if="note.avatar" :src="note.avatar" alt="" />
                <span v-else>{{ (note.author_name || '?').slice(0, 1) }}</span>
              </div>
              <div>
                <strong>{{ note.author_name || '得到用户' }}</strong>
                <small>{{ note.time_desc || '刚刚' }}</small>
              </div>
              <span v-if="note.v_info || note.slogan" class="author-badge">{{ note.v_info || note.slogan }}</span>
            </div>

            <h3 v-if="note.note_title">{{ note.note_title }}</h3>
            <p>{{ note.note || '暂无内容' }}</p>

            <div v-if="note.images?.length" class="note-images">
              <img v-for="image in note.images.slice(0, 3)" :key="image" :src="image" alt="" />
            </div>

            <div v-if="note.base_title" class="source-card">
              <img v-if="note.base_img" :src="note.base_img" alt="" />
              <div>
                <strong>{{ note.base_title }}</strong>
                <span>{{ note.base_sub_title || '来自得到内容' }}</span>
              </div>
            </div>

            <footer>
              <span># {{ note.topic_name || selectedTopic?.name || '知识城邦' }}</span>
              <small>{{ note.like_count || 0 }} 赞 · {{ note.comment_count || 0 }} 评论 · {{ note.repost_count || 0 }} 转发</small>
            </footer>
          </article>

          <div v-if="!loadingNotes && !notes.length" class="empty-state">
            {{ selectedTopic ? '当前话题暂无讨论，切换精选/最新或刷新。' : '先从左侧选择一个话题。' }}
          </div>
        </div>

        <div class="book-pagination">
          <button type="button" :disabled="notePage <= 1 || loadingNotes" @click="changeNotePage(notePage - 1)">Prev</button>
          <span>Page {{ notePage }} · {{ elected ? '精选' : '最新' }}</span>
          <button type="button" :disabled="!noteHasMore || loadingNotes" @click="changeNotePage(notePage + 1)">Next</button>
        </div>
      </section>

      <aside class="topic-detail-panel">
        <div v-if="selectedTopic" class="topic-detail">
          <img v-if="selectedTopic.img" :src="selectedTopic.img" alt="" />
          <span class="eyebrow">Topic</span>
          <h2># {{ selectedTopic.name }}</h2>
          <p>{{ selectedTopic.intro || '暂无介绍' }}</p>
          <dl>
            <div>
              <dt>阅读</dt>
              <dd>{{ formatCount(selectedTopic.view_count) }}</dd>
            </div>
            <div>
              <dt>讨论</dt>
              <dd>{{ formatCount(selectedTopic.notes_count) }}</dd>
            </div>
            <div>
              <dt>状态</dt>
              <dd>{{ selectedTopic.has_new_notes ? '有新讨论' : '已同步' }}</dd>
            </div>
          </dl>
        </div>
        <div v-else class="empty-state">选择话题后查看详情。</div>
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getBrowserSession, KBaseClient, type DedaoTopic, type DedaoTopicNote } from '../api'

const storageKey = 'dedao-kbase-web-settings'

const baseUrl = ref(window.location.origin)
const token = ref('')
const connected = ref(false)
const loadingTopics = ref(false)
const loadingNotes = ref(false)
const errorMessage = ref('')
const query = ref('')
const topics = ref<DedaoTopic[]>([])
const notes = ref<DedaoTopicNote[]>([])
const selectedTopicID = ref('')
const topicPage = ref(1)
const topicPageSize = ref(20)
const topicHasMore = ref(false)
const notePage = ref(1)
const notePageSize = ref(20)
const noteHasMore = ref(false)
const elected = ref(true)

const client = computed(() => new KBaseClient(baseUrl.value, token.value))
const selectedTopic = computed(() => topics.value.find((topic) => topic.topic_id_hazy === selectedTopicID.value) || null)
const filteredTopics = computed(() => {
  const term = query.value.trim().toLowerCase()
  if (!term) {
    return topics.value
  }
  return topics.value.filter((topic) => [topic.name, topic.intro].join(' ').toLowerCase().includes(term))
})

onMounted(async () => {
  restoreConnection()
  try {
    await hydrateBrowserSession()
    if (token.value) {
      await loadTopics()
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

const loadTopics = async () => {
  if (!token.value) {
    connected.value = false
    errorMessage.value = '缺少 KBASE_AUTH_TOKEN，登录浏览器页后会自动填充。'
    return
  }
  loadingTopics.value = true
  errorMessage.value = ''
  try {
    await hydrateBrowserSession()
    const result = await client.value.listDedaoTopics(topicPage.value, topicPageSize.value)
    topics.value = result.topics || []
    topicPage.value = result.page || topicPage.value
    topicPageSize.value = result.page_size || topicPageSize.value
    topicHasMore.value = Boolean(result.has_more)
    connected.value = true
    if (!topics.value.some((topic) => topic.topic_id_hazy === selectedTopicID.value)) {
      selectedTopicID.value = topics.value[0]?.topic_id_hazy || ''
    }
    await loadNotes()
  } catch (error) {
    connected.value = false
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loadingTopics.value = false
  }
}

const loadNotes = async () => {
  if (!selectedTopicID.value || !token.value) {
    notes.value = []
    return
  }
  loadingNotes.value = true
  errorMessage.value = ''
  try {
    const result = await client.value.listDedaoTopicNotes(selectedTopicID.value, elected.value, notePage.value, notePageSize.value)
    notes.value = result.notes || []
    notePage.value = result.page || notePage.value
    notePageSize.value = result.page_size || notePageSize.value
    noteHasMore.value = Boolean(result.has_more)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loadingNotes.value = false
  }
}

const reloadTopics = async () => {
  topicPage.value = 1
  notePage.value = 1
  await loadTopics()
}

const selectTopic = async (topicID: string) => {
  selectedTopicID.value = topicID
  notePage.value = 1
  await loadNotes()
}

const switchElected = async (nextValue: boolean) => {
  elected.value = nextValue
  notePage.value = 1
  await loadNotes()
}

const changeTopicPage = async (nextPage: number) => {
  topicPage.value = Math.max(1, nextPage)
  notePage.value = 1
  await loadTopics()
}

const changeNotePage = async (nextPage: number) => {
  notePage.value = Math.max(1, nextPage)
  await loadNotes()
}

const formatCount = (value?: number) => {
  const count = Number(value || 0)
  if (count >= 10000) {
    return `${(count / 10000).toFixed(1)}万`
  }
  return String(count)
}
</script>

<style scoped>
.knowledge-city {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: calc(100vh - 80px);
  margin-top: 8px;
}

.knowledge-toolbar,
.topic-panel,
.notes-panel,
.topic-detail-panel {
  border: 1px solid var(--dedao-line);
  border-radius: 10px;
  background: #ffffff;
}

.knowledge-toolbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 92px 82px;
  gap: 10px;
  align-items: center;
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 6px 0 10px;
}

.toolbar-summary {
  display: grid;
  gap: 2px;
}

.toolbar-summary strong {
  color: #111111;
  font-size: 22px;
}

.toolbar-summary span,
.topic-row small,
.note-author small,
.topic-note-card footer small {
  color: var(--dedao-muted);
  font-size: 12px;
}

.knowledge-workspace {
  display: grid;
  grid-template-columns: minmax(240px, 300px) minmax(0, 1fr) minmax(220px, 280px);
  gap: 10px;
  min-height: 0;
}

.topic-panel,
.notes-panel,
.topic-detail-panel {
  min-width: 0;
  padding: 14px;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.panel-head h2,
.topic-detail h2 {
  margin: 0;
  color: #111111;
  font-size: 22px;
  line-height: 30px;
}

.eyebrow {
  color: var(--dedao-muted);
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
}

.topic-panel input {
  width: 100%;
  min-height: 40px;
  box-sizing: border-box;
  border: 1px solid var(--dedao-border);
  border-radius: 8px;
  padding: 0 12px;
  color: var(--dedao-text);
  font-size: 14px;
}

.topic-list,
.note-list {
  display: grid;
  gap: 0;
  margin-top: 10px;
}

.topic-row {
  border: 0;
  border-bottom: 1px solid var(--dedao-line);
  border-radius: 0;
  padding: 12px 0;
  background: transparent;
  text-align: left;
}

.topic-row.active {
  background: #fff8f2;
}

.topic-row div {
  display: flex;
  align-items: center;
  gap: 8px;
}

.topic-row strong {
  color: #222222;
  font-size: 15px;
}

.topic-row p,
.topic-note-card p,
.topic-detail p {
  margin: 6px 0;
  color: #666666;
  line-height: 22px;
}

.topic-tag,
.author-badge {
  border-radius: 999px;
  padding: 2px 7px;
  background: var(--dedao-orange-soft);
  color: var(--dedao-orange-dark);
  font-size: 11px;
  font-weight: 800;
}

.segmented-control {
  display: grid;
  grid-template-columns: repeat(2, 74px);
  gap: 6px;
}

.segmented-control button {
  min-height: 34px;
  border: 1px solid var(--dedao-border);
  border-radius: 8px;
  background: #ffffff;
  color: var(--dedao-text);
  font-weight: 700;
}

.segmented-control button.active {
  border-color: var(--dedao-orange);
  color: var(--dedao-orange);
}

.topic-note-card {
  border-bottom: 1px solid var(--dedao-line);
  padding: 14px 0;
}

.note-author {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
}

.avatar {
  display: grid;
  overflow: hidden;
  width: 44px;
  height: 44px;
  place-items: center;
  border-radius: 8px;
  background: var(--dedao-subtle);
  color: var(--dedao-orange);
  font-weight: 800;
}

.avatar img,
.note-images img,
.source-card img,
.topic-detail > img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.topic-note-card h3 {
  margin: 12px 0 6px;
  color: #111111;
  font-size: 18px;
  line-height: 26px;
}

.note-images {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin-top: 10px;
}

.note-images img {
  aspect-ratio: 1 / 1;
  border-radius: 8px;
  background: var(--dedao-subtle);
}

.source-card {
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  margin-top: 10px;
  border-radius: 8px;
  padding: 8px;
  background: var(--dedao-subtle);
}

.source-card img {
  height: 52px;
  border-radius: 6px;
}

.source-card strong,
.source-card span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.source-card span {
  color: var(--dedao-muted);
  font-size: 12px;
}

.topic-note-card footer {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 12px;
  color: var(--dedao-orange);
  font-size: 12px;
  font-weight: 700;
}

.topic-detail > img {
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: 8px;
  margin-bottom: 12px;
  background: var(--dedao-subtle);
}

.topic-detail dl {
  display: grid;
  gap: 8px;
  margin: 14px 0 0;
}

.topic-detail dl div {
  border-radius: 8px;
  padding: 10px;
  background: var(--dedao-subtle);
}

.topic-detail dt {
  color: var(--dedao-muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
}

.topic-detail dd {
  margin: 3px 0 0;
  color: #111111;
  font-weight: 800;
}

.empty-state {
  border: 1px dashed var(--dedao-border);
  border-radius: 8px;
  padding: 22px;
  color: var(--dedao-muted);
  text-align: center;
}

@media (max-width: 1120px) {
  .knowledge-workspace {
    grid-template-columns: 1fr;
  }

  .knowledge-toolbar {
    grid-template-columns: 1fr;
  }
}
</style>
