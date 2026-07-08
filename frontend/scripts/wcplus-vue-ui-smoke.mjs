import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const routeSource = readFileSync(join(here, '../src/router/index.ts'), 'utf8')
const viewSource = readFileSync(join(here, '../src/views/WCPlusSource.vue'), 'utf8')

assert.ok(routeSource.includes("path: 'wcplus-source'"), 'router should expose /wcplus-source')
assert.ok(routeSource.includes('../views/WCPlusSource.vue'), 'router should load WCPlusSource.vue')

for (const hook of [
  'wcplus-workbench',
  'wcplus-sidebar',
  'wcplus-main',
  'wcplus-preview',
  'wcplus-task-panel',
  'wcplus-raw-import',
  'wcplus-batch-import',
  'wcplus-batch-import-to-kbase',
  'wcplus-env-check',
  'wcplus-diagnostics',
  'wcplus-utility-panel',
  'wcplus-utility-result',
  'wcplus-raw-file',
  'wcplus-batch-result',
  'wcplus-action-bar',
]) {
  assert.ok(viewSource.includes(hook), `WCPlusSource.vue should include ${hook}`)
}

for (const endpoint of [
  '/api/wcplus/status',
  '/api/wcplus/env/check',
  '/api/wcplus/gzh/list',
  '/api/wcplus/gzh/articles',
  '/api/wcplus/article/content',
  '/api/wcplus/import/article',
  '/api/wcplus/import/raw',
  '/api/wcplus/import/account',
  '/api/wcplus/search',
  '/api/wcplus/article/search-title',
  '/api/wcplus/search-gzh',
  '/api/wcplus/article/all',
  '/api/wcplus/report/reading-data',
  '/api/wcplus/report/statistic-data',
  '/api/wcplus/article/gzh',
  '/api/wcplus/like-articles',
  '/api/wcplus/request/gzh',
  '/api/wcplus/task/all',
  '/api/wcplus/task/new',
  '/api/wcplus/task/control',
  '/api/wcplus/batch-task/create',
  '/api/wcplus/batch-task/delete',
  '/api/wcplus/batch-import/gzh',
  '/api/wcplus/export/text',
  '/api/wcplus/export/gzh-csv',
  '/api/wcplus/export/all-articles-xlsx',
]) {
  assert.ok(viewSource.includes(endpoint), `WCPlusSource.vue should call ${endpoint}`)
}

assert.ok(viewSource.includes('Authorization'), 'WCPlusSource.vue should set Authorization header')
assert.ok(viewSource.includes('Bearer'), 'WCPlusSource.vue should use Bearer token auth')
assert.ok(viewSource.includes('KBASE_AUTH_TOKEN'), 'WCPlusSource.vue should reuse the existing kbase token key')
assert.ok(viewSource.includes('isSafeBearerToken'), 'WCPlusSource.vue should validate token before setting Authorization')
assert.ok(viewSource.includes('clearStoredToken'), 'WCPlusSource.vue should clear invalid local token values')
assert.ok(viewSource.includes('/^[\\x21-\\x7e]+$/'), 'WCPlusSource.vue should reject non-ASCII bearer tokens')
assert.ok(viewSource.includes('KBase API Base URL'), 'WCPlusSource.vue should expose a configurable kbase API base for desktop')
assert.ok(viewSource.includes('kbase.apiBaseURL'), 'WCPlusSource.vue should persist the kbase API base URL')
assert.ok(viewSource.includes('resolveAPIURL'), 'WCPlusSource.vue should resolve API calls through a shared base URL helper')
assert.ok(viewSource.includes('fetch(resolveAPIURL(path)'), 'WCPlusSource.vue should apply the API base URL before fetch')
assert.ok(!viewSource.includes('fetch(path,'), 'WCPlusSource.vue should not fetch raw relative API paths directly')
assert.ok(viewSource.includes('bootstrapWCPlusSource'), 'WCPlusSource.vue should bootstrap WC Plus diagnostics on mount')
assert.ok(viewSource.includes('Promise.allSettled'), 'WCPlusSource.vue should load startup diagnostics without blocking the page')
assert.ok(viewSource.includes('启动时自动检查环境'), 'WCPlusSource.vue should explain startup diagnostics')
assert.ok(viewSource.includes('searchOffset'), 'WCPlusSource.vue should track WC Plus search pagination offset')
assert.ok(viewSource.includes('searchNum'), 'WCPlusSource.vue should track WC Plus search pagination page size')
assert.ok(viewSource.includes('pageSearch'), 'WCPlusSource.vue should expose WC Plus search result pagination')
assert.ok(viewSource.includes('搜索每页'), 'WCPlusSource.vue should render WC Plus search page-size control')
assert.ok(viewSource.includes('offset: searchOffset.value'), 'WCPlusSource.vue should send search offset')
assert.ok(viewSource.includes('num: searchNum.value'), 'WCPlusSource.vue should send search page size')
assert.ok(viewSource.includes('success_text'), 'WCPlusSource.vue should expose WC Plus batch import success text')
assert.ok(viewSource.includes('failed_text'), 'WCPlusSource.vue should expose WC Plus batch import failed text')
assert.ok(viewSource.includes('batchImportToKBase'), 'WCPlusSource.vue should support batch import to knowledge base')
assert.ok(viewSource.includes('batchWaitForCompletion'), 'WCPlusSource.vue should support waiting for WC Plus tasks before import')
assert.ok(viewSource.includes('batchImportLimit'), 'WCPlusSource.vue should send batch import limits')
assert.ok(viewSource.includes('import_to_kbase'), 'WCPlusSource.vue should send import_to_kbase')
assert.ok(viewSource.includes('wait_for_completion'), 'WCPlusSource.vue should send wait_for_completion')
assert.ok(viewSource.includes('poll_attempts'), 'WCPlusSource.vue should send bounded poll attempts')
assert.ok(viewSource.includes('imported_count'), 'WCPlusSource.vue should render imported knowledge count')
assert.ok(viewSource.includes('同步后导入书籍知识库'), 'WCPlusSource.vue should label knowledge import option')
assert.ok(viewSource.includes('等待任务完成后入库'), 'WCPlusSource.vue should label task wait option')
assert.ok(viewSource.includes('envCheck'), 'WCPlusSource.vue should render WC Plus environment check details')
assert.ok(viewSource.includes('utilityResult'), 'WCPlusSource.vue should render auxiliary WC Plus API results')
assert.ok(viewSource.includes('runUtility'), 'WCPlusSource.vue should call auxiliary WC Plus APIs')
assert.ok(viewSource.includes('辅助查询'), 'WCPlusSource.vue should expose auxiliary query controls')
assert.ok(viewSource.includes('阅读数据'), 'WCPlusSource.vue should expose reading report query')
assert.ok(viewSource.includes('统计数据'), 'WCPlusSource.vue should expose statistic report query')
assert.ok(viewSource.includes('公众号详情'), 'WCPlusSource.vue should expose article owner query')
assert.ok(viewSource.includes('收藏文章'), 'WCPlusSource.vue should expose liked articles query')
assert.ok(viewSource.includes('请求公众号'), 'WCPlusSource.vue should expose request gzh query')
assert.ok(viewSource.includes('base_url'), 'WCPlusSource.vue should show server-side WC Plus base_url diagnostics')
assert.ok(viewSource.includes('copyDiagnostics'), 'WCPlusSource.vue should support copying WC Plus diagnostics')
assert.ok(viewSource.includes('loadRawFile'), 'WCPlusSource.vue should support raw TXT/Markdown file import fallback')
assert.ok(viewSource.includes('FileReader'), 'WCPlusSource.vue should read local raw article files')
assert.ok(viewSource.includes("articleURL(item)"), 'WCPlusSource.vue should allow URL-only article results')
assert.ok(viewSource.includes("id ? {nickname, id} : {url}"), 'WCPlusSource.vue should preview URL-only articles')
assert.ok(viewSource.includes("id ? {nickname, id} : {url}"), 'WCPlusSource.vue should import URL-only articles')
assert.ok(viewSource.includes('appmsgid'), 'WCPlusSource.vue should normalize alternate article id fields')
assert.ok(viewSource.includes("'link'"), 'WCPlusSource.vue should normalize alternate article URL fields')
assert.ok(!viewSource.includes('WCPLUS_BASE_URL'), 'browser code must not embed local WC Plus base URL config')
assert.ok(!viewSource.includes('localhost:5324'), 'browser code must not call local WC Plus directly')

console.log('wcplus vue UI smoke passed')
