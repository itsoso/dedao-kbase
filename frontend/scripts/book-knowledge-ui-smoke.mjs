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
  'notebooklm-panel',
  'notebooklm-actions',
]) {
  assert.ok(source.includes(hook), `BookKnowledge.vue should include ${hook}`)
}

assert.ok(source.includes('BookKnowledgeChatHistory'), 'BookKnowledge.vue should load chat history through Wails')
assert.ok(source.includes('BookKnowledgeNotebookLMBridge'), 'BookKnowledge.vue should load NotebookLM bridge metadata')
assert.ok(source.includes('BookKnowledgeNotebookLMExport'), 'BookKnowledge.vue should export NotebookLM bridge packages')
assert.ok(source.includes('BookKnowledgeNotebookLMSaveLink'), 'BookKnowledge.vue should save NotebookLM links')
assert.ok(source.includes('BrowserOpenURL'), 'BookKnowledge.vue should open NotebookLM in the browser')
assert.ok(source.includes("const chatModel = ref('qwen3.7-max')"), 'BookKnowledge.vue should default to Qwen3.7 Max')
assert.ok(source.includes('chatLoadingByBookID'), 'BookKnowledge.vue should track chat loading by book id')
assert.ok(source.includes('currentBookChatLoading'), 'BookKnowledge.vue should expose current book loading state')
assert.ok(source.includes('selectedBook.value?.book_id === bookID'), 'BookKnowledge.vue should only write async chat results to the originating book')
assert.ok(!source.includes('const chatLoading = ref(false)'), 'BookKnowledge.vue should not use one global chat loading flag')
assert.ok(/\.book-knowledge[\s\S]{0,260}text-align:\s*left/.test(source), 'book knowledge page should override global centered text')
assert.ok(/\.answer-markdown\s*\{[\s\S]{0,260}text-align:\s*left/.test(source), 'rendered markdown should be left aligned')
assert.ok(source.includes('.answer-markdown :deep(table)'), 'rendered markdown should style tables')
assert.ok(!/answer-markdown[\s\S]{0,220}text-align:\s*center/.test(source), 'answer markdown should not be center aligned')

console.log('book knowledge UI smoke passed')
