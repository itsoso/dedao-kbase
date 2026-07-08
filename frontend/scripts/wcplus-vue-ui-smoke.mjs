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
  'wcplus-env-check',
  'wcplus-diagnostics',
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
assert.ok(viewSource.includes('bootstrapWCPlusSource'), 'WCPlusSource.vue should bootstrap WC Plus diagnostics on mount')
assert.ok(viewSource.includes('Promise.allSettled'), 'WCPlusSource.vue should load startup diagnostics without blocking the page')
assert.ok(viewSource.includes('启动时自动检查环境'), 'WCPlusSource.vue should explain startup diagnostics')
assert.ok(viewSource.includes('success_text'), 'WCPlusSource.vue should expose WC Plus batch import success text')
assert.ok(viewSource.includes('failed_text'), 'WCPlusSource.vue should expose WC Plus batch import failed text')
assert.ok(viewSource.includes('envCheck'), 'WCPlusSource.vue should render WC Plus environment check details')
assert.ok(viewSource.includes('base_url'), 'WCPlusSource.vue should show server-side WC Plus base_url diagnostics')
assert.ok(viewSource.includes('copyDiagnostics'), 'WCPlusSource.vue should support copying WC Plus diagnostics')
assert.ok(viewSource.includes('loadRawFile'), 'WCPlusSource.vue should support raw TXT/Markdown file import fallback')
assert.ok(viewSource.includes('FileReader'), 'WCPlusSource.vue should read local raw article files')
assert.ok(!viewSource.includes('WCPLUS_BASE_URL'), 'browser code must not embed local WC Plus base URL config')
assert.ok(!viewSource.includes('localhost:5324'), 'browser code must not call local WC Plus directly')

console.log('wcplus vue UI smoke passed')
