<template>
    <div class="wcplus-workbench">
        <section class="wcplus-action-bar">
            <div class="workbench-title">
                <div class="kicker">WC Plus Source</div>
                <h1>公众号知识来源</h1>
                <p>从本地 WC Plus 同步文章、创建任务，并导入书籍知识库；启动时自动检查环境。</p>
            </div>
            <div class="bar-actions">
                <el-tag :type="serviceTagType" effect="plain">{{ serviceLabel }}</el-tag>
                <el-button :loading="loading === 'status'" @click="checkStatus">检查状态</el-button>
                <el-button :loading="loading === 'env'" @click="checkEnvironment">环境检查</el-button>
                <el-button type="primary" :loading="loading === 'accounts'" @click="loadAccounts">加载公众号</el-button>
                <el-button :loading="loading === 'tasks'" @click="loadTasks">任务</el-button>
                <el-button type="warning" :loading="loading === 'queue'" @click="runQueue">启动队列</el-button>
            </div>
        </section>

        <el-alert
            v-if="message"
            class="workbench-message"
            :title="message"
            :type="messageType"
            show-icon
            :closable="false"
        />

        <div class="wcplus-grid">
            <aside class="wcplus-sidebar panel">
                <form class="search-form" @submit.prevent="searchWCPlus">
                    <el-input v-model="searchQuery" placeholder="搜索标题、全文或公众号" clearable />
                    <div class="search-row">
                        <el-select v-model="searchMode">
                            <el-option label="全文" value="fulltext" />
                            <el-option label="标题" value="title" />
                            <el-option label="已入库公众号" value="account" />
                            <el-option label="可导入公众号" value="candidate" />
                            <el-option label="全库文章" value="all" />
                        </el-select>
                        <el-button type="primary" native-type="submit" :loading="loading === 'search'">搜索</el-button>
                    </div>
                </form>

                <div class="pager-row">
                    <el-input-number v-model="accountNum" :min="1" :max="100" size="small" />
                    <el-button :disabled="accountOffset <= 0" @click="pageAccounts(-1)">上一页</el-button>
                    <el-button @click="pageAccounts(1)">下一页</el-button>
                </div>

                <div class="section-head">
                    <span>公众号</span>
                    <el-tag size="small" effect="plain">{{ accounts.length }}</el-tag>
                </div>
                <div class="account-list">
                    <button
                        v-for="account in accounts"
                        :key="accountBiz(account) || accountNickname(account)"
                        type="button"
                        class="account-row"
                        :class="{ active: accountBiz(account) === accountBiz(selectedAccount) }"
                        @click="selectAccount(account)"
                    >
                        <strong>{{ accountNickname(account) || '未命名公众号' }}</strong>
                        <span>{{ [accountBiz(account), accountArticleCount(account) ? `${accountArticleCount(account)} 篇` : ''].filter(Boolean).join(' · ') }}</span>
                    </button>
                    <el-empty v-if="!accounts.length" description="加载或搜索公众号" :image-size="64" />
                </div>
            </aside>

            <main class="wcplus-main panel">
                <div class="main-head">
                    <div>
                        <div class="kicker">Current Source</div>
                        <h2>{{ accountNickname(selectedAccount) || '选择公众号或搜索文章' }}</h2>
                    </div>
                    <div class="main-actions">
                        <el-button :disabled="!selectedAccount" @click="createTask">同步任务</el-button>
                        <el-button :disabled="!selectedAccount" @click="createBatchTask">批量任务</el-button>
                        <el-button :disabled="!selectedAccount" @click="exportText">导出 TXT</el-button>
                        <el-button :disabled="!selectedAccount" @click="exportCSV">导出 CSV</el-button>
                        <el-button type="primary" :disabled="!selectedAccount" @click="importAccount">批量导入</el-button>
                    </div>
                </div>

                <div class="options-row">
                    <label>
                        文章每页
                        <el-input-number v-model="articleNum" :min="1" :max="100" size="small" />
                    </label>
                    <label>
                        导入篇数
                        <el-input-number v-model="importLimit" :min="1" :max="100" size="small" />
                    </label>
                    <label>
                        任务类型
                        <el-select v-model="taskCrawlerType" size="small">
                            <el-option label="公众号链接" value="gzh_article_link" />
                            <el-option label="文章内容" value="article" />
                            <el-option label="公众号信息" value="gzh" />
                        </el-select>
                    </label>
                    <label>
                        范围
                        <el-select v-model="taskArticleListType" size="small">
                            <el-option label="全部" value="all" />
                            <el-option label="指定篇数" value="amount" />
                        </el-select>
                    </label>
                    <label>
                        数量
                        <el-input-number v-model="taskArticleListAmount" :min="0" :max="1000" size="small" />
                    </label>
                </div>

                <el-tabs v-model="mainTab" class="main-tabs">
                    <el-tab-pane label="文章" name="articles">
                        <div class="list-pager">
                            <span>{{ articleOffset + 1 }} - {{ articleOffset + articles.length }}</span>
                            <el-button :disabled="articleOffset <= 0" @click="pageArticles(-1)">上一页</el-button>
                            <el-button :disabled="!selectedAccount" @click="pageArticles(1)">下一页</el-button>
                        </div>
                        <div class="article-list">
                            <article v-for="article in articles" :key="articleID(article)" class="article-row">
                                <div>
                                    <h3>{{ articleTitle(article) || articleID(article) || '未命名文章' }}</h3>
                                    <p>{{ articleSubline(article) }}</p>
                                </div>
                                <div class="row-actions">
                                    <el-button @click="previewArticle(article)">预览</el-button>
                                    <el-button type="primary" @click="importArticle(article)">导入知识库</el-button>
                                </div>
                            </article>
                            <el-empty v-if="!articles.length" description="选择公众号后显示文章" :image-size="80" />
                        </div>
                    </el-tab-pane>

                    <el-tab-pane label="搜索结果" name="search">
                        <div class="article-list search-results">
                            <article v-for="(item, index) in searchResults" :key="`${articleID(item)}-${index}`" class="article-row">
                                <div>
                                    <h3>{{ articleTitle(item) || accountNickname(item) || articleID(item) || '搜索结果' }}</h3>
                                    <p>{{ resultSubline(item) }}</p>
                                </div>
                                <div class="row-actions">
                                    <el-button v-if="accountBiz(item)" @click="selectAccount(item)">选择</el-button>
                                    <el-button v-if="articleID(item) || articleURL(item)" @click="previewArticle(item)">预览</el-button>
                                    <el-button v-if="articleID(item) || articleURL(item)" type="primary" @click="importArticle(item)">导入</el-button>
                                </div>
                            </article>
                            <el-empty v-if="!searchResults.length" description="提交搜索后显示结果" :image-size="80" />
                        </div>
                    </el-tab-pane>
                </el-tabs>
            </main>

            <aside class="wcplus-preview">
                <section class="panel preview-panel">
                    <div class="section-head">
                        <span>文章预览</span>
                        <el-tag size="small" effect="plain">{{ preview ? '已加载' : '空' }}</el-tag>
                    </div>
                    <template v-if="preview">
                        <h2>{{ articleTitle(preview) || '未命名文章' }}</h2>
                        <p class="preview-meta">{{ [articleNickname(preview), articlePublishTime(preview), articleURL(preview)].filter(Boolean).join(' · ') }}</p>
                        <div class="preview-content">{{ articleContent(preview) }}</div>
                    </template>
                    <el-empty v-else description="点击文章预览" :image-size="64" />
                </section>

                <section class="panel wcplus-task-panel">
                    <div class="section-head">
                        <span>任务</span>
                        <div class="mini-actions">
                            <el-button size="small" @click="cleanBatchTasks">清理</el-button>
                            <el-button size="small" type="primary" @click="exportAllArticlesXLSX">XLSX</el-button>
                        </div>
                    </div>
                    <div class="task-list">
                        <div v-for="task in tasks" :key="taskID(task)" class="task-row">
                            <div>
                                <strong>{{ taskTitle(task) }}</strong>
                                <span>{{ taskStatus(task) }}</span>
                            </div>
                            <div class="mini-actions">
                                <el-button size="small" @click="controlTask(task, 'start')">开始</el-button>
                                <el-button size="small" @click="controlTask(task, 'stop')">停止</el-button>
                            </div>
                        </div>
                        <el-empty v-if="!tasks.length" description="暂无任务" :image-size="64" />
                    </div>
                </section>

                <section v-if="envCheck" class="panel wcplus-env-check">
                    <div class="section-head">
                        <span>环境诊断</span>
                        <div class="mini-actions">
                            <el-button size="small" @click="copyDiagnostics">复制诊断</el-button>
                            <el-tag size="small" :type="envCheck.ok ? 'success' : 'danger'" effect="plain">
                                {{ envCheck.ok ? '通过' : '需处理' }}
                            </el-tag>
                        </div>
                    </div>
                    <div class="wcplus-diagnostics">
                        <strong>服务地址</strong>
                        <code>{{ envCheck.base_url || '-' }}</code>
                        <small>kbase 服务端实际访问的 WC Plus API 地址。</small>
                    </div>
                    <div class="env-check-list">
                        <div v-for="item in envCheck.checks || []" :key="item.name" class="env-check-row">
                            <strong>{{ item.name }}</strong>
                            <span :class="{ ok: item.ok, bad: !item.ok }">{{ item.ok ? 'OK' : 'FAIL' }}</span>
                            <small>{{ item.message || '-' }}</small>
                        </div>
                    </div>
                    <ul v-if="envCheck.advice?.length" class="env-advice">
                        <li v-for="item in envCheck.advice" :key="item">{{ item }}</li>
                    </ul>
                </section>

                <section class="panel wcplus-batch-import">
                    <div class="section-head">
                        <span>批量公众号</span>
                    </div>
                    <el-input
                        v-model="batchNicknames"
                        type="textarea"
                        :rows="4"
                        placeholder="每行一个公众号昵称"
                    />
                    <div class="batch-options">
                        <el-checkbox v-model="batchExactMatch">精确匹配</el-checkbox>
                        <el-select v-model="batchArticleListType" size="small">
                            <el-option label="全部" value="all" />
                            <el-option label="指定篇数" value="amount" />
                        </el-select>
                        <el-input-number v-model="batchArticleListAmount" :min="0" :max="1000" size="small" />
                    </div>
                    <el-button type="primary" class="full-button" @click="batchImportNicknames">创建任务并启动</el-button>
                    <div v-if="batchResult" class="wcplus-batch-result">
                        <div class="section-head">
                            <span>批量结果</span>
                            <el-tag size="small" effect="plain">
                                成功 {{ batchResult.success?.length || 0 }} / 失败 {{ batchResult.failed?.length || 0 }}
                            </el-tag>
                        </div>
                        <el-input
                            :model-value="batchResult.success_text || '无成功项'"
                            type="textarea"
                            :rows="3"
                            readonly
                        />
                        <el-input
                            :model-value="batchResult.failed_text || '无失败项'"
                            type="textarea"
                            :rows="3"
                            readonly
                        />
                        <div class="mini-actions">
                            <el-button size="small" @click="copyBatchText('success')">复制成功</el-button>
                            <el-button size="small" @click="copyBatchText('failed')">复制失败</el-button>
                        </div>
                    </div>
                </section>

                <section class="panel wcplus-raw-import">
                    <div class="section-head">
                        <span>原文粘贴导入</span>
                    </div>
                    <el-input v-model="rawTitle" placeholder="文章标题" />
                    <el-input v-model="rawNickname" placeholder="公众号/作者" />
                    <el-input v-model="rawURL" placeholder="原文链接" />
                    <el-input v-model="rawBookID" placeholder="book_id 可选" />
                    <input
                        class="wcplus-raw-file"
                        type="file"
                        accept=".txt,.md,.markdown,text/plain,text/markdown"
                        @change="loadRawFile"
                    />
                    <el-input v-model="rawContent" type="textarea" :rows="5" placeholder="粘贴正文或 Markdown" />
                    <el-button type="primary" class="full-button" @click="importRawArticle">导入知识库</el-button>
                </section>
            </aside>
        </div>
    </div>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

const tokenKeys = ['kbase.token', 'kbaseToken', 'KBASE_AUTH_TOKEN']

const loading = ref('')
const message = ref('')
const messageType = ref<'success' | 'warning' | 'info' | 'error'>('info')
const serviceStatus = ref<any>(null)
const envCheck = ref<any>(null)
const accounts = ref<any[]>([])
const selectedAccount = ref<any>(null)
const articles = ref<any[]>([])
const searchResults = ref<any[]>([])
const tasks = ref<any[]>([])
const preview = ref<any>(null)
const mainTab = ref('articles')
const batchResult = ref<any>(null)

const searchQuery = ref('')
const searchMode = ref('fulltext')
const accountOffset = ref(0)
const accountNum = ref(20)
const articleOffset = ref(0)
const articleNum = ref(20)
const importLimit = ref(10)
const exportRecentNum = ref(100)
const taskCrawlerType = ref('gzh_article_link')
const taskArticleListType = ref('all')
const taskArticleListAmount = ref(20)
const batchNicknames = ref('')
const batchExactMatch = ref(true)
const batchArticleListType = ref('all')
const batchArticleListAmount = ref(0)
const rawTitle = ref('')
const rawNickname = ref('')
const rawURL = ref('')
const rawBookID = ref('')
const rawContent = ref('')

const serviceTagType = computed(() => {
    if (!serviceStatus.value) {
        return 'info'
    }
    return serviceStatus.value.ok ? 'success' : 'danger'
})

const serviceLabel = computed(() => {
    if (!serviceStatus.value) {
        return '未检查'
    }
    return serviceStatus.value.ok ? '已连接' : '未连接'
})

onMounted(() => {
    bootstrapWCPlusSource()
})

const readToken = () => {
    for (const key of tokenKeys) {
        const value = window.localStorage.getItem(key)
        if (value?.trim()) {
            return value.trim()
        }
    }
    return ''
}

const apiURL = (path: string, params?: Record<string, string | number | boolean | undefined>) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(params || {})) {
        if (value === undefined || value === '') {
            continue
        }
        query.set(key, String(value))
    }
    const suffix = query.toString()
    return suffix ? `${path}?${suffix}` : path
}

const apiJSON = async <T = any>(path: string, options: RequestInit = {}): Promise<T> => {
    const headers = new Headers(options.headers || {})
    headers.set('Accept', 'application/json')
    if (options.body && !headers.has('Content-Type')) {
        headers.set('Content-Type', 'application/json')
    }
    const token = readToken()
    if (token) {
        headers.set('Authorization', `Bearer ${token}`)
    }
    const response = await fetch(path, {...options, headers})
    const text = await response.text()
    let payload: any = null
    if (text) {
        try {
            payload = JSON.parse(text)
        } catch {
            payload = text
        }
    }
    if (!response.ok) {
        const err = payload && typeof payload === 'object'
            ? payload.error || payload.message || JSON.stringify(payload)
            : payload || `HTTP ${response.status}`
        throw new Error(err)
    }
    return payload as T
}

const apiDownload = async (path: string, options: RequestInit, filename: string) => {
    const headers = new Headers(options.headers || {})
    headers.set('Accept', '*/*')
    if (options.body && !headers.has('Content-Type')) {
        headers.set('Content-Type', 'application/json')
    }
    const token = readToken()
    if (token) {
        headers.set('Authorization', `Bearer ${token}`)
    }
    const response = await fetch(path, {...options, headers})
    if (!response.ok) {
        throw new Error(await response.text() || `HTTP ${response.status}`)
    }
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.append(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
    return blob.size
}

const withLoading = async (key: string, action: () => Promise<void>) => {
    loading.value = key
    message.value = ''
    try {
        await action()
    } catch (error: any) {
        notify(String(error?.message || error), 'warning')
    } finally {
        loading.value = ''
    }
}

const notify = (text: string, type: 'success' | 'warning' | 'info' | 'error' = 'info') => {
    message.value = text
    messageType.value = type
    ElMessage({message: text, type})
}

const firstValue = (value: any, keys: string[]) => {
    if (!value || typeof value !== 'object') {
        return ''
    }
    for (const key of keys) {
        const found = value[key]
        if (found !== undefined && found !== null && String(found).trim()) {
            return String(found).trim()
        }
    }
    return ''
}

const firstArray = (payload: any, keys: string[]) => {
    if (Array.isArray(payload)) {
        return payload
    }
    const source = payload?.data && typeof payload.data === 'object' ? payload.data : payload
    if (Array.isArray(source)) {
        return source
    }
    if (!source || typeof source !== 'object') {
        return []
    }
    for (const key of keys) {
        if (Array.isArray(source[key])) {
            return source[key]
        }
    }
    return []
}

const numberValue = (value: any, keys: string[]) => {
    const raw = firstValue(value, keys)
    const parsed = Number.parseInt(raw, 10)
    return Number.isFinite(parsed) ? parsed : 0
}

const accountBiz = (account: any) => firstValue(account, ['biz', 'Biz', 'fakeid', 'FakeID'])
const accountNickname = (account: any) => firstValue(account, ['nickname', 'Nickname', 'name', 'Name', 'gzh_nickname'])
const accountArticleCount = (account: any) => numberValue(account, ['article_count', 'ArticleCount', 'articleCount', 'total'])
const articleID = (article: any) => firstValue(article, ['id', 'ID', 'article_id', 'ArticleID', 'ArticleId', 'articleId', 'appmsgid', 'AppMsgID', 'app_msg_id', 'msgid', 'MsgID', 'aid', 'Aid'])
const articleTitle = (article: any) => firstValue(article, ['title', 'Title'])
const articleNickname = (article: any) => firstValue(article, ['nickname', 'Nickname', 'gzh_nickname'])
const articleURL = (article: any) => firstValue(article, ['url', 'URL', 'link', 'Link', 'content_url', 'ContentURL', 'source_url', 'SourceURL'])
const articleDigest = (article: any) => firstValue(article, ['digest', 'Digest', 'summary', 'Summary'])
const articlePublishTime = (article: any) => firstValue(article, ['publish_time', 'PublishTime', 'p_date_text', 'PDateText', 'pDateText', 'date', 'Date'])
const articleContent = (article: any) => firstValue(article, ['content', 'Content', 'markdown', 'Markdown', 'text', 'Text']) || '暂无正文内容'
const articleSubline = (article: any) => [articleDigest(article), articlePublishTime(article), articleURL(article)].filter(Boolean).join(' · ') || '暂无摘要'
const resultSubline = (item: any) => [articleDigest(item), articlePublishTime(item), articleURL(item), accountBiz(item)].filter(Boolean).join(' · ') || '暂无摘要'
const taskID = (task: any) => firstValue(task, ['task_id', 'TaskID', 'id', 'ID'])
const taskStatus = (task: any) => firstValue(task, ['status', 'Status', 'message', 'Message']) || 'unknown'
const taskTitle = (task: any) => firstValue(task, ['nickname', 'Nickname', 'title', 'Title']) || taskID(task) || '任务'

const checkStatus = async () => {
    await withLoading('status', async () => {
        serviceStatus.value = await apiJSON('/api/wcplus/status')
        notify(serviceStatus.value?.ok ? 'WC Plus 本地服务已连接。' : 'WC Plus 本地服务未连接。', serviceStatus.value?.ok ? 'success' : 'warning')
    })
}

const checkEnvironment = async () => {
    await withLoading('env', async () => {
        envCheck.value = await apiJSON('/api/wcplus/env/check')
        serviceStatus.value = {ok: Boolean(envCheck.value?.ok)}
        const failed = Array.isArray(envCheck.value?.checks)
            ? envCheck.value.checks.filter((item: any) => !item.ok).map((item: any) => item.name).join(', ')
            : ''
        notify(envCheck.value?.ok ? '环境检查通过。' : `环境检查未通过：${failed || '请检查服务状态'}`, envCheck.value?.ok ? 'success' : 'warning')
    })
}

const bootstrapWCPlusSource = async () => {
    loading.value = 'bootstrap'
    message.value = '启动时自动检查环境，加载诊断、任务和公众号列表。'
    messageType.value = 'info'
    const [envResult, taskResult, accountResult] = await Promise.allSettled([
        apiJSON('/api/wcplus/env/check'),
        apiJSON('/api/wcplus/task/all'),
        apiJSON(apiURL('/api/wcplus/gzh/list', {offset: accountOffset.value, num: accountNum.value})),
    ])

    const failures: string[] = []
    if (envResult.status === 'fulfilled') {
        envCheck.value = envResult.value
        serviceStatus.value = {ok: Boolean(envResult.value?.ok)}
        if (!envResult.value?.ok) {
            failures.push('环境检查')
        }
    } else {
        serviceStatus.value = {ok: false}
        failures.push('环境检查')
    }

    if (taskResult.status === 'fulfilled') {
        tasks.value = firstArray(taskResult.value, ['tasks', 'items', 'list'])
    } else {
        failures.push('任务列表')
    }

    if (accountResult.status === 'fulfilled') {
        accounts.value = firstArray(accountResult.value, ['accounts', 'gzhs', 'items', 'list'])
        selectedAccount.value = accounts.value[0] || null
        if (selectedAccount.value) {
            await loadArticles(false)
        }
    } else {
        failures.push('公众号列表')
    }

    loading.value = ''
    if (failures.length) {
        notify(`启动检查完成，但 ${failures.join('、')} 需要处理；可继续使用手动导入知识库。`, 'warning')
    } else {
        notify(`启动检查完成：${accounts.value.length} 个公众号，${tasks.value.length} 个任务。`, 'success')
    }
}

const loadAccounts = async () => {
    await withLoading('accounts', async () => {
        const payload: any = await apiJSON(apiURL('/api/wcplus/gzh/list', {offset: accountOffset.value, num: accountNum.value}))
        accounts.value = firstArray(payload, ['accounts', 'gzhs', 'items', 'list'])
        selectedAccount.value = accounts.value[0] || null
        articles.value = []
        if (selectedAccount.value) {
            await loadArticles(false)
        }
        notify(`已加载 ${accounts.value.length} 个公众号。`, 'success')
    })
}

const selectAccount = async (account: any) => {
    selectedAccount.value = account
    articleOffset.value = 0
    mainTab.value = 'articles'
    await loadArticles()
}

const pageAccounts = async (delta: number) => {
    accountOffset.value = Math.max(0, accountOffset.value + delta * accountNum.value)
    await loadAccounts()
}

const loadArticles = async (showMessage = true) => {
    const biz = accountBiz(selectedAccount.value)
    if (!biz) {
        return
    }
    await withLoading('articles', async () => {
        const payload: any = await apiJSON(apiURL('/api/wcplus/gzh/articles', {
            biz,
            nickname: accountNickname(selectedAccount.value),
            offset: articleOffset.value,
            num: articleNum.value,
        }))
        articles.value = firstArray(payload, ['articles', 'items', 'list'])
        if (showMessage) {
            notify(`已加载 ${articles.value.length} 篇文章。`, 'success')
        }
    })
}

const pageArticles = async (delta: number) => {
    articleOffset.value = Math.max(0, articleOffset.value + delta * articleNum.value)
    await loadArticles()
}

const searchWCPlus = async () => {
    if (!searchQuery.value.trim() && searchMode.value !== 'all') {
        notify('请输入搜索关键词。', 'warning')
        return
    }
    await withLoading('search', async () => {
        const endpointByMode: Record<string, string> = {
            fulltext: '/api/wcplus/search',
            title: '/api/wcplus/article/search-title',
            account: '/api/wcplus/gzh/search',
            candidate: '/api/wcplus/search-gzh',
            all: '/api/wcplus/article/all',
        }
        const payload: any = await apiJSON(apiURL(endpointByMode[searchMode.value] || endpointByMode.fulltext, {
            q: searchQuery.value.trim(),
            keyword: searchQuery.value.trim(),
            offset: 0,
            num: searchMode.value === 'all' ? articleNum.value : 30,
            sort: 'p_date',
            direction: 'desc',
        }))
        searchResults.value = searchMode.value === 'account' || searchMode.value === 'candidate'
            ? firstArray(payload, ['accounts', 'Accounts', 'gzhs', 'Gzhs', 'candidates', 'Candidates', 'items', 'Items'])
            : firstArray(payload, ['results', 'Results', 'articles', 'Articles', 'items', 'Items'])
        mainTab.value = 'search'
        notify(`搜索完成：${searchResults.value.length} 条结果。`, 'success')
    })
}

const previewArticle = async (article: any) => {
    const nickname = articleNickname(article) || accountNickname(selectedAccount.value)
    const id = articleID(article)
    const url = articleURL(article)
    if ((!nickname || !id) && !url) {
        notify('文章缺少 nickname/id 或 URL。', 'warning')
        return
    }
    await withLoading('preview', async () => {
        preview.value = await apiJSON(apiURL('/api/wcplus/article/content', id ? {nickname, id} : {url}))
        notify('文章预览已更新。', 'success')
    })
}

const importArticle = async (article: any) => {
    const nickname = articleNickname(article) || accountNickname(selectedAccount.value)
    const id = articleID(article)
    const url = articleURL(article)
    if ((!nickname || !id) && !url) {
        notify('文章缺少 nickname/id 或 URL。', 'warning')
        return
    }
    await withLoading('importArticle', async () => {
        const payload: any = await apiJSON('/api/wcplus/import/article', {
            method: 'POST',
            body: JSON.stringify(id ? {nickname, id} : {url}),
        })
        notify(`已导入：${payload?.book?.title || articleTitle(article) || id || url}`, 'success')
    })
}

const importAccount = async () => {
    const biz = accountBiz(selectedAccount.value)
    if (!biz) {
        notify('请先选择公众号。', 'warning')
        return
    }
    await withLoading('importAccount', async () => {
        const payload: any = await apiJSON('/api/wcplus/import/account', {
            method: 'POST',
            body: JSON.stringify({
                biz,
                nickname: accountNickname(selectedAccount.value),
                limit: importLimit.value,
            }),
        })
        notify(`批量导入完成：${payload?.imported_count || 0} 篇。`, 'success')
    })
}

const createTask = async () => {
    const biz = accountBiz(selectedAccount.value)
    if (!biz) {
        notify('请先选择公众号。', 'warning')
        return
    }
    await withLoading('taskCreate', async () => {
        const task: any = await apiJSON('/api/wcplus/task/new', {
            method: 'POST',
            body: JSON.stringify(taskPayload()),
        })
        notify(`已创建同步任务：${task?.task_id || accountNickname(selectedAccount.value) || biz}`, 'success')
        await loadTasks(false)
    })
}

const createBatchTask = async () => {
    const biz = accountBiz(selectedAccount.value)
    if (!biz) {
        notify('请先选择公众号。', 'warning')
        return
    }
    await withLoading('batchTaskCreate', async () => {
        const result: any = await apiJSON('/api/wcplus/batch-task/create', {
            method: 'POST',
            body: JSON.stringify(taskPayload()),
        })
        notify(`批量任务已提交：${firstValue(result, ['task_id', 'TaskID', 'status', 'Status']) || '完成'}`, 'success')
        await loadTasks(false)
    })
}

const taskPayload = () => ({
    biz: accountBiz(selectedAccount.value),
    nickname: accountNickname(selectedAccount.value),
    crawlerType: taskCrawlerType.value,
    articleListType: taskArticleListType.value,
    articleListAmount: taskArticleListAmount.value,
})

const loadTasks = async (showMessage = true) => {
    await withLoading('tasks', async () => {
        const payload: any = await apiJSON('/api/wcplus/task/all')
        tasks.value = firstArray(payload, ['tasks', 'items', 'list'])
        if (showMessage) {
            notify(`已加载 ${tasks.value.length} 个任务。`, 'success')
        }
    })
}

const controlTask = async (task: any, action: string) => {
    const id = taskID(task)
    if (!id) {
        notify('任务缺少 task_id。', 'warning')
        return
    }
    await withLoading('taskControl', async () => {
        const updated: any = await apiJSON('/api/wcplus/task/control', {
            method: 'POST',
            body: JSON.stringify({task_id: id, action}),
        })
        notify(`任务状态：${firstValue(updated, ['status', 'Status', 'message', 'Message']) || action}`, 'success')
        await loadTasks(false)
    })
}

const runQueue = async () => {
    await withLoading('queue', async () => {
        const result: any = await apiJSON('/api/wcplus/task/control', {
            method: 'POST',
            body: JSON.stringify({command: 'run'}),
        })
        notify(`队列已启动：${firstValue(result, ['status', 'Status', 'message', 'Message']) || 'running'}`, 'success')
        await loadTasks(false)
    })
}

const cleanBatchTasks = async () => {
    await withLoading('cleanBatch', async () => {
        const result: any = await apiJSON('/api/wcplus/batch-task/delete', {
            method: 'POST',
            body: JSON.stringify({status: ['ready', 'error']}),
        })
        notify(`批量任务已清理：${firstValue(result, ['deleted', 'Deleted', 'count', 'Count']) || '完成'}`, 'success')
        await loadTasks(false)
    })
}

const batchImportNicknames = async () => {
    const nicknames = batchNicknames.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
    if (!nicknames.length) {
        notify('请先输入公众号昵称。', 'warning')
        return
    }
    await withLoading('batchImport', async () => {
        const result: any = await apiJSON('/api/wcplus/batch-import/gzh', {
            method: 'POST',
            body: JSON.stringify({
                nicknames,
                articleListType: batchArticleListType.value,
                articleListAmount: batchArticleListType.value === 'amount' ? batchArticleListAmount.value : 0,
                start_queue: true,
                exact_match: batchExactMatch.value,
            }),
        })
        batchResult.value = result
        const successCount = Array.isArray(result?.success) ? result.success.length : 0
        const failedCount = Array.isArray(result?.failed) ? result.failed.length : 0
        notify(`批量任务完成：成功 ${successCount}，失败 ${failedCount}${result?.started ? '，队列已启动' : ''}。`, failedCount ? 'warning' : 'success')
        await loadTasks(false)
    })
}

const copyBatchText = async (kind: 'success' | 'failed') => {
    const text = kind === 'success'
        ? batchResult.value?.success_text
        : batchResult.value?.failed_text
    if (!text) {
        notify(kind === 'success' ? '暂无成功清单。' : '暂无失败清单。', 'info')
        return
    }
    try {
        await navigator.clipboard.writeText(text)
        notify('已复制到剪贴板。', 'success')
    } catch {
        notify('浏览器不允许写入剪贴板，请手动复制文本框内容。', 'warning')
    }
}

const diagnosticText = () => {
    const check = envCheck.value || {}
    const lines = [
        `WC Plus environment: ${check.ok ? 'OK' : 'NEEDS_ACTION'}`,
        `base_url: ${check.base_url || '-'}`,
    ]
    if (Array.isArray(check.checks) && check.checks.length) {
        lines.push('', 'checks:')
        for (const item of check.checks) {
            lines.push(`- ${item.name || 'check'}: ${item.ok ? 'OK' : 'FAIL'} ${item.message || ''}`.trim())
        }
    }
    if (Array.isArray(check.advice) && check.advice.length) {
        lines.push('', 'advice:')
        for (const item of check.advice) {
            lines.push(`- ${item}`)
        }
    }
    const batch = batchResult.value
    if (batch) {
        lines.push(
            '',
            `batch_success: ${Array.isArray(batch.success) ? batch.success.length : 0}`,
            `batch_failed: ${Array.isArray(batch.failed) ? batch.failed.length : 0}`,
        )
        if (batch.failed_text) {
            lines.push('', 'failed_text:', batch.failed_text)
        }
    }
    return lines.join('\n')
}

const copyDiagnostics = async () => {
    if (!envCheck.value) {
        notify('请先执行环境检查。', 'info')
        return
    }
    try {
        await navigator.clipboard.writeText(diagnosticText())
        notify('诊断信息已复制。', 'success')
    } catch {
        notify('浏览器不允许写入剪贴板，请手动复制环境诊断内容。', 'warning')
    }
}

const loadRawFile = (event: Event) => {
    const input = event.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) {
        return
    }
    const reader = new FileReader()
    reader.onload = () => {
        rawContent.value = String(reader.result || '')
        if (!rawTitle.value.trim()) {
            rawTitle.value = file.name.replace(/\.(txt|md|markdown)$/i, '')
        }
        notify(`已读取文件：${file.name}`, 'success')
    }
    reader.onerror = () => {
        notify(reader.error?.message || '读取文件失败。', 'warning')
    }
    reader.readAsText(file)
}

const importRawArticle = async () => {
    if (!rawTitle.value.trim() || !rawContent.value.trim()) {
        notify('请填写标题和正文。', 'warning')
        return
    }
    await withLoading('rawImport', async () => {
        const payload: any = await apiJSON('/api/wcplus/import/raw', {
            method: 'POST',
            body: JSON.stringify({
                title: rawTitle.value.trim(),
                nickname: rawNickname.value.trim(),
                url: rawURL.value.trim(),
                book_id: rawBookID.value.trim(),
                content: rawContent.value,
            }),
        })
        notify(`已导入：${payload?.book?.title || rawTitle.value}`, 'success')
    })
}

const exportText = async () => {
    const biz = accountBiz(selectedAccount.value)
    if (!biz) {
        notify('请先选择公众号。', 'warning')
        return
    }
    await withLoading('exportText', async () => {
        const result = await apiJSON(apiURL('/api/wcplus/export/text', {
            biz,
            nickname: accountNickname(selectedAccount.value),
            only_main: true,
            need_img: false,
            open_dir: false,
        }))
        notify(`TXT 导出已触发：${JSON.stringify(result)}`, 'success')
    })
}

const exportCSV = async () => {
    const biz = accountBiz(selectedAccount.value)
    if (!biz) {
        notify('请先选择公众号。', 'warning')
        return
    }
    await withLoading('exportCSV', async () => {
        const result = await apiJSON(apiURL('/api/wcplus/export/gzh-csv', {
            biz,
            nickname: accountNickname(selectedAccount.value),
            open_dir: false,
        }))
        notify(`CSV 导出已触发：${JSON.stringify(result)}`, 'success')
    })
}

const exportAllArticlesXLSX = async () => {
    await withLoading('exportXLSX', async () => {
        const size = await apiDownload('/api/wcplus/export/all-articles-xlsx', {
            method: 'POST',
            body: JSON.stringify({
                sort: 'p_date',
                direction: 'desc',
                only_headline: false,
                range_mode: 'recent',
                recent_num: exportRecentNum.value,
                fields: [
                    'gzh_nickname',
                    'title',
                    'author',
                    'p_date_text',
                    'read_num',
                    'like_num',
                    'comment_num',
                    'digest',
                    'content_url',
                    'source_url',
                    'content',
                ],
            }),
        }, 'wcplus-all-articles.xlsx')
        notify(`XLSX 已下载：${size} bytes。`, 'success')
    })
}
</script>

<style scoped>
.wcplus-workbench {
    display: flex;
    flex-direction: column;
    height: calc(100vh - 92px);
    min-height: 640px;
    gap: 12px;
    color: #1f2937;
    text-align: left;
}

.wcplus-action-bar,
.panel {
    border: 1px solid #e5e7eb;
    border-radius: 8px;
    background: #ffffff;
    box-shadow: 0 10px 24px rgb(15 23 42 / 5%);
}

.wcplus-action-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 14px;
}

.workbench-title h1,
.main-head h2,
.preview-panel h2 {
    margin: 0;
    color: #111827;
    font-weight: 800;
    letter-spacing: 0;
}

.workbench-title h1 {
    font-size: 22px;
    line-height: 30px;
}

.workbench-title p {
    margin: 2px 0 0;
    color: #64748b;
}

.kicker {
    color: #f97316;
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0;
    text-transform: uppercase;
}

.bar-actions,
.main-actions,
.row-actions,
.mini-actions,
.search-row,
.pager-row,
.options-row,
.batch-options {
    display: flex;
    align-items: center;
    gap: 8px;
}

.bar-actions,
.main-actions {
    flex-wrap: wrap;
    justify-content: flex-end;
}

.workbench-message {
    flex-shrink: 0;
}

.wcplus-grid {
    display: grid;
    grid-template-columns: minmax(260px, 18vw) minmax(620px, 1fr) minmax(320px, 22vw);
    gap: 12px;
    min-height: 0;
    flex: 1;
}

.panel {
    min-width: 0;
    min-height: 0;
    padding: 12px;
}

.wcplus-sidebar,
.wcplus-main,
.wcplus-preview {
    min-height: 0;
}

.wcplus-sidebar,
.wcplus-main {
    display: flex;
    flex-direction: column;
}

.wcplus-preview {
    display: grid;
    grid-template-rows: minmax(180px, 1fr) minmax(150px, 0.72fr) auto auto auto;
    gap: 12px;
    overflow: hidden;
}

.search-form {
    display: grid;
    gap: 8px;
}

.search-row :deep(.el-select) {
    flex: 1;
}

.pager-row {
    margin: 12px 0;
}

.section-head,
.main-head {
    display: flex;
    justify-content: space-between;
    gap: 12px;
}

.section-head {
    align-items: center;
    margin-bottom: 10px;
    font-weight: 800;
}

.main-head {
    align-items: flex-start;
    padding-bottom: 10px;
    border-bottom: 1px solid #edf0f5;
}

.main-head h2 {
    overflow: hidden;
    max-width: 720px;
    font-size: 20px;
    line-height: 28px;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.account-list,
.article-list,
.task-list,
.preview-content {
    min-height: 0;
    overflow: auto;
}

.account-list {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 6px;
}

.account-row {
    display: grid;
    gap: 4px;
    width: 100%;
    border: 1px solid transparent;
    border-radius: 8px;
    padding: 10px;
    background: #fff;
    color: #1f2937;
    text-align: left;
    cursor: pointer;
}

.account-row:hover,
.account-row.active {
    border-color: #fb923c;
    background: #fff7ed;
}

.account-row span,
.article-row p,
.preview-meta,
.task-row span {
    color: #64748b;
    font-size: 12px;
    line-height: 18px;
}

.options-row {
    flex-wrap: wrap;
    padding: 10px 0;
}

.options-row label {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #64748b;
    font-size: 12px;
}

.main-tabs {
    min-height: 0;
    flex: 1;
}

.main-tabs :deep(.el-tabs__content) {
    height: calc(100% - 48px);
}

.main-tabs :deep(.el-tab-pane) {
    height: 100%;
}

.list-pager {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    margin-bottom: 8px;
}

.article-list {
    display: grid;
    gap: 8px;
    height: calc(100% - 42px);
}

.search-results {
    height: 100%;
}

.article-row,
.task-row {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    border: 1px solid #edf0f5;
    border-radius: 8px;
    padding: 10px;
    background: #fff;
}

.article-row h3 {
    margin: 0 0 4px;
    color: #111827;
    font-size: 15px;
    line-height: 22px;
}

.article-row p {
    display: -webkit-box;
    overflow: hidden;
    margin: 0;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
}

.row-actions {
    flex-shrink: 0;
}

.preview-panel,
.wcplus-task-panel,
.wcplus-env-check,
.wcplus-batch-import,
.wcplus-raw-import {
    overflow: hidden;
}

.preview-panel,
.wcplus-task-panel,
.wcplus-env-check {
    display: flex;
    flex-direction: column;
}

.preview-panel h2 {
    font-size: 18px;
    line-height: 26px;
}

.preview-meta {
    margin: 6px 0 10px;
}

.preview-content {
    white-space: pre-wrap;
    color: #334155;
    line-height: 1.7;
}

.task-list {
    display: grid;
    gap: 8px;
}

.task-row {
    align-items: center;
}

.task-row > div:first-child {
    display: grid;
    gap: 3px;
}

.wcplus-batch-import,
.wcplus-raw-import,
.wcplus-batch-result {
    display: grid;
    gap: 8px;
}

.env-check-list {
    display: grid;
    gap: 6px;
    overflow: auto;
}

.wcplus-diagnostics {
    display: grid;
    grid-template-columns: minmax(80px, 120px) minmax(0, 1fr);
    gap: 4px 8px;
    padding-bottom: 8px;
    border-bottom: 1px solid #edf0f5;
}

.wcplus-diagnostics code {
    overflow-wrap: anywhere;
    border-radius: 5px;
    background: #f8fafc;
    padding: 2px 6px;
    color: #334155;
    font-size: 12px;
}

.wcplus-diagnostics small {
    grid-column: 1 / -1;
    color: #64748b;
    line-height: 18px;
}

.env-check-row {
    display: grid;
    grid-template-columns: minmax(70px, 1fr) auto;
    gap: 4px 8px;
    border-bottom: 1px solid #edf0f5;
    padding-bottom: 6px;
}

.env-check-row small {
    grid-column: 1 / -1;
    color: #64748b;
    line-height: 18px;
}

.env-check-row .ok {
    color: #047857;
    font-weight: 800;
}

.env-check-row .bad {
    color: #b91c1c;
    font-weight: 800;
}

.env-advice {
    margin: 8px 0 0;
    padding-left: 18px;
    color: #64748b;
    font-size: 12px;
    line-height: 18px;
}

.batch-options {
    flex-wrap: wrap;
}

.full-button {
    width: 100%;
}

.wcplus-raw-file {
    width: 100%;
    border: 1px solid #e5e7eb;
    border-radius: 6px;
    padding: 8px 10px;
    background: #f8fafc;
    color: #334155;
}

@media (max-width: 1280px) {
    .wcplus-grid {
        grid-template-columns: minmax(240px, 24vw) minmax(520px, 1fr);
    }

    .wcplus-preview {
        grid-column: 1 / -1;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        grid-template-rows: auto;
        overflow: visible;
    }
}

@media (max-width: 820px) {
    .wcplus-workbench {
        height: auto;
    }

    .wcplus-action-bar,
    .wcplus-grid,
    .wcplus-preview {
        display: flex;
        flex-direction: column;
    }
}
</style>
