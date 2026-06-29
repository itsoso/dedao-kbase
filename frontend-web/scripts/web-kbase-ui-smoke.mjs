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
const courseLibraryPath = join(here, '../src/views/CourseLibrary.vue')
const ebookLibraryPath = join(here, '../src/views/EbookLibrary.vue')
const courseDetailReaderPath = join(here, '../src/views/CourseDetailReader.vue')
const ebookDetailReaderPath = join(here, '../src/views/EbookDetailReader.vue')
const accountProfilePath = join(here, '../src/views/AccountProfile.vue')
const accountLoginPath = join(here, '../src/views/AccountLogin.vue')
const webSettingsPath = join(here, '../src/views/WebSettings.vue')

assert.ok(existsSync(routerPath), 'router.ts should define the Web GUI routes')
assert.ok(existsSync(workbenchPath), 'KBaseWorkbench.vue should host the KBase workbench route')
assert.ok(existsSync(moduleLandingPath), 'ModuleLanding.vue should host non-KBase module routes')
assert.ok(existsSync(courseLibraryPath), 'CourseLibrary.vue should host the Web course route')
assert.ok(existsSync(ebookLibraryPath), 'EbookLibrary.vue should host the Web ebook bookshelf route')
assert.ok(existsSync(courseDetailReaderPath), 'CourseDetailReader.vue should host course detail reading')
assert.ok(existsSync(ebookDetailReaderPath), 'EbookDetailReader.vue should host ebook detail reading')
assert.ok(existsSync(accountProfilePath), 'AccountProfile.vue should host the Web personal center route')
assert.ok(existsSync(accountLoginPath), 'AccountLogin.vue should host the Web QR login route')
assert.ok(existsSync(webSettingsPath), 'WebSettings.vue should host global Web connection settings')

const appSource = readFileSync(appPath, 'utf8')
const apiSource = readFileSync(apiPath, 'utf8')
const routerSource = readFileSync(routerPath, 'utf8')
const workbenchSource = readFileSync(workbenchPath, 'utf8')
const moduleLandingSource = readFileSync(moduleLandingPath, 'utf8')
const courseLibrarySource = existsSync(courseLibraryPath) ? readFileSync(courseLibraryPath, 'utf8') : ''
const ebookLibrarySource = existsSync(ebookLibraryPath) ? readFileSync(ebookLibraryPath, 'utf8') : ''
const courseDetailReaderSource = existsSync(courseDetailReaderPath) ? readFileSync(courseDetailReaderPath, 'utf8') : ''
const ebookDetailReaderSource = existsSync(ebookDetailReaderPath) ? readFileSync(ebookDetailReaderPath, 'utf8') : ''
const accountProfileSource = readFileSync(accountProfilePath, 'utf8')
const accountLoginSource = readFileSync(accountLoginPath, 'utf8')
const webSettingsSource = existsSync(webSettingsPath) ? readFileSync(webSettingsPath, 'utf8') : ''

assert.ok(appSource.includes('dedao-web-shell'), 'App.vue should render the Dedao Web shell')
assert.ok(appSource.includes('compact-shell-nav'), 'App.vue should render compact shell navigation')
assert.ok(appSource.includes('router-view'), 'App.vue should render routed pages')
assert.ok(appSource.includes('router-link'), 'App.vue should expose shell navigation links')
assert.ok(moduleLandingSource.includes('module-landing-compact'), 'ModuleLanding.vue should use the compact module layout')
assert.ok(moduleLandingSource.includes('module-summary-row'), 'ModuleLanding.vue should render a compact summary row')

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

assert.ok(!workbenchSource.includes('kbase-workbench-header'), 'KBaseWorkbench.vue should not render a duplicate secondary navigation row')
assert.ok(!workbenchSource.includes('app-subnavigation'), 'KBaseWorkbench.vue should not render duplicate section tabs above the workbench')
assert.ok(!workbenchSource.includes('navigationItems'), 'KBaseWorkbench.vue should rely on the workbench panels instead of a duplicate navigation list')

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
assert.ok(!workbenchSource.includes('name="baseUrl"'), 'KBaseWorkbench.vue should not render inline Base URL settings')
assert.ok(!workbenchSource.includes('name="token"'), 'KBaseWorkbench.vue should not render inline Token settings')

assert.ok(apiSource.includes('/browser/session-token'), 'api.ts should request the browser session token endpoint')
assert.ok(apiSource.includes('BookKnowledgeBooksPage'), 'api.ts should type paginated book results')
assert.ok(apiSource.includes('BookKnowledgePrompt'), 'api.ts should type prompt templates')
assert.ok(apiSource.includes('BookKnowledgeChatResponse'), 'api.ts should type chat responses')
assert.ok(apiSource.includes('BookKnowledgeChatHistoryItem'), 'api.ts should type chat history')
assert.ok(apiSource.includes('BookKnowledgeJob'), 'api.ts should type online jobs')
assert.ok(apiSource.includes('ebook_id'), 'api.ts should type Dedao ebook job ids')
assert.ok(apiSource.includes('ebook_enid'), 'api.ts should type Dedao ebook job enids')
assert.ok(apiSource.includes('download_type'), 'api.ts should type Dedao ebook download formats')
assert.ok(apiSource.includes('DedaoSession'), 'api.ts should type Dedao account session')
assert.ok(apiSource.includes('DedaoLoginQRCode'), 'api.ts should type Dedao login QR payload')
assert.ok(apiSource.includes('DedaoLoginCheck'), 'api.ts should type Dedao login polling payload')
assert.ok(apiSource.includes('/api/jobs'), 'api.ts should call jobs endpoints')
assert.ok(apiSource.includes('/api/dedao/session'), 'api.ts should call the Dedao session endpoint')
assert.ok(apiSource.includes('/api/dedao/auth/qrcode'), 'api.ts should call the Dedao QR login endpoint')
assert.ok(apiSource.includes('/api/dedao/auth/check'), 'api.ts should call the Dedao login polling endpoint')
assert.ok(apiSource.includes("credentials: 'same-origin'"), 'api.ts should include browser credentials for the session token endpoint')
assert.ok(/Authorization['"]?\s*:\s*`Bearer \$\{this\.token\}`/.test(apiSource), 'api.ts should attach Bearer token')
assert.ok(apiSource.includes('HTTP ${response.status}'), 'api.ts should include status in failed request errors')
assert.ok(apiSource.includes('await response.text()'), 'api.ts should include response body in failed request errors')
assert.ok(apiSource.includes('encodeURIComponent'), 'api.ts should encode query parameters')
assert.ok(apiSource.includes("method: 'POST'"), 'api.ts should POST chat requests')
assert.ok(apiSource.includes('getDedaoSession'), 'api.ts should expose a Dedao session client method')
assert.ok(apiSource.includes('createDedaoLoginQRCode'), 'api.ts should expose a Dedao QR login client method')
assert.ok(apiSource.includes('checkDedaoLogin'), 'api.ts should expose a Dedao login polling client method')
assert.ok(apiSource.includes('DedaoEbookPage'), 'api.ts should type paginated Dedao ebook results')
assert.ok(apiSource.includes('listDedaoEbooks'), 'api.ts should expose a Dedao ebook list client method')
assert.ok(apiSource.includes('/api/dedao/ebooks'), 'api.ts should call the Dedao ebook list endpoint')
assert.ok(apiSource.includes('DedaoCoursePage'), 'api.ts should type paginated Dedao course results')
assert.ok(apiSource.includes('listDedaoCourses'), 'api.ts should expose a Dedao course list client method')
assert.ok(apiSource.includes('/api/dedao/courses'), 'api.ts should call the Dedao course list endpoint')
assert.ok(routerSource.includes('AccountLogin'), 'router.ts should route login to AccountLogin')
assert.ok(routerSource.includes('AccountProfile'), 'router.ts should route personal center to AccountProfile')
assert.ok(routerSource.includes('CourseLibrary'), 'router.ts should route courses to CourseLibrary')
assert.ok(/path:\s*['"]\/course['"][\s\S]{0,120}component:\s*CourseLibrary/.test(routerSource), 'router.ts should render CourseLibrary for /course')
assert.ok(routerSource.includes('CourseDetailReader'), 'router.ts should route course detail reading')
assert.ok(/path:\s*['"]\/course\/:enid['"][\s\S]{0,140}component:\s*CourseDetailReader/.test(routerSource), 'router.ts should render CourseDetailReader for /course/:enid')
assert.ok(routerSource.includes('EbookLibrary'), 'router.ts should route the ebook shelf to EbookLibrary')
assert.ok(/path:\s*['"]\/ebook['"][\s\S]{0,120}component:\s*EbookLibrary/.test(routerSource), 'router.ts should render EbookLibrary for /ebook')
assert.ok(routerSource.includes('EbookDetailReader'), 'router.ts should route ebook detail reading')
assert.ok(/path:\s*['"]\/ebook\/:enid['"][\s\S]{0,140}component:\s*EbookDetailReader/.test(routerSource), 'router.ts should render EbookDetailReader for /ebook/:enid')
assert.ok(routerSource.includes('WebSettings'), 'router.ts should route settings to WebSettings')
assert.ok(/path:\s*['"]\/setting['"][\s\S]{0,140}component:\s*WebSettings/.test(routerSource), 'router.ts should render WebSettings for /setting')
assert.ok(courseLibrarySource.includes('course-library'), 'CourseLibrary.vue should expose the course library surface')
assert.ok(courseLibrarySource.includes('listDedaoCourses'), 'CourseLibrary.vue should load courses through the API client')
assert.ok(courseLibrarySource.includes('router.push'), 'CourseLibrary.vue should navigate to course detail on row click')
assert.ok(courseLibrarySource.includes('/course/'), 'CourseLibrary.vue should build course detail URLs')
assert.ok(courseLibrarySource.includes('book-pagination'), 'CourseLibrary.vue should expose pagination controls')
assert.ok(courseLibrarySource.includes('empty-state'), 'CourseLibrary.vue should render actionable empty/error states')
assert.ok(!courseLibrarySource.includes('courseBaseUrl'), 'CourseLibrary.vue should not render inline Base URL settings')
assert.ok(!courseLibrarySource.includes('courseToken'), 'CourseLibrary.vue should not render inline Token settings')
assert.ok(ebookLibrarySource.includes('ebook-library'), 'EbookLibrary.vue should expose the ebook library surface')
assert.ok(ebookLibrarySource.includes('listDedaoEbooks'), 'EbookLibrary.vue should load ebooks through the API client')
assert.ok(ebookLibrarySource.includes('router.push'), 'EbookLibrary.vue should navigate to ebook detail on row click')
assert.ok(ebookLibrarySource.includes('/ebook/'), 'EbookLibrary.vue should build ebook detail URLs')
assert.ok(ebookLibrarySource.includes('book-pagination'), 'EbookLibrary.vue should expose pagination controls')
assert.ok(ebookLibrarySource.includes('empty-state'), 'EbookLibrary.vue should render actionable empty/error states')
assert.ok(!ebookLibrarySource.includes('ebookBaseUrl'), 'EbookLibrary.vue should not render inline Base URL settings')
assert.ok(!ebookLibrarySource.includes('ebookToken'), 'EbookLibrary.vue should not render inline Token settings')
assert.ok(ebookLibrarySource.includes('ebook-action-bar'), 'EbookLibrary.vue should expose row action controls')
assert.ok(ebookLibrarySource.includes('dedao_ebook_download'), 'EbookLibrary.vue should create ebook download jobs')
assert.ok(ebookLibrarySource.includes('dedao_ebook_sync_kbase'), 'EbookLibrary.vue should create ebook KBase sync jobs')
assert.ok(ebookLibrarySource.includes('downloadTypes'), 'EbookLibrary.vue should expose a download format selector')
assert.ok(ebookLibrarySource.includes('ebook-job-status'), 'EbookLibrary.vue should show selected ebook job status')
assert.ok(apiSource.includes('DedaoCourseDetail'), 'api.ts should type Dedao course detail')
assert.ok(apiSource.includes('DedaoArticleMarkdown'), 'api.ts should type Dedao course article markdown')
assert.ok(apiSource.includes('DedaoEbookDetail'), 'api.ts should type Dedao ebook detail')
assert.ok(apiSource.includes('DedaoEbookChapterPages'), 'api.ts should type Dedao ebook pages')
assert.ok(apiSource.includes('getDedaoCourseDetail'), 'api.ts should expose a Dedao course detail client method')
assert.ok(apiSource.includes('listDedaoCourseArticles'), 'api.ts should expose a Dedao course article list client method')
assert.ok(apiSource.includes('getDedaoArticleMarkdown'), 'api.ts should expose a Dedao article markdown client method')
assert.ok(apiSource.includes('getDedaoEbookDetail'), 'api.ts should expose a Dedao ebook detail client method')
assert.ok(apiSource.includes('getDedaoEbookChapterPages'), 'api.ts should expose a Dedao ebook chapter pages client method')
assert.ok(courseDetailReaderSource.includes('course-detail-reader'), 'CourseDetailReader.vue should expose the course detail reader surface')
assert.ok(courseDetailReaderSource.includes('getDedaoCourseDetail'), 'CourseDetailReader.vue should load course detail')
assert.ok(courseDetailReaderSource.includes('listDedaoCourseArticles'), 'CourseDetailReader.vue should page course articles')
assert.ok(courseDetailReaderSource.includes('getDedaoArticleMarkdown'), 'CourseDetailReader.vue should load article markdown')
assert.ok(courseDetailReaderSource.includes('renderMarkdown'), 'CourseDetailReader.vue should render Markdown article content')
assert.ok(courseDetailReaderSource.includes('answer-markdown'), 'CourseDetailReader.vue should reuse Markdown answer styling')
assert.ok(!courseDetailReaderSource.includes('courseDetailBaseUrl'), 'CourseDetailReader.vue should not render inline Base URL settings')
assert.ok(!courseDetailReaderSource.includes('courseDetailToken'), 'CourseDetailReader.vue should not render inline Token settings')
assert.ok(ebookDetailReaderSource.includes('ebook-detail-reader'), 'EbookDetailReader.vue should expose the ebook detail reader surface')
assert.ok(ebookDetailReaderSource.includes('getDedaoEbookDetail'), 'EbookDetailReader.vue should load ebook detail')
assert.ok(ebookDetailReaderSource.includes('getDedaoEbookChapterPages'), 'EbookDetailReader.vue should load chapter pages')
assert.ok(ebookDetailReaderSource.includes('sandbox'), 'EbookDetailReader.vue should sandbox SVG page rendering')
assert.ok(ebookDetailReaderSource.includes('ebook-page-frame'), 'EbookDetailReader.vue should render SVG pages in frames')
assert.ok(!ebookDetailReaderSource.includes('ebookDetailBaseUrl'), 'EbookDetailReader.vue should not render inline Base URL settings')
assert.ok(!ebookDetailReaderSource.includes('ebookDetailToken'), 'EbookDetailReader.vue should not render inline Token settings')
assert.ok(accountProfileSource.includes('account-profile'), 'AccountProfile.vue should expose the account profile surface')
assert.ok(accountProfileSource.includes('getDedaoSession'), 'AccountProfile.vue should load server-side Dedao session')
assert.ok(accountProfileSource.includes('user_count'), 'AccountProfile.vue should render configured user count')
assert.ok(!accountProfileSource.includes('accountBaseUrl'), 'AccountProfile.vue should not render inline Base URL settings')
assert.ok(!accountProfileSource.includes('accountToken'), 'AccountProfile.vue should not render inline Token settings')
assert.ok(accountLoginSource.includes('account-login'), 'AccountLogin.vue should expose the account login surface')
assert.ok(accountLoginSource.includes('createDedaoLoginQRCode'), 'AccountLogin.vue should request QR code login')
assert.ok(accountLoginSource.includes('checkDedaoLogin'), 'AccountLogin.vue should poll login status')
assert.ok(accountLoginSource.includes('qr_code_string'), 'AccountLogin.vue should track the QR polling string')
assert.ok(!accountLoginSource.includes('loginBaseUrl'), 'AccountLogin.vue should not render inline Base URL settings')
assert.ok(!accountLoginSource.includes('loginToken'), 'AccountLogin.vue should not render inline Token settings')
assert.ok(webSettingsSource.includes('web-settings'), 'WebSettings.vue should expose the settings surface')
assert.ok(webSettingsSource.includes('settingsBaseUrl'), 'WebSettings.vue should own the Base URL field')
assert.ok(webSettingsSource.includes('settingsToken'), 'WebSettings.vue should own the Token field')
assert.ok(webSettingsSource.includes('dedao-kbase-web-settings'), 'WebSettings.vue should persist the shared Web settings key')

console.log('web kbase UI smoke passed')
