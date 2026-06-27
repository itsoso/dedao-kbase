<template>
    <div class="book-knowledge knowledge-shell">
        <div class="toolbar workbench-toolbar">
            <div class="toolbar-left">
                <el-input
                    v-model="searchText"
                    class="search-input"
                    placeholder="检索 chunks / claims"
                    clearable
                    @keyup.enter="searchKnowledge"
                    @clear="clearSearch"
                >
                    <template #append>
                        <el-button icon="Search" @click="searchKnowledge" />
                    </template>
                </el-input>
                <el-button icon="Refresh" @click="loadBooks">刷新</el-button>
            </div>
            <el-tag effect="plain" type="info" class="root-tag">{{ rootPath || 'book_knowledge' }}</el-tag>
        </div>

        <div class="workspace">
            <section class="library-panel">
                <div class="panel-head">
                    <div>
                        <div class="panel-kicker">Knowledge Books</div>
                        <div class="section-title">书籍</div>
                    </div>
                    <el-tag effect="plain" size="small" class="count-tag">{{ books.length }} 本</el-tag>
                </div>
                <el-table
                    :data="books"
                    v-loading="loadingBooks"
                    class="library-table"
                    highlight-current-row
                    height="100%"
                    table-layout="auto"
                    @current-change="selectBook"
                >
                    <el-table-column prop="book_id" label="ID" width="74" />
                    <el-table-column prop="title" label="标题" min-width="180">
                        <template #default="scope">
                            <div class="book-title">{{ scope.row.title }}</div>
                            <div class="book-meta">
                                <span>{{ scope.row.status || 'draft' }}</span>
                                <span>{{ scope.row.extractor || '-' }}</span>
                            </div>
                        </template>
                    </el-table-column>
                </el-table>
            </section>

            <section class="research-panel">
                <el-empty v-if="!selectedBook" description="暂无书籍知识包" />
                <template v-else>
                    <div class="research-head">
                        <div class="book-heading">
                            <div class="panel-kicker">Current Book</div>
                            <div class="detail-title">{{ selectedBook.title }}</div>
                            <div class="detail-meta">
                                <span>book_id: {{ selectedBook.book_id }}</span>
                                <span>{{ bookPackage?.chapters?.length || 0 }} 章</span>
                                <span>{{ bookPackage?.claims?.length || 0 }} claims</span>
                                <span>{{ bookPackage?.chunks?.length || 0 }} chunks</span>
                                <span v-if="selectedBook.source_html">HTML: {{ selectedBook.source_html }}</span>
                            </div>
                        </div>
                        <div class="actions">
                            <el-button icon="UploadFilled" size="small" @click="exportKnowledge('health_system_kb_v2')">
                                Health KB
                            </el-button>
                            <el-button icon="DataAnalysis" size="small" @click="exportKnowledge('quant_rule_cards')">
                                Quant Rules
                            </el-button>
                        </div>
                    </div>

                    <div v-if="searchResults.length" class="search-results">
                        <div class="section-title">检索结果</div>
                        <el-table :data="searchResults" height="220" table-layout="auto">
                            <el-table-column prop="kind" label="类型" width="84" />
                            <el-table-column prop="title" label="标题" width="180" />
                            <el-table-column prop="snippet" label="片段" min-width="360" />
                            <el-table-column prop="score" label="得分" width="76">
                                <template #default="scope">{{ scope.row.score.toFixed(2) }}</template>
                            </el-table-column>
                        </el-table>
                    </div>

                    <el-tabs v-model="activeTab" class="detail-tabs">
                        <el-tab-pane label="章节" name="chapters">
                            <div class="chapter-grid">
                                <el-table
                                    :data="bookPackage?.chapters || []"
                                    height="100%"
                                    highlight-current-row
                                    table-layout="auto"
                                    @current-change="selectChapter"
                                >
                                    <el-table-column prop="order" label="#" width="60" />
                                    <el-table-column prop="title" label="章节" min-width="180" />
                                    <el-table-column prop="summary" label="摘要" min-width="260" />
                                </el-table>
                                <div class="chapter-panel">
                                    <el-empty v-if="!selectedChapter" description="选择章节" />
                                    <template v-else>
                                        <div class="panel-title">{{ selectedChapter.title }}</div>
                                        <div class="chunk-list">
                                            <div v-for="chunk in chapterChunks" :key="chunk.chunk_id" class="chunk-row">
                                                <div class="chunk-meta">{{ chunk.chunk_id }}</div>
                                                <div class="chunk-text">{{ chunk.text }}</div>
                                            </div>
                                        </div>
                                    </template>
                                </div>
                            </div>
                        </el-tab-pane>

                        <el-tab-pane label="Claims" name="claims">
                            <el-table :data="bookPackage?.claims || []" height="100%" table-layout="auto">
                                <el-table-column prop="claim_id" label="ID" width="150" />
                                <el-table-column prop="title" label="标题" width="220" />
                                <el-table-column prop="summary" label="内容" min-width="360" />
                                <el-table-column prop="review_status" label="状态" width="100">
                                    <template #default="scope">
                                        <el-tag size="small" :type="scope.row.review_status === 'reviewed' ? 'success' : 'warning'">
                                            {{ scope.row.review_status || 'draft' }}
                                        </el-tag>
                                    </template>
                                </el-table-column>
                                <el-table-column prop="evidence_level" label="证据" width="76" />
                            </el-table>
                        </el-tab-pane>

                        <el-tab-pane label="Chunks" name="chunks">
                            <el-table :data="bookPackage?.chunks || []" height="100%" table-layout="auto">
                                <el-table-column prop="chunk_id" label="ID" width="150" />
                                <el-table-column prop="chapter_id" label="章节" width="150" />
                                <el-table-column prop="text" label="内容" min-width="520" />
                                <el-table-column prop="tokens" label="Tokens" width="90" />
                            </el-table>
                        </el-tab-pane>

                        <el-tab-pane label="对话" name="chat">
                            <div class="chat-pane">
                                <div class="chat-main">
                                    <div class="chat-composer">
                                        <div class="chat-controls">
                                            <el-select v-model="chatModel" class="model-select" size="small">
                                                <el-option
                                                    v-for="model in tokenPlanModels"
                                                    :key="model.value"
                                                    :label="model.label"
                                                    :value="model.value"
                                                />
                                            </el-select>
                                            <el-button-group class="quick-actions">
                                                <el-button size="small" icon="Document" :disabled="currentBookChatLoading" @click="runQuickChat('summary')">总结本书</el-button>
                                                <el-button size="small" icon="TrendCharts" :disabled="currentBookChatLoading" @click="runQuickChat('analysis')">分析本书</el-button>
                                                <el-button size="small" icon="List" :disabled="currentBookChatLoading" @click="runQuickChat('actions')">行动清单</el-button>
                                                <el-button size="small" icon="Operation" :disabled="currentBookChatLoading" @click="runQuickChat('rules')">规则卡</el-button>
                                            </el-button-group>
                                        </div>
                                        <el-input
                                            v-model="chatQuestion"
                                            class="question-input"
                                            type="textarea"
                                            :rows="4"
                                            resize="none"
                                            placeholder="基于当前书籍提问，例如：这本书最值得落地的 3 条方法是什么？"
                                        />
                                        <div class="chat-submit">
                                            <el-button type="primary" icon="Promotion" :loading="currentBookChatLoading" @click="sendChat">
                                                发送
                                            </el-button>
                                            <span v-if="chatResponse" class="chat-stats">
                                                {{ chatResponse.model }} · chapters {{ chatResponse.context_stats.chapters }} · claims {{ chatResponse.context_stats.claims }} · chunks {{ chatResponse.context_stats.chunks }}
                                            </span>
                                        </div>
                                    </div>
                                    <div class="answer-panel answer-report" v-loading="currentBookChatLoading">
                                        <el-empty v-if="!chatResponse && !currentBookChatLoading" description="选择快捷按钮或输入问题" />
                                        <template v-if="chatResponse">
                                            <div class="answer-head">
                                                <el-radio-group v-model="answerView" size="small">
                                                    <el-radio-button label="rendered">渲染</el-radio-button>
                                                    <el-radio-button label="raw">Markdown</el-radio-button>
                                                </el-radio-group>
                                            </div>
                                            <div
                                                v-if="answerView === 'rendered'"
                                                class="answer-markdown"
                                                v-html="renderedChatAnswer"
                                            ></div>
                                            <div v-else class="answer-text">{{ chatResponse.answer }}</div>
                                            <div v-if="chatResponse.sources.length" class="source-list">
                                                <el-tag
                                                    v-for="source in chatResponse.sources"
                                                    :key="`${source.kind}:${source.id}`"
                                                    size="small"
                                                    effect="plain"
                                                >
                                                    {{ source.kind }}:{{ source.id }}
                                                </el-tag>
                                            </div>
                                        </template>
                                    </div>
                                </div>
                                <aside class="history-panel" v-loading="historyLoading">
                                    <div class="history-head">
                                        <div>
                                            <div class="panel-kicker">History</div>
                                            <div class="history-title">历史记录</div>
                                        </div>
                                        <el-button icon="Refresh" circle size="small" @click="loadChatHistory()" />
                                    </div>
                                    <el-empty
                                        v-if="!chatHistory.length && !historyLoading"
                                        description="暂无历史"
                                        :image-size="64"
                                    />
                                    <div v-else class="history-list">
                                        <button
                                            v-for="history in chatHistory"
                                            :key="history.id"
                                            type="button"
                                            class="history-item"
                                            :class="{ active: history.id === selectedHistoryID }"
                                            @click="restoreChatHistory(history)"
                                        >
                                            <div class="history-top">
                                                <el-tag size="small" effect="plain">{{ modeLabel(history.mode) }}</el-tag>
                                                <span class="history-time">{{ formatChatTime(history.created_at) }}</span>
                                            </div>
                                            <div class="history-question">{{ history.question || modeLabel(history.mode) }}</div>
                                            <div class="history-answer">{{ history.answer }}</div>
                                            <div class="history-meta">
                                                {{ history.model }} · {{ history.context_stats.claims }} claims · {{ history.context_stats.chunks }} chunks
                                            </div>
                                        </button>
                                    </div>
                                </aside>
                            </div>
                        </el-tab-pane>

                        <el-tab-pane label="Prompt模板" name="prompts">
                            <div class="prompt-studio">
                                <div class="prompt-toolbar">
                                    <div>
                                        <div class="panel-kicker">Prompt Studio</div>
                                        <div class="prompt-title">书籍分析模板</div>
                                    </div>
                                    <el-radio-group v-model="promptCategory" size="small">
                                        <el-radio-button label="全部">全部</el-radio-button>
                                        <el-radio-button
                                            v-for="category in promptCategories"
                                            :key="category"
                                            :label="category"
                                        >
                                            {{ category }}
                                        </el-radio-button>
                                    </el-radio-group>
                                </div>
                                <el-empty
                                    v-if="!filteredPrompts.length"
                                    description="暂无 Prompt 模板"
                                    :image-size="72"
                                />
                                <div v-else class="prompt-grid">
                                    <article
                                        v-for="prompt in filteredPrompts"
                                        :key="prompt.prompt_id"
                                        class="prompt-card"
                                    >
                                        <div class="prompt-card-head">
                                            <div>
                                                <div class="prompt-card-title">{{ prompt.title }}</div>
                                                <div class="prompt-card-desc">{{ prompt.description }}</div>
                                            </div>
                                            <el-tag
                                                size="small"
                                                effect="plain"
                                                :type="prompt.dynamic ? 'success' : 'info'"
                                            >
                                                {{ prompt.dynamic ? '动态' : prompt.category }}
                                            </el-tag>
                                        </div>
                                        <div class="prompt-preview">{{ prompt.prompt }}</div>
                                        <div class="prompt-card-foot">
                                            <el-tag size="small" effect="plain">{{ prompt.output_format || 'markdown' }}</el-tag>
                                            <div class="prompt-actions">
                                                <el-button size="small" icon="DocumentCopy" @click="copyPrompt(prompt)">复制</el-button>
                                                <el-button size="small" icon="EditPen" @click="insertPrompt(prompt)">填入</el-button>
                                                <el-button
                                                    size="small"
                                                    type="primary"
                                                    icon="Promotion"
                                                    :disabled="currentBookChatLoading"
                                                    @click="runPrompt(prompt)"
                                                >
                                                    运行
                                                </el-button>
                                            </div>
                                        </div>
                                    </article>
                                </div>
                            </div>
                        </el-tab-pane>

                        <el-tab-pane label="MCP" name="mcp">
                            <el-table :data="mcpTools" height="100%" table-layout="auto">
                                <el-table-column prop="name" label="Tool" width="180" />
                                <el-table-column prop="description" label="说明" min-width="420" />
                            </el-table>
                        </el-tab-pane>

                        <el-tab-pane label="NotebookLM" name="notebooklm">
                            <div class="notebooklm-panel" v-loading="notebookLMLoading">
                                <section class="bridge-card bridge-card-main">
                                    <div>
                                        <div class="panel-kicker">Bridge Package</div>
                                        <div class="bridge-title">NotebookLM 资料包</div>
                                        <div class="bridge-subtitle">
                                            {{ notebookLMBridge?.last_export_dir || '尚未导出' }}
                                        </div>
                                    </div>
                                    <div class="notebooklm-actions">
                                        <el-button
                                            type="primary"
                                            icon="FolderOpened"
                                            :loading="notebookLMExporting"
                                            @click="exportNotebookLMBridge"
                                        >
                                            导出资料包
                                        </el-button>
                                        <el-button icon="Link" @click="openNotebookLM">
                                            打开 NotebookLM
                                        </el-button>
                                        <el-button icon="DocumentCopy" @click="copyNotebookLMUploadGuide">
                                            复制上传指南
                                        </el-button>
                                    </div>
                                </section>

                                <section class="bridge-grid">
                                    <div class="bridge-card">
                                        <div class="panel-kicker">Notebook Link</div>
                                        <div class="bridge-title">保存 Notebook 链接</div>
                                        <div class="bridge-link-row">
                                            <el-input
                                                v-model="notebookLMLinkInput"
                                                clearable
                                                placeholder="https://notebooklm.google.com/..."
                                            />
                                            <el-button
                                                type="primary"
                                                plain
                                                icon="Check"
                                                :loading="notebookLMSaving"
                                                @click="saveNotebookLMLink"
                                            >
                                                保存
                                            </el-button>
                                        </div>
                                        <div v-if="notebookLMBridge?.notebook_url" class="bridge-saved-url">
                                            {{ notebookLMBridge.notebook_url }}
                                        </div>
                                    </div>

                                    <div class="bridge-card">
                                        <div class="panel-kicker">Export Files</div>
                                        <div class="bridge-title">最近导出</div>
                                        <el-empty
                                            v-if="!notebookLMBridge?.last_export_files?.length"
                                            description="暂无导出文件"
                                            :image-size="54"
                                        />
                                        <div v-else class="bridge-file-list">
                                            <div
                                                v-for="file in notebookLMBridge.last_export_files"
                                                :key="file"
                                                class="bridge-file"
                                            >
                                                {{ fileName(file) }}
                                            </div>
                                            <el-button
                                                size="small"
                                                icon="DocumentCopy"
                                                @click="copyNotebookLMExportDir"
                                            >
                                                复制目录
                                            </el-button>
                                        </div>
                                    </div>
                                </section>

                                <section class="bridge-card bridge-prompts">
                                    <div class="panel-kicker">Suggested Prompts</div>
                                    <div class="prompt-chip">总结本书的核心结论，并按章节给出依据。</div>
                                    <div class="prompt-chip">提取最值得落地的 10 条行动建议，标注来源 claim 或 chunk。</div>
                                    <div class="prompt-chip">把本书转换成可执行规则卡或项目知识库条目。</div>
                                </section>
                            </div>
                        </el-tab-pane>
                    </el-tabs>
                </template>
            </section>
        </div>
    </div>
</template>

<script lang="ts" setup>
import {computed, onMounted, reactive, ref} from 'vue'
import {ElMessage} from 'element-plus'
import { renderMarkdown } from '../utils/markdownRender.js'
import { BrowserOpenURL, ClipboardSetText } from '../../wailsjs/runtime'
import {
    BookKnowledgeChat,
    BookKnowledgeChatHistory,
    BookKnowledgeExport,
    BookKnowledgeGetBook,
    BookKnowledgeListBooks,
    BookKnowledgeMCPTools,
    BookKnowledgeNotebookLMBridge,
    BookKnowledgeNotebookLMExport,
    BookKnowledgeNotebookLMSaveLink,
    BookKnowledgePrompts,
    BookKnowledgeRoot,
    BookKnowledgeSearch
} from '../../wailsjs/go/backend/App'

interface BookKnowledgeBook {
    book_id: string
    dedao_id?: number
    enid?: string
    title: string
    author?: string
    source_html?: string
    status?: string
    extractor?: string
}

interface BookKnowledgeChapter {
    chapter_id: string
    book_id: string
    order: number
    title: string
    summary?: string
    chunk_ids?: string[]
}

interface BookKnowledgeChunk {
    chunk_id: string
    book_id: string
    chapter_id: string
    order: number
    text: string
    tokens?: number
}

interface BookKnowledgeClaim {
    claim_id: string
    title: string
    summary: string
    review_status?: string
    evidence_level?: string
}

interface BookKnowledgePackage {
    book: BookKnowledgeBook
    chapters: BookKnowledgeChapter[]
    chunks: BookKnowledgeChunk[]
    claims: BookKnowledgeClaim[]
}

interface BookKnowledgeSearchResult {
    kind: string
    title?: string
    snippet: string
    score: number
}

interface BookKnowledgeMCPTool {
    name: string
    description: string
}

interface BookKnowledgeChatSource {
    kind: string
    id: string
    title?: string
    chapter_id?: string
}

interface BookKnowledgeChatResponse {
    history_id?: string
    answer: string
    model: string
    mode: string
    sources: BookKnowledgeChatSource[]
    context_stats: {
        chapters: number
        claims: number
        chunks: number
        chars: number
    }
    created_at?: string
}

interface BookKnowledgeChatHistoryItem {
    id: string
    book_id: string
    book_title: string
    mode: string
    question: string
    model: string
    answer: string
    sources: BookKnowledgeChatSource[]
    context_stats: {
        chapters: number
        claims: number
        chunks: number
        chars: number
    }
    created_at: string
}

interface BookKnowledgePrompt {
    prompt_id: string
    category: string
    title: string
    description?: string
    prompt: string
    output_format?: string
    dynamic?: boolean
}

interface BookKnowledgeNotebookLMBridge {
    book_id: string
    notebook_url?: string
    last_export_dir?: string
    last_export_files?: string[]
    updated_at?: string
}

const notebookLMHomeURL = 'https://notebooklm.google.com/'
const rootPath = ref('')
const loadingBooks = ref(false)
const books = ref<BookKnowledgeBook[]>([])
const selectedBook = ref<BookKnowledgeBook | null>(null)
const bookPackage = ref<BookKnowledgePackage | null>(null)
const selectedChapter = ref<BookKnowledgeChapter | null>(null)
const activeTab = ref('chapters')
const searchText = ref('')
const searchResults = ref<BookKnowledgeSearchResult[]>([])
const mcpTools = ref<BookKnowledgeMCPTool[]>([])
const bookPrompts = ref<BookKnowledgePrompt[]>([])
const promptCategory = ref('全部')
const chatModel = ref('qwen3.7-max')
const chatQuestion = ref('')
const chatLoadingByBookID = reactive<Record<string, boolean>>({})
const chatResponse = ref<BookKnowledgeChatResponse | null>(null)
const chatHistory = ref<BookKnowledgeChatHistoryItem[]>([])
const historyLoading = ref(false)
const selectedHistoryID = ref('')
const answerView = ref('rendered')
const notebookLMBridge = ref<BookKnowledgeNotebookLMBridge | null>(null)
const notebookLMLinkInput = ref('')
const notebookLMLoading = ref(false)
const notebookLMExporting = ref(false)
const notebookLMSaving = ref(false)
const tokenPlanModels = [
    {label: 'MiniMax M2.5', value: 'MiniMax-M2.5'},
    {label: 'Qwen3.7 Max', value: 'qwen3.7-max'},
    {label: 'Qwen3.7 Plus', value: 'qwen3.7-plus'},
    {label: 'DeepSeek V4 Pro', value: 'deepseek-v4-pro'},
    {label: 'DeepSeek V4 Flash', value: 'deepseek-v4-flash'},
    {label: 'GLM 5.2', value: 'glm-5.2'},
]

const chapterChunks = computed(() => {
    if (!selectedChapter.value || !bookPackage.value) {
        return []
    }
    return bookPackage.value.chunks.filter((chunk) => chunk.chapter_id === selectedChapter.value?.chapter_id)
})

const renderedChatAnswer = computed(() => {
    return renderMarkdown(chatResponse.value?.answer || '')
})

const currentBookChatLoading = computed(() => {
    const bookID = selectedBook.value?.book_id || ''
    return Boolean(bookID && chatLoadingByBookID[bookID])
})

const promptCategories = computed(() => {
    const categories = new Set<string>()
    for (const prompt of bookPrompts.value) {
        if (prompt.category) {
            categories.add(prompt.category)
        }
    }
    return Array.from(categories)
})

const filteredPrompts = computed(() => {
    if (promptCategory.value === '全部') {
        return bookPrompts.value
    }
    return bookPrompts.value.filter((prompt) => prompt.category === promptCategory.value)
})

onMounted(() => {
    loadRoot()
    loadMCPTools()
    loadBooks()
})

const loadRoot = async () => {
    rootPath.value = await BookKnowledgeRoot()
}

const loadMCPTools = async () => {
    mcpTools.value = await BookKnowledgeMCPTools()
}

const loadBooks = async () => {
    loadingBooks.value = true
    try {
        const list = await BookKnowledgeListBooks()
        books.value = Array.isArray(list) ? list : []
        if (books.value.length > 0) {
            await selectBook(books.value[0])
        } else {
            selectedBook.value = null
            bookPackage.value = null
            selectedChapter.value = null
            chatHistory.value = []
            selectedHistoryID.value = ''
            notebookLMBridge.value = null
            notebookLMLinkInput.value = ''
            bookPrompts.value = []
            promptCategory.value = '全部'
        }
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    } finally {
        loadingBooks.value = false
    }
}

const selectBook = async (row: BookKnowledgeBook | null) => {
    if (!row) {
        return
    }
    selectedBook.value = row
    searchResults.value = []
    chatResponse.value = null
    chatHistory.value = []
    selectedHistoryID.value = ''
    notebookLMBridge.value = null
    notebookLMLinkInput.value = ''
    bookPrompts.value = []
    promptCategory.value = '全部'
    try {
        const pkg = await BookKnowledgeGetBook(row.book_id)
        bookPackage.value = pkg as BookKnowledgePackage
        selectedChapter.value = bookPackage.value.chapters?.[0] || null
        await loadChatHistory(row.book_id)
        await loadNotebookLMBridge(row.book_id)
        await loadBookPrompts(row.book_id)
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    }
}

const selectChapter = (row: BookKnowledgeChapter | null) => {
    selectedChapter.value = row
}

const searchKnowledge = async () => {
    const query = searchText.value.trim()
    if (!query) {
        searchResults.value = []
        return
    }
    try {
        const bookID = selectedBook.value?.book_id || ''
        searchResults.value = await BookKnowledgeSearch(query, bookID, 20)
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    }
}

const clearSearch = () => {
    searchResults.value = []
}

const exportKnowledge = async (target: string) => {
    if (!selectedBook.value) {
        return
    }
    try {
        const result = await BookKnowledgeExport(selectedBook.value.book_id, target)
        ElMessage({
            message: `已导出: ${result.output_dir}`,
            type: 'success',
        })
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    }
}

const loadBookPrompts = async (bookID?: string) => {
    const targetBookID = bookID || selectedBook.value?.book_id || ''
    if (!targetBookID) {
        return
    }
    try {
        const list = await BookKnowledgePrompts(targetBookID)
        if (selectedBook.value?.book_id === targetBookID) {
            bookPrompts.value = Array.isArray(list) ? (list as BookKnowledgePrompt[]) : []
            promptCategory.value = '全部'
        }
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    }
}

const copyPrompt = (prompt: BookKnowledgePrompt) => {
    ClipboardSetText(prompt.prompt)
    ElMessage({message: 'Prompt 已复制', type: 'success'})
}

const insertPrompt = (prompt: BookKnowledgePrompt) => {
    chatQuestion.value = prompt.prompt
    activeTab.value = 'chat'
}

const runPrompt = async (prompt: BookKnowledgePrompt) => {
    chatQuestion.value = prompt.prompt
    await callBookChat('chat', prompt.prompt)
}

const loadNotebookLMBridge = async (bookID?: string) => {
    const targetBookID = bookID || selectedBook.value?.book_id || ''
    if (!targetBookID) {
        return
    }
    notebookLMLoading.value = true
    try {
        const bridge = await BookKnowledgeNotebookLMBridge(targetBookID) as BookKnowledgeNotebookLMBridge
        if (selectedBook.value?.book_id === targetBookID) {
            notebookLMBridge.value = bridge
            notebookLMLinkInput.value = bridge.notebook_url || ''
        }
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    } finally {
        notebookLMLoading.value = false
    }
}

const exportNotebookLMBridge = async () => {
    const bookID = selectedBook.value?.book_id || ''
    if (!bookID) {
        return
    }
    notebookLMExporting.value = true
    try {
        const bridge = await BookKnowledgeNotebookLMExport(bookID) as BookKnowledgeNotebookLMBridge
        notebookLMBridge.value = bridge
        notebookLMLinkInput.value = bridge.notebook_url || ''
        ElMessage({message: `已导出 NotebookLM 资料包: ${bridge.last_export_dir}`, type: 'success'})
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    } finally {
        notebookLMExporting.value = false
    }
}

const openNotebookLM = () => {
    BrowserOpenURL(notebookLMBridge.value?.notebook_url || notebookLMHomeURL)
}

const saveNotebookLMLink = async () => {
    const bookID = selectedBook.value?.book_id || ''
    if (!bookID) {
        return
    }
    notebookLMSaving.value = true
    try {
        const bridge = await BookKnowledgeNotebookLMSaveLink(bookID, notebookLMLinkInput.value.trim()) as BookKnowledgeNotebookLMBridge
        notebookLMBridge.value = bridge
        notebookLMLinkInput.value = bridge.notebook_url || ''
        ElMessage({message: bridge.notebook_url ? 'NotebookLM 链接已保存' : 'NotebookLM 链接已清空', type: 'success'})
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    } finally {
        notebookLMSaving.value = false
    }
}

const copyNotebookLMExportDir = () => {
    const dir = notebookLMBridge.value?.last_export_dir || ''
    if (!dir) {
        return
    }
    ClipboardSetText(dir)
    ElMessage({message: '已复制导出目录', type: 'success'})
}

const copyNotebookLMUploadGuide = () => {
    const bookTitle = selectedBook.value?.title || '当前书籍'
    const dir = notebookLMBridge.value?.last_export_dir || '请先点击「导出资料包」生成目录'
    const files = notebookLMBridge.value?.last_export_files?.map(fileName) || ['book.md', 'claims.md', 'notebooklm-prompt.md', 'upload-guide.md']
    const guide = [
        `NotebookLM 上传指南：${bookTitle}`,
        '',
        `1. 打开 ${notebookLMBridge.value?.notebook_url || notebookLMHomeURL}`,
        '2. 新建或打开对应 notebook。',
        `3. 从目录上传资料：${dir}`,
        `4. 优先上传：${files.filter((file) => file === 'book.md' || file === 'claims.md').join('、') || 'book.md、claims.md'}`,
        '5. 打开 notebooklm-prompt.md，复制推荐问题到 NotebookLM 对话框。',
        '6. 回到 dedao-gui 保存 NotebookLM 链接，方便下次继续。',
    ].join('\n')
    ClipboardSetText(guide)
    ElMessage({message: '已复制上传指南', type: 'success'})
}

const fileName = (path: string) => {
    return path.split(/[\\/]/).filter(Boolean).pop() || path
}

const runQuickChat = async (mode: string) => {
    await callBookChat(mode, '')
}

const sendChat = async () => {
    await callBookChat('chat', chatQuestion.value.trim())
}

const loadChatHistory = async (bookID?: string) => {
    const targetBookID = bookID || selectedBook.value?.book_id || ''
    if (!targetBookID) {
        return
    }
    historyLoading.value = true
    try {
        const list = await BookKnowledgeChatHistory(targetBookID, 50)
        chatHistory.value = Array.isArray(list) ? list : []
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    } finally {
        historyLoading.value = false
    }
}

const restoreChatHistory = (history: BookKnowledgeChatHistoryItem) => {
    selectedHistoryID.value = history.id
    chatQuestion.value = history.question
    answerView.value = 'rendered'
    activeTab.value = 'chat'
    chatResponse.value = {
        history_id: history.id,
        answer: history.answer,
        model: history.model,
        mode: history.mode,
        sources: history.sources || [],
        context_stats: history.context_stats || {chapters: 0, claims: 0, chunks: 0, chars: 0},
        created_at: history.created_at,
    }
}

const modeLabel = (mode: string) => {
    const labels: Record<string, string> = {
        summary: '总结',
        analysis: '分析',
        actions: '行动',
        rules: '规则',
        chat: '问答',
    }
    return labels[mode] || '问答'
}

const formatChatTime = (value: string) => {
    if (!value) {
        return ''
    }
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) {
        return value
    }
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hour = String(date.getHours()).padStart(2, '0')
    const minute = String(date.getMinutes()).padStart(2, '0')
    return `${month}-${day} ${hour}:${minute}`
}

const setBookChatLoading = (bookID: string, loading: boolean) => {
    if (!bookID) {
        return
    }
    if (loading) {
        chatLoadingByBookID[bookID] = true
        return
    }
    delete chatLoadingByBookID[bookID]
}

const callBookChat = async (mode: string, question: string) => {
    const book = selectedBook.value
    if (!book) {
        return
    }
    if (mode === 'chat' && !question) {
        ElMessage({message: '请输入问题', type: 'warning'})
        return
    }
    const bookID = book.book_id
    if (chatLoadingByBookID[bookID]) {
        ElMessage({message: '当前书籍仍在分析中，可切换其他书继续并行发送', type: 'warning'})
        return
    }
    setBookChatLoading(bookID, true)
    chatResponse.value = null
    answerView.value = 'rendered'
    activeTab.value = 'chat'
    try {
        const response = await BookKnowledgeChat(
            bookID,
            mode,
            question,
            chatModel.value,
        ) as BookKnowledgeChatResponse
        if (selectedBook.value?.book_id === bookID) {
            chatResponse.value = response
            selectedHistoryID.value = response.history_id || ''
            await loadChatHistory(bookID)
        }
    } catch (error: any) {
        ElMessage({message: String(error), type: 'warning'})
    } finally {
        setBookChatLoading(bookID, false)
    }
}
</script>

<style scoped>
.book-knowledge {
    display: flex;
    flex-direction: column;
    height: calc(100vh - 92px);
    min-height: 520px;
    gap: 12px;
    color: #1f2937;
    text-align: left;
}

.workbench-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    min-height: 44px;
    gap: 10px;
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    padding: 7px 9px;
    background: #ffffff;
    box-shadow: 0 8px 18px rgb(15 23 42 / 4%);
}

.toolbar-left {
    display: flex;
    min-width: 0;
    flex: 1;
    align-items: center;
    gap: 10px;
}

.search-input {
    width: min(520px, 42vw);
}

.root-tag {
    max-width: 520px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    border-color: #d7deea;
    color: #64748b;
    background: #f8fafc;
}

.workspace {
    display: grid;
    grid-template-columns: minmax(320px, 34%) minmax(680px, 1fr);
    gap: 12px;
    min-height: 0;
    flex: 1;
}

.library-panel,
.research-panel {
    min-height: 0;
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    background: #ffffff;
    box-shadow: 0 10px 24px rgb(15 23 42 / 5%);
}

.library-panel {
    display: flex;
    flex-direction: column;
    padding: 12px;
}

.research-panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    padding: 14px 16px;
}

.panel-head,
.research-head {
    display: flex;
    justify-content: space-between;
    gap: 12px;
}

.panel-head {
    align-items: flex-end;
    margin-bottom: 10px;
}

.panel-kicker {
    color: #64748b;
    font-size: 11px;
    font-weight: 700;
    line-height: 14px;
    letter-spacing: 0;
    text-transform: uppercase;
}

.section-title {
    color: #111827;
    font-size: 17px;
    font-weight: 700;
    line-height: 24px;
}

.count-tag {
    border-color: #d7deea;
    color: #475569;
    background: #f8fafc;
}

.library-table {
    flex: 1;
    min-height: 0;
}

.library-table :deep(.el-table__header th.el-table__cell) {
    color: #64748b;
    font-size: 12px;
    font-weight: 700;
    background: #f8fafc;
}

.library-table :deep(.el-table__row.current-row > td.el-table__cell) {
    background: #eef5ff;
}

.book-title {
    display: -webkit-box;
    overflow: hidden;
    color: #1f2937;
    font-size: 13px;
    font-weight: 600;
    line-height: 18px;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
}

.book-meta,
.detail-meta,
.chunk-meta {
    display: flex;
    gap: 8px;
    color: #7a8699;
    font-size: 12px;
    line-height: 18px;
}

.research-head {
    align-items: flex-start;
    border-bottom: 1px solid #e5ebf3;
    padding-bottom: 12px;
    margin-bottom: 10px;
}

.book-heading {
    min-width: 0;
}

.actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
}

.detail-title {
    overflow: hidden;
    color: #111827;
    font-size: 18px;
    font-weight: 750;
    line-height: 26px;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.detail-meta {
    flex-wrap: wrap;
    max-width: 100%;
    margin-top: 4px;
}

.search-results {
    min-height: 250px;
    margin-bottom: 10px;
}

.detail-tabs {
    min-height: 0;
    flex: 1;
}

.detail-tabs :deep(.el-tabs__header) {
    margin-bottom: 12px;
}

.detail-tabs :deep(.el-tabs__nav-wrap::after) {
    height: 1px;
    background: #e5ebf3;
}

.detail-tabs :deep(.el-tabs__item) {
    color: #4b5563;
    font-weight: 650;
}

.detail-tabs :deep(.el-tabs__item.is-active) {
    color: #2563eb;
}

.detail-tabs :deep(.el-tabs__content) {
    height: calc(100% - 48px);
}

.detail-tabs :deep(.el-tab-pane) {
    height: 100%;
}

.chapter-grid {
    display: grid;
    grid-template-columns: minmax(300px, 42%) minmax(360px, 1fr);
    gap: 12px;
    height: 100%;
    min-height: 0;
}

.chapter-panel {
    min-height: 0;
    overflow: auto;
    border: 1px solid #e5ebf3;
    border-radius: 8px;
    padding: 12px;
    background: #fbfdff;
}

.panel-title {
    color: #1f2937;
    font-size: 15px;
    font-weight: 700;
    line-height: 24px;
    margin-bottom: 8px;
}

.chunk-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.chunk-row {
    border-bottom: 1px solid #e5ebf3;
    padding-bottom: 10px;
}

.chunk-text {
    color: #273449;
    font-size: 13px;
    line-height: 1.65;
    white-space: pre-wrap;
}

.chat-pane {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    gap: 12px;
    height: 100%;
    min-height: 0;
}

.chat-main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    gap: 12px;
}

.chat-composer {
    display: flex;
    flex-direction: column;
    gap: 10px;
    border: 1px solid #dbe4f0;
    border-radius: 8px;
    padding: 12px;
    background: #f8fafc;
}

.chat-controls,
.chat-submit {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
}

.quick-actions {
    display: flex;
    flex-wrap: wrap;
}

.quick-actions :deep(.el-button) {
    font-weight: 600;
}

.question-input :deep(.el-textarea__inner) {
    border-color: #cdd8e6;
    border-radius: 8px;
    color: #1f2937;
    line-height: 1.65;
    box-shadow: none;
}

.question-input :deep(.el-textarea__inner:focus) {
    border-color: #3b82f6;
    box-shadow: 0 0 0 2px rgb(59 130 246 / 12%);
}

.model-select {
    width: 190px;
}

.chat-stats {
    color: #7a8699;
    font-size: 12px;
}

.history-panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    padding: 12px;
    background: #fbfdff;
}

.history-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px;
    border-bottom: 1px solid #e5ebf3;
    padding-bottom: 10px;
    margin-bottom: 10px;
}

.history-title {
    color: #111827;
    font-size: 15px;
    font-weight: 700;
    line-height: 22px;
}

.history-list {
    display: flex;
    flex-direction: column;
    min-height: 0;
    gap: 8px;
    overflow: auto;
}

.history-item {
    width: 100%;
    border: 1px solid #e2e8f0;
    border-radius: 8px;
    padding: 10px;
    background: #ffffff;
    cursor: pointer;
    text-align: left;
    transition: border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.history-item:hover {
    border-color: #bfdbfe;
    background: #f8fbff;
}

.history-item.active {
    border-color: #3b82f6;
    background: #eff6ff;
    box-shadow: 0 0 0 2px rgb(59 130 246 / 10%);
}

.history-top,
.history-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
}

.history-time,
.history-meta {
    color: #7a8699;
    font-size: 12px;
}

.history-question {
    display: -webkit-box;
    overflow: hidden;
    margin-top: 8px;
    color: #1f2937;
    font-size: 13px;
    font-weight: 700;
    line-height: 18px;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
}

.history-answer {
    display: -webkit-box;
    overflow: hidden;
    margin-top: 6px;
    color: #475569;
    font-size: 12px;
    line-height: 17px;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
}

.history-meta {
    margin-top: 8px;
}

.answer-panel {
    min-height: 0;
    flex: 1;
    overflow: auto;
}

.answer-report {
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    padding: 18px 22px;
    background: #ffffff;
    text-align: left;
}

.answer-head {
    display: flex;
    justify-content: flex-end;
    position: sticky;
    z-index: 1;
    top: 0;
    margin: -4px 0 12px;
    padding-bottom: 8px;
    background: #ffffff;
    border-bottom: 1px solid #eef2f7;
}

.answer-text {
    color: #1f2937;
    font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif;
    font-size: 14px;
    line-height: 1.75;
    text-align: left;
    white-space: pre-wrap;
}

.answer-markdown {
    width: 100%;
    max-width: none;
    margin: 0;
    color: #1f2937;
    font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif;
    font-size: 14px;
    line-height: 1.82;
    text-align: left;
}

.answer-markdown :deep(*) {
    text-align: left;
}

.answer-markdown :deep(h1),
.answer-markdown :deep(h2),
.answer-markdown :deep(h3),
.answer-markdown :deep(h4) {
    color: #111827;
    font-weight: 700;
    line-height: 1.35;
    margin: 20px 0 10px;
    text-align: left;
}

.answer-markdown :deep(h1) {
    border-bottom: 1px solid #dbe4f0;
    padding-bottom: 10px;
    font-size: 22px;
}

.answer-markdown :deep(h2) {
    border-top: 1px solid #e5ebf3;
    padding-top: 18px;
    font-size: 19px;
}

.answer-markdown :deep(h3) {
    border-left: 4px solid #2563eb;
    padding-left: 10px;
    font-size: 16px;
}

.answer-markdown :deep(p) {
    margin: 10px 0 12px;
    color: #263244;
    line-height: 1.86;
    text-align: left;
}

.answer-markdown :deep(ul),
.answer-markdown :deep(ol) {
    margin: 10px 0 14px;
    padding-left: 22px;
    text-align: left;
}

.answer-markdown :deep(li) {
    margin: 7px 0;
    padding-left: 2px;
    line-height: 1.76;
    text-align: left;
}

.answer-markdown :deep(strong) {
    font-weight: 700;
    color: #111827;
}

.answer-markdown :deep(code) {
    border-radius: 4px;
    padding: 1px 4px;
    background: #eef2f7;
    color: #be123c;
    font-family: "JetBrains Mono", monospace;
    font-size: 12px;
}

.answer-markdown :deep(pre) {
    overflow: auto;
    border-radius: 6px;
    padding: 10px;
    background: #111827;
    color: #f5f7fa;
}

.answer-markdown :deep(pre code) {
    padding: 0;
    background: transparent;
    color: inherit;
}

.answer-markdown :deep(blockquote) {
    margin: 12px 0;
    padding: 8px 12px;
    border-left: 3px solid #0f766e;
    border-radius: 0 6px 6px 0;
    background: #f0fdfa;
    color: #334155;
    text-align: left;
}

.answer-markdown :deep(hr) {
    height: 1px;
    border: 0;
    margin: 22px 0;
    background: #d8e1ed;
}

.answer-markdown :deep(table) {
    display: table;
    width: 100%;
    margin: 14px 0 18px;
    border-collapse: collapse;
    border: 1px solid #dfe7f2;
    border-radius: 8px;
    color: #263244;
    font-size: 13px;
    line-height: 1.62;
    text-align: left;
}

.answer-markdown :deep(th),
.answer-markdown :deep(td) {
    border: 1px solid #e5ebf3;
    padding: 9px 11px;
    vertical-align: top;
    text-align: left;
}

.answer-markdown :deep(th) {
    color: #111827;
    font-weight: 700;
    background: #f6f8fb;
}

.answer-markdown :deep(tbody tr:nth-child(even)) {
    background: #fbfdff;
}

.source-list {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 12px;
    padding-top: 10px;
    border-top: 1px solid #e5ebf3;
}

.prompt-studio {
    display: flex;
    flex-direction: column;
    gap: 12px;
    height: 100%;
    min-height: 0;
    overflow: auto;
}

.prompt-toolbar {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    padding: 12px;
    background: #f8fafc;
}

.prompt-title {
    color: #111827;
    font-size: 16px;
    font-weight: 750;
    line-height: 24px;
}

.prompt-toolbar .el-radio-group {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 4px;
}

.prompt-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 12px;
    min-height: 0;
}

.prompt-card {
    display: flex;
    flex-direction: column;
    min-height: 228px;
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    padding: 12px;
    background: #ffffff;
    box-shadow: 0 8px 18px rgb(15 23 42 / 4%);
}

.prompt-card-head,
.prompt-card-foot {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
}

.prompt-card-title {
    color: #111827;
    font-size: 14px;
    font-weight: 750;
    line-height: 20px;
}

.prompt-card-desc {
    margin-top: 2px;
    color: #64748b;
    font-size: 12px;
    line-height: 18px;
}

.prompt-preview {
    display: -webkit-box;
    overflow: hidden;
    flex: 1;
    margin: 12px 0;
    color: #334155;
    font-size: 12px;
    line-height: 1.65;
    -webkit-line-clamp: 6;
    -webkit-box-orient: vertical;
}

.prompt-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 6px;
}

.notebooklm-panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
    height: 100%;
    min-height: 0;
    overflow: auto;
}

.bridge-card {
    border: 1px solid #dfe5ef;
    border-radius: 8px;
    padding: 14px;
    background: #ffffff;
}

.bridge-card-main {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 14px;
    background: linear-gradient(180deg, #ffffff 0%, #f8fbff 100%);
}

.bridge-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(280px, 36%);
    gap: 12px;
    min-height: 0;
}

.bridge-title {
    color: #111827;
    font-size: 16px;
    font-weight: 750;
    line-height: 24px;
}

.bridge-subtitle,
.bridge-saved-url {
    overflow: hidden;
    margin-top: 4px;
    color: #64748b;
    font-size: 12px;
    line-height: 18px;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.notebooklm-actions,
.bridge-link-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.notebooklm-actions {
    flex-wrap: wrap;
    justify-content: flex-end;
}

.bridge-link-row {
    margin-top: 12px;
}

.bridge-file-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 10px;
}

.bridge-file {
    overflow: hidden;
    border: 1px solid #e5ebf3;
    border-radius: 6px;
    padding: 7px 9px;
    background: #f8fafc;
    color: #273449;
    font-family: "JetBrains Mono", monospace;
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.bridge-prompts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
}

.prompt-chip {
    border: 1px solid #d8e2ef;
    border-radius: 999px;
    padding: 6px 10px;
    background: #f8fafc;
    color: #344256;
    font-size: 12px;
    line-height: 18px;
}

@media (max-width: 980px) {
    .book-knowledge {
        height: auto;
    }

    .workbench-toolbar,
    .toolbar-left,
    .workspace,
    .chapter-grid,
    .chat-pane,
    .prompt-toolbar,
    .prompt-card-foot,
    .bridge-card-main,
    .bridge-grid,
    .bridge-link-row {
        display: flex;
        flex-direction: column;
    }

    .search-input {
        width: 100%;
    }

    .library-panel,
    .research-panel {
        min-height: 360px;
    }

    .detail-title {
        white-space: normal;
    }
}
</style>
