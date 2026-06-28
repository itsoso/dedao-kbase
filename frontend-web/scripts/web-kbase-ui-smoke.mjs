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
  'search-panel',
  'detail-panel',
  'system-kb-panel',
]) {
  assert.ok(appSource.includes(hook), `App.vue should include ${hook}`)
}

for (const surface of [
  'baseUrl',
  'token',
  'listBooks',
  'getBook',
  'searchKnowledge',
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

assert.ok(apiSource.includes('class KBaseClient'), 'api.ts should define KBaseClient')
assert.ok(/Authorization['"]?\s*:\s*`Bearer \$\{this\.token\}`/.test(apiSource), 'api.ts should attach Bearer token')
assert.ok(apiSource.includes('HTTP ${response.status}'), 'api.ts should include status in failed request errors')
assert.ok(apiSource.includes('await response.text()'), 'api.ts should include response body in failed request errors')
assert.ok(apiSource.includes('encodeURIComponent'), 'api.ts should encode query parameters')

console.log('web kbase UI smoke passed')
