import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const appPath = join(here, '../src/App.vue')
const apiPath = join(here, '../src/api.ts')
const routerPath = join(here, '../src/router.ts')
const stylePath = join(here, '../src/style.css')
const homeDiscoveryPath = join(here, '../src/views/HomeDiscovery.vue')
const knowledgeCityPath = join(here, '../src/views/KnowledgeCity.vue')
const compassLibraryPath = join(here, '../src/views/CompassLibrary.vue')
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
assert.ok(existsSync(stylePath), 'style.css should define the Web GUI styles')
assert.ok(existsSync(homeDiscoveryPath), 'HomeDiscovery.vue should host the implemented Web home route')
assert.ok(existsSync(knowledgeCityPath), 'KnowledgeCity.vue should host the implemented Web knowledge city route')
assert.ok(existsSync(compassLibraryPath), 'CompassLibrary.vue should host the implemented Web compass route')
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
const styleSource = readFileSync(stylePath, 'utf8')
const homeDiscoverySource = readFileSync(homeDiscoveryPath, 'utf8')
const knowledgeCitySource = readFileSync(knowledgeCityPath, 'utf8')
const compassLibrarySource = readFileSync(compassLibraryPath, 'utf8')
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
assert.ok(appSource.includes('immersive-shell'), 'App.vue should support immersive reader routes')
assert.ok(appSource.includes('wide-shell'), 'App.vue should support wide workbench routes')
assert.ok(appSource.includes('!route.meta.immersive'), 'App.vue should hide shell navigation for immersive routes')
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
  'two-pane-layout',
  'book-rail',
  'book-pagination',
  'library-search-panel',
  'chat-panel',
  'context-drawer',
  'interop-panel',
  'jobs-panel',
  'ops-panel',
  'system-kb-panel',
  'model-select',
  'prompt-chip-grid',
  'column-resizer',
  'answer-markdown',
]) {
  assert.ok(workbenchSource.includes(hook), `KBaseWorkbench.vue should include ${hook}`)
}

assert.ok(!workbenchSource.includes('right-resizer'), 'KBaseWorkbench.vue should not keep a third details column')
assert.ok(!workbenchSource.includes('compact-reference-panel'), 'KBaseWorkbench.vue should not render a permanent right details panel')
assert.ok(!workbenchSource.includes('prompt-select'), 'KBaseWorkbench.vue should render prompt templates as chips, not a dropdown')
assert.ok(!workbenchSource.includes('mode-strip'), 'KBaseWorkbench.vue should not render the legacy simple prompt mode strip')
assert.ok(!styleSource.includes('mode-strip'), 'style.css should not keep legacy simple prompt mode strip styles')
assert.ok(!workbenchSource.includes('chatModes'), 'KBaseWorkbench.vue should not keep legacy simple prompt modes')
assert.ok(!workbenchSource.includes('setChatMode'), 'KBaseWorkbench.vue should not keep legacy prompt mode switching')
assert.ok(!workbenchSource.includes('Library Search'), 'KBaseWorkbench.vue should not show the library search label')
assert.ok(!workbenchSource.includes('找书与检索'), 'KBaseWorkbench.vue should not show the redundant library search title')
assert.ok(!workbenchSource.includes('Refresh'), 'KBaseWorkbench.vue should not show a Refresh button in the search panel')
assert.ok(!workbenchSource.includes('Current Book'), 'KBaseWorkbench.vue should not show a Current Book search scope dropdown')
assert.ok(!workbenchSource.includes('Updated'), 'KBaseWorkbench.vue should not show a sort dropdown in the search panel')
assert.ok(workbenchSource.includes('resetBookStudyState'), 'KBaseWorkbench.vue should clear stale chat and details when switching books')
assert.ok(workbenchSource.includes("selectedChatModel.value = 'qwen3.7-max'"), 'KBaseWorkbench.vue should reset the default model to Qwen-3.7-Max')
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
assert.ok(workbenchSource.includes('pendingChatRequests'), 'KBaseWorkbench.vue should track parallel chat requests')
assert.ok(workbenchSource.includes('const requestBookID = selectedBookID.value'), 'KBaseWorkbench.vue should bind each chat request to the selected book at send time')
assert.ok(workbenchSource.includes('selectedBookID.value === requestBookID'), 'KBaseWorkbench.vue should not show stale chat responses after switching books')
assert.ok(!workbenchSource.includes(':disabled="!selectedBookID || chatLoading"'), 'KBaseWorkbench.vue should not block new prompts while another chat request is running')
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
assert.ok(routerSource.includes('HomeDiscovery'), 'router.ts should route home to HomeDiscovery')
assert.ok(/path:\s*['"]\/home['"][\s\S]{0,120}component:\s*HomeDiscovery/.test(routerSource), 'router.ts should render HomeDiscovery for /home')
assert.ok(routerSource.includes('KnowledgeCity'), 'router.ts should route knowledge city to KnowledgeCity')
assert.ok(/path:\s*['"]\/knowledge['"][\s\S]{0,140}component:\s*KnowledgeCity/.test(routerSource), 'router.ts should render KnowledgeCity for /knowledge')
assert.ok(routerSource.includes('CompassLibrary'), 'router.ts should route compass to CompassLibrary')
assert.ok(/path:\s*['"]\/compass['"][\s\S]{0,140}component:\s*CompassLibrary/.test(routerSource), 'router.ts should render CompassLibrary for /compass')
assert.ok(routerSource.includes('CourseLibrary'), 'router.ts should route courses to CourseLibrary')
assert.ok(/path:\s*['"]\/course['"][\s\S]{0,120}component:\s*CourseLibrary/.test(routerSource), 'router.ts should render CourseLibrary for /course')
assert.ok(routerSource.includes('CourseDetailReader'), 'router.ts should route course detail reading')
assert.ok(/path:\s*['"]\/course\/:enid['"][\s\S]{0,140}component:\s*CourseDetailReader/.test(routerSource), 'router.ts should render CourseDetailReader for /course/:enid')
assert.ok(/path:\s*['"]\/course\/:enid['"][\s\S]{0,220}wide:\s*true/.test(routerSource), 'router.ts should make course detail reading a wide route')
assert.ok(routerSource.includes('EbookLibrary'), 'router.ts should route the ebook shelf to EbookLibrary')
assert.ok(/path:\s*['"]\/ebook['"][\s\S]{0,120}component:\s*EbookLibrary/.test(routerSource), 'router.ts should render EbookLibrary for /ebook')
assert.ok(routerSource.includes('EbookDetailReader'), 'router.ts should route ebook detail reading')
assert.ok(/path:\s*['"]\/ebook\/:enid['"][\s\S]{0,140}component:\s*EbookDetailReader/.test(routerSource), 'router.ts should render EbookDetailReader for /ebook/:enid')
assert.ok(/path:\s*['"]\/ebook\/:enid['"][\s\S]{0,220}immersive:\s*true/.test(routerSource), 'router.ts should make ebook reading immersive')
assert.ok(routerSource.includes('WebSettings'), 'router.ts should route settings to WebSettings')
assert.ok(/path:\s*['"]\/book-knowledge['"][\s\S]{0,220}wide:\s*true/.test(routerSource), 'router.ts should make /book-knowledge a wide workbench route')
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
assert.ok(homeDiscoverySource.includes('home-discovery'), 'HomeDiscovery.vue should expose the home page surface')
assert.ok(homeDiscoverySource.includes('getDedaoSession'), 'HomeDiscovery.vue should load Dedao account state')
assert.ok(homeDiscoverySource.includes('listDedaoCourses'), 'HomeDiscovery.vue should load course recommendations')
assert.ok(homeDiscoverySource.includes('listDedaoEbooks'), 'HomeDiscovery.vue should load ebook recommendations')
assert.ok(homeDiscoverySource.includes('listDedaoOdobs'), 'HomeDiscovery.vue should load odob recommendations')
assert.ok(homeDiscoverySource.includes('listJobs'), 'HomeDiscovery.vue should show recent KBase jobs')
assert.ok(homeDiscoverySource.includes('continueItems'), 'HomeDiscovery.vue should build a continue-learning feed')
assert.ok(homeDiscoverySource.includes('/book-knowledge'), 'HomeDiscovery.vue should link into the knowledge workbench')
assert.ok(knowledgeCitySource.includes('knowledge-city'), 'KnowledgeCity.vue should expose the knowledge city surface')
assert.ok(knowledgeCitySource.includes('listDedaoTopics'), 'KnowledgeCity.vue should load topic recommendations')
assert.ok(knowledgeCitySource.includes('listDedaoTopicNotes'), 'KnowledgeCity.vue should load selected topic notes')
assert.ok(knowledgeCitySource.includes('topic-note-card'), 'KnowledgeCity.vue should render discussion notes')
assert.ok(compassLibrarySource.includes('compass-library'), 'CompassLibrary.vue should expose the compass library surface')
assert.ok(compassLibrarySource.includes('listDedaoCourses'), 'CompassLibrary.vue should load compass courses through the API client')
assert.ok(compassLibrarySource.includes("category: 'compass'"), 'CompassLibrary.vue should request the compass course category')
assert.ok(compassLibrarySource.includes('/course/'), 'CompassLibrary.vue should open compass items in the course reader')
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
assert.ok(courseDetailReaderSource.includes('course-two-pane-layout'), 'CourseDetailReader.vue should use a two-pane reading layout')
assert.ok(courseDetailReaderSource.includes('course-context-drawer'), 'CourseDetailReader.vue should render course context as an on-demand drawer')
assert.ok(courseDetailReaderSource.includes('course-reader-actions'), 'CourseDetailReader.vue should expose compact course reader actions')
assert.ok(courseDetailReaderSource.includes('activeContextPanel'), 'CourseDetailReader.vue should hide course context until requested')
assert.ok(!courseDetailReaderSource.includes('right-resizer'), 'CourseDetailReader.vue should not keep a third details column')
assert.ok(!courseDetailReaderSource.includes('--course-right-column'), 'CourseDetailReader.vue should not reserve a permanent right column')
assert.ok(!courseDetailReaderSource.includes('courseDetailBaseUrl'), 'CourseDetailReader.vue should not render inline Base URL settings')
assert.ok(!courseDetailReaderSource.includes('courseDetailToken'), 'CourseDetailReader.vue should not render inline Token settings')
assert.ok(ebookDetailReaderSource.includes('ebook-detail-reader'), 'EbookDetailReader.vue should expose the ebook detail reader surface')
assert.ok(ebookDetailReaderSource.includes('getDedaoEbookDetail'), 'EbookDetailReader.vue should load ebook detail')
assert.ok(ebookDetailReaderSource.includes('getDedaoEbookChapterPages'), 'EbookDetailReader.vue should load chapter pages')
assert.ok(ebookDetailReaderSource.includes('sandbox'), 'EbookDetailReader.vue should sandbox SVG page rendering')
assert.ok(ebookDetailReaderSource.includes('ebook-page-frame'), 'EbookDetailReader.vue should render SVG pages in frames')
assert.ok(ebookDetailReaderSource.includes('dedao-reader-toolbar'), 'EbookDetailReader.vue should expose a Dedao-style reader toolbar')
assert.ok(ebookDetailReaderSource.includes('columnMode'), 'EbookDetailReader.vue should support one/two/three column reading')
assert.ok(ebookDetailReaderSource.includes('reader-floating-actions'), 'EbookDetailReader.vue should expose floating listen/AI actions')
assert.ok(ebookDetailReaderSource.includes('reader-bottom-bar'), 'EbookDetailReader.vue should expose fixed page navigation')
assert.ok(ebookDetailReaderSource.includes('readableCatalogItems'), 'EbookDetailReader.vue should advance across readable chapters')
assert.ok(ebookDetailReaderSource.includes('canGoNext'), 'EbookDetailReader.vue should not disable next at chapter end when another chapter exists')
assert.ok(ebookDetailReaderSource.includes('handleReaderWheel'), 'EbookDetailReader.vue should page through loaded frames with wheel input')
assert.ok(ebookDetailReaderSource.includes('--reader-page-height'), 'EbookDetailReader.vue should define an explicit viewport-based reader page height')
assert.ok(ebookDetailReaderSource.includes('grid-auto-rows: minmax(0, 1fr)'), 'EbookDetailReader.vue should stretch reader page rows to the available viewport height')
assert.ok(ebookDetailReaderSource.includes('height: 100%;') && ebookDetailReaderSource.includes('.ebook-page-shell'), 'EbookDetailReader.vue should stretch each page shell to the reader grid height')
assert.ok(ebookDetailReaderSource.includes('readerFullscreen'), 'EbookDetailReader.vue should keep an in-page fullscreen state')
assert.ok(ebookDetailReaderSource.includes('reader-fullscreen'), 'EbookDetailReader.vue should apply an in-page fullscreen class')
assert.ok(ebookDetailReaderSource.includes('fullscreenchange'), 'EbookDetailReader.vue should sync browser fullscreen changes')
assert.ok(ebookDetailReaderSource.includes('requestReaderFullscreen'), 'EbookDetailReader.vue should use a fullscreen helper with fallback')
assert.ok(ebookDetailReaderSource.includes('退出全屏'), 'EbookDetailReader.vue should expose an exit fullscreen label')
assert.ok(ebookDetailReaderSource.includes('normalizeEbookSvg'), 'EbookDetailReader.vue should normalize SVG pages before rendering')
assert.ok(ebookDetailReaderSource.includes('ebookSvgTextFallback'), 'EbookDetailReader.vue should render pathological text-only SVG pages as readable text')
assert.ok(ebookDetailReaderSource.includes('computeEbookSvgContentBox'), 'EbookDetailReader.vue should measure SVG content bounds before choosing a render mode')
assert.ok(ebookDetailReaderSource.includes('textDenseHugeCanvas'), 'EbookDetailReader.vue should fallback for text-heavy huge SVG chapters even when inline images exist')
assert.ok(!ebookDetailReaderSource.includes('complexVisualCount > 0'), 'EbookDetailReader.vue should not block text fallback only because SVG pages contain images')
assert.ok(ebookDetailReaderSource.includes('ebook-text-page'), 'EbookDetailReader.vue should include a readable text page fallback style')
assert.ok(ebookDetailReaderSource.includes('overflow: auto'), 'EbookDetailReader.vue should allow long text fallback pages to scroll inside the reader frame')
assert.ok(ebookDetailReaderSource.includes('estimateEbookTotalPageCount'), 'EbookDetailReader.vue should estimate page totals from page payloads')
assert.ok(!ebookDetailReaderSource.includes('const totalPageCount = computed(() => detail.value?.count || 0)'), 'EbookDetailReader.vue should not display ebook word count as total pages')
assert.ok(ebookDetailReaderSource.includes('preserveAspectRatio="xMidYMid meet"'), 'EbookDetailReader.vue should force readable SVG aspect handling')
assert.ok(ebookDetailReaderSource.includes('height: 100vh'), 'EbookDetailReader.vue should use a fixed full-viewport reader canvas')
assert.ok(ebookDetailReaderSource.includes('overflow: hidden'), 'EbookDetailReader.vue should avoid body-level reader scrollbars')
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
