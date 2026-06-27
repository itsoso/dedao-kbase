import assert from 'node:assert/strict'
import { renderMarkdown } from '../src/utils/markdownRender.js'

const rendered = renderMarkdown('### 核心结论\n\n**重点**\n\n- 第一条')
assert.match(rendered, /<h3>核心结论<\/h3>/)
assert.match(rendered, /<strong>重点<\/strong>/)
assert.match(rendered, /<li>第一条<\/li>/)

const escaped = renderMarkdown('<script>alert(1)</script>\n\n**safe**')
assert.doesNotMatch(escaped, /<script>/)
assert.match(escaped, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/)
assert.match(escaped, /<strong>safe<\/strong>/)

console.log('markdown render smoke passed')
