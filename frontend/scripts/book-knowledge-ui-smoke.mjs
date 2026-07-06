import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const vuePath = join(here, '../src/views/BookKnowledge.vue')
const source = readFileSync(vuePath, 'utf8')

for (const hook of [
  'knowledge-shell',
  'library-panel',
  'research-panel',
  'chat-composer',
  'history-panel',
  'history-list',
  'answer-report',
  'answer-running',
  'answer-body',
  'prompt-picker',
  'prompt-select',
  'notebooklm-panel',
  'notebooklm-actions',
]) {
  assert.ok(source.includes(hook), `BookKnowledge.vue should include ${hook}`)
}

assert.ok(source.includes('BookKnowledgeChatHistory'), 'BookKnowledge.vue should load chat history through Wails')
assert.ok(source.includes('BookKnowledgePrompts'), 'BookKnowledge.vue should load book prompts through Wails')
assert.ok(source.includes('BookKnowledgeNotebookLMBridge'), 'BookKnowledge.vue should load NotebookLM bridge metadata')
assert.ok(source.includes('BookKnowledgeNotebookLMExport'), 'BookKnowledge.vue should export NotebookLM bridge packages')
assert.ok(source.includes('BookKnowledgeNotebookLMSaveLink'), 'BookKnowledge.vue should save NotebookLM links')
assert.ok(source.includes('BrowserOpenURL'), 'BookKnowledge.vue should open NotebookLM in the browser')
assert.ok(source.includes('copyNotebookLMUploadGuide'), 'BookKnowledge.vue should copy NotebookLM upload guide')
assert.ok(source.includes('复制上传指南'), 'BookKnowledge.vue should expose an upload-guide copy action')
assert.ok(source.includes("const activeTab = ref('chat')"), 'BookKnowledge.vue should open the chat tab by default')
const firstTabPane = source.match(/<el-tab-pane[^>]*label="([^"]+)"/)
assert.equal(firstTabPane?.[1], '对话', 'BookKnowledge.vue should render the chat tab first')
assert.ok(source.includes('selectedPromptID'), 'BookKnowledge.vue should track the selected prompt template')
assert.ok(source.includes('promptGroups'), 'BookKnowledge.vue should group prompt templates in a dropdown')
assert.ok(source.includes('applySelectedPrompt'), 'BookKnowledge.vue should insert selected prompt templates into the composer')
assert.ok(source.includes('runSelectedPrompt'), 'BookKnowledge.vue should run selected prompt templates immediately')
assert.ok(source.includes('popper-class="book-prompt-popper"'), 'prompt dropdown should use a dedicated popper class')
assert.ok(source.includes(':global(.book-prompt-popper .el-select-dropdown__item)'), 'prompt dropdown options should have dedicated global styling')
assert.ok(/:global\(\.book-prompt-popper\)[\s\S]{0,220}text-align:\s*left/.test(source), 'prompt dropdown popper should be left aligned')
assert.ok(!source.includes('<el-tab-pane label="Prompt模板"'), 'BookKnowledge.vue should not expose prompt templates as a separate tab')
assert.ok(!source.includes('prompt-card'), 'BookKnowledge.vue should not render prompt templates as cards')
assert.ok(source.includes("const chatModel = ref('qwen3.7-max')"), 'BookKnowledge.vue should default to Qwen3.7 Max')
assert.ok(source.includes('chatLoadingByBookID'), 'BookKnowledge.vue should track chat loading by book id')
assert.ok(source.includes('currentBookChatLoading'), 'BookKnowledge.vue should expose current book loading state')
assert.ok(source.includes('selectedBook.value?.book_id === bookID'), 'BookKnowledge.vue should only write async chat results to the originating book')
assert.ok(!source.includes('const chatLoading = ref(false)'), 'BookKnowledge.vue should not use one global chat loading flag')
assert.ok(!source.includes('answer-report" v-loading="currentBookChatLoading"'), 'answer panel should not mask existing conclusions while running')
assert.ok(!source.includes('chatResponse.value = null\n    answerView.value'), 'starting a new analysis should keep the previous conclusion visible')
assert.ok(source.includes(':disabled="currentBookChatLoading" @click="sendChat"'), 'send button should be disabled while running')
assert.ok(!source.includes(':loading="currentBookChatLoading" @click="sendChat"'), 'send button should not show a primary loading spinner while running')
assert.ok(/\.answer-panel\s*\{[\s\S]{0,220}overflow:\s*hidden/.test(source), 'answer panel should not scroll under its toolbar')
assert.ok(/\.answer-body\s*\{[\s\S]{0,220}overflow:\s*auto/.test(source), 'answer body should own the scroll area')
assert.ok(!/\.answer-head\s*\{[\s\S]{0,220}position:\s*sticky/.test(source), 'answer toolbar should not overlay answer content')
assert.ok(/\.book-knowledge[\s\S]{0,260}text-align:\s*left/.test(source), 'book knowledge page should override global centered text')
assert.ok(/\.answer-markdown\s*\{[\s\S]{0,260}text-align:\s*left/.test(source), 'rendered markdown should be left aligned')
assert.ok(source.includes('.answer-markdown :deep(table)'), 'rendered markdown should style tables')
assert.ok(!/answer-markdown[\s\S]{0,220}text-align:\s*center/.test(source), 'answer markdown should not be center aligned')

console.log('book knowledge UI smoke passed')
