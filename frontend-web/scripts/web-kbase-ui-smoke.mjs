import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const appPath = join(here, '../src/App.vue')
const apiPath = join(here, '../src/api.ts')
const routerPath = join(here, '../src/router.ts')
const workbenchPath = join(here, '../src/views/KBaseWorkbench.vue')
const moduleLandingPath = join(here, '../src/views/ModuleLanding.vue')
const accountProfilePath = join(here, '../src/views/AccountProfile.vue')

assert.ok(existsSync(routerPath), 'router.ts should define the Web GUI routes')
assert.ok(existsSync(workbenchPath), 'KBaseWorkbench.vue should host the KBase workbench route')
assert.ok(existsSync(moduleLandingPath), 'ModuleLanding.vue should host non-KBase module routes')
assert.ok(existsSync(accountProfilePath), 'AccountProfile.vue should host the Web personal center route')

const appSource = readFileSync(appPath, 'utf8')
const apiSource = readFileSync(apiPath, 'utf8')
const routerSource = readFileSync(routerPath, 'utf8')
const workbenchSource = readFileSync(workbenchPath, 'utf8')
const accountProfileSource = readFileSync(accountProfilePath, 'utf8')

assert.ok(appSource.includes('dedao-web-shell'), 'App.vue should render the Dedao Web shell')
assert.ok(appSource.includes('router-view'), 'App.vue should render routed pages')
assert.ok(appSource.includes('router-link'), 'App.vue should expose shell navigation links')

for (const routePath of [
  '/home',
  '/course',
  '/odob',
  '/ebook',
  '/knowledge',
  '/book-knowledge',
  '/compass',
  '/setting',
  '/user/login',
  '/user/profile',
  '/user/switch',
]) {
  assert.ok(routerSource.includes(routePath), `router.ts should include ${routePath}`)
}

for (const hook of [
  'kbase-web-shell',
  'connection-bar',
  'app-navigation',
  'book-rail',
  'book-pagination',
  'library-search-panel',
  'chat-panel',
  'detail-panel',
  'interop-panel',
  'jobs-panel',
  'ops-panel',
  'system-kb-panel',
  'model-select',
  'column-resizer',
  'compact-detail-summary',
  'answer-markdown',
]) {
  assert.ok(workbenchSource.includes(hook), `KBaseWorkbench.vue should include ${hook}`)
}

for (const surface of [
  'baseUrl',
  'token',
  'listBooksPage',
  'getBook',
  'getBrowserSession',
  'navigationItems',
  'navigateTo',
  'combinedSearchQuery',
  'runLibrarySearch',
  'renderedChatAnswer',
  'searchKnowledge',
  'getBookPrompts',
  'chatWithBook',
  'getBookChatHistory',
  'listJobs',
  'createJob',
  'getJob',
  'jobType',
  'getSystemKBManifest',
  'getSystemKBExport',
]) {
  assert.ok(workbenchSource.includes(surface), `KBaseWorkbench.vue should reference ${surface}`)
}

assert.ok(workbenchSource.includes('localStorage'), 'KBaseWorkbench.vue should persist connection settings')
assert.ok(workbenchSource.includes('Overview'), 'KBaseWorkbench.vue should expose overview details')
assert.ok(workbenchSource.includes('Chapters'), 'KBaseWorkbench.vue should expose chapter details')
assert.ok(workbenchSource.includes('Claims'), 'KBaseWorkbench.vue should expose claim details')
assert.ok(workbenchSource.includes('Chunks'), 'KBaseWorkbench.vue should expose chunk details')
assert.ok(workbenchSource.includes('System KB'), 'KBaseWorkbench.vue should expose system KB details')
assert.ok(workbenchSource.includes('Jobs'), 'KBaseWorkbench.vue should expose jobs details')
assert.ok(workbenchSource.includes('Skills/API'), 'KBaseWorkbench.vue should expose skills/API navigation')
assert.ok(workbenchSource.includes('Ops'), 'KBaseWorkbench.vue should expose ops navigation')
assert.ok(workbenchSource.includes('/.well-known/dedao-kbase-skills.json'), 'KBaseWorkbench.vue should expose skill discovery route')
assert.ok(workbenchSource.includes('chatHistory'), 'KBaseWorkbench.vue should expose chat history')
assert.ok(workbenchSource.includes('promptTemplates'), 'KBaseWorkbench.vue should expose prompt templates')
assert.ok(workbenchSource.includes('layoutColumns'), 'KBaseWorkbench.vue should persist draggable column widths')
assert.ok(workbenchSource.includes('qwen3.7-max'), 'KBaseWorkbench.vue should default to Qwen 3.7 Max')
assert.ok(workbenchSource.includes('renderMarkdown'), 'KBaseWorkbench.vue should render Markdown answers')
assert.ok(apiSource.includes('class KBaseClient'), 'api.ts should define KBaseClient')
assert.ok(readFileSync(join(here, '../src/utils/markdownRender.ts'), 'utf8').includes('marked'), 'markdownRender should use marked')
assert.ok(workbenchSource.includes('bookTotalPages'), 'KBaseWorkbench.vue should track paginated book totals')

assert.ok(apiSource.includes('/browser/session-token'), 'api.ts should request the browser session token endpoint')
assert.ok(apiSource.includes('BookKnowledgeBooksPage'), 'api.ts should type paginated book results')
assert.ok(apiSource.includes('BookKnowledgePrompt'), 'api.ts should type prompt templates')
assert.ok(apiSource.includes('BookKnowledgeChatResponse'), 'api.ts should type chat responses')
assert.ok(apiSource.includes('BookKnowledgeChatHistoryItem'), 'api.ts should type chat history')
assert.ok(apiSource.includes('BookKnowledgeJob'), 'api.ts should type online jobs')
assert.ok(apiSource.includes('DedaoSession'), 'api.ts should type Dedao account session')
assert.ok(apiSource.includes('/api/jobs'), 'api.ts should call jobs endpoints')
assert.ok(apiSource.includes('/api/dedao/session'), 'api.ts should call the Dedao session endpoint')
assert.ok(apiSource.includes("credentials: 'same-origin'"), 'api.ts should include browser credentials for the session token endpoint')
assert.ok(/Authorization['"]?\s*:\s*`Bearer \$\{this\.token\}`/.test(apiSource), 'api.ts should attach Bearer token')
assert.ok(apiSource.includes('HTTP ${response.status}'), 'api.ts should include status in failed request errors')
assert.ok(apiSource.includes('await response.text()'), 'api.ts should include response body in failed request errors')
assert.ok(apiSource.includes('encodeURIComponent'), 'api.ts should encode query parameters')
assert.ok(apiSource.includes("method: 'POST'"), 'api.ts should POST chat requests')
assert.ok(apiSource.includes('getDedaoSession'), 'api.ts should expose a Dedao session client method')
assert.ok(routerSource.includes('AccountProfile'), 'router.ts should route personal center to AccountProfile')
assert.ok(accountProfileSource.includes('account-profile'), 'AccountProfile.vue should expose the account profile surface')
assert.ok(accountProfileSource.includes('getDedaoSession'), 'AccountProfile.vue should load server-side Dedao session')
assert.ok(accountProfileSource.includes('user_count'), 'AccountProfile.vue should render configured user count')

console.log('web kbase UI smoke passed')
