import { marked } from 'marked'

marked.setOptions({
  mangle: false,
  headerIds: false,
  breaks: true,
})

const htmlReplacer = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

export function renderMarkdown(markdown) {
  const escaped = String(markdown || '').replace(/[&<>"']/g, (char) => htmlReplacer[char])
  return marked(escaped)
}
