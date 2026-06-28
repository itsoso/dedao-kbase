import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const appPath = join(here, '../src/App.vue')
const apiPath = join(here, '../src/api.ts')

const appSource = readFileSync(appPath, 'utf8')
const apiSource = readFileSync(apiPath, 'utf8')

for (const hook of [
  'kbase-web-shell',
  'connection-bar',
  'book-rail',
  'book-pagination',
  'library-search-panel',
  'chat-panel',
  'detail-panel',
  'system-kb-panel',
  'model-select',
  'column-resizer',
  'compact-detail-summary',
  'answer-markdown',
]) {
  assert.ok(appSource.includes(hook), `App.vue should include ${hook}`)
}

for (const surface of [
  'baseUrl',
  'token',
  'listBooksPage',
  'getBook',
  'getBrowserSession',
  'combinedSearchQuery',
  'runLibrarySearch',
  'renderedChatAnswer',
  'searchKnowledge',
  'getBookPrompts',
  'chatWithBook',
  'getBookChatHistory',
  'getSystemKBManifest',
  'getSystemKBExport',
]) {
  assert.ok(appSource.includes(surface), `App.vue should reference ${surface}`)
}

assert.ok(appSource.includes('localStorage'), 'App.vue should persist connection settings')
assert.ok(appSource.includes('Overview'), 'App.vue should expose overview details')
assert.ok(appSource.includes('Chapters'), 'App.vue should expose chapter details')
assert.ok(appSource.includes('Claims'), 'App.vue should expose claim details')
assert.ok(appSource.includes('Chunks'), 'App.vue should expose chunk details')
assert.ok(appSource.includes('System KB'), 'App.vue should expose system KB details')
assert.ok(appSource.includes('chatHistory'), 'App.vue should expose chat history')
assert.ok(appSource.includes('promptTemplates'), 'App.vue should expose prompt templates')
assert.ok(appSource.includes('layoutColumns'), 'App.vue should persist draggable column widths')
assert.ok(appSource.includes('qwen3.7-max'), 'App.vue should default to Qwen 3.7 Max')
assert.ok(appSource.includes('renderMarkdown'), 'App.vue should render Markdown answers')
assert.ok(apiSource.includes('class KBaseClient'), 'api.ts should define KBaseClient')
assert.ok(readFileSync(join(here, '../src/utils/markdownRender.ts'), 'utf8').includes('marked'), 'markdownRender should use marked')
assert.ok(appSource.includes('bookTotalPages'), 'App.vue should track paginated book totals')

assert.ok(apiSource.includes('/browser/session-token'), 'api.ts should request the browser session token endpoint')
assert.ok(apiSource.includes('BookKnowledgeBooksPage'), 'api.ts should type paginated book results')
assert.ok(apiSource.includes('BookKnowledgePrompt'), 'api.ts should type prompt templates')
assert.ok(apiSource.includes('BookKnowledgeChatResponse'), 'api.ts should type chat responses')
assert.ok(apiSource.includes('BookKnowledgeChatHistoryItem'), 'api.ts should type chat history')
assert.ok(apiSource.includes("credentials: 'same-origin'"), 'api.ts should include browser credentials for the session token endpoint')
assert.ok(/Authorization['"]?\s*:\s*`Bearer \$\{this\.token\}`/.test(apiSource), 'api.ts should attach Bearer token')
assert.ok(apiSource.includes('HTTP ${response.status}'), 'api.ts should include status in failed request errors')
assert.ok(apiSource.includes('await response.text()'), 'api.ts should include response body in failed request errors')
assert.ok(apiSource.includes('encodeURIComponent'), 'api.ts should encode query parameters')
assert.ok(apiSource.includes("method: 'POST'"), 'api.ts should POST chat requests')

console.log('web kbase UI smoke passed')
