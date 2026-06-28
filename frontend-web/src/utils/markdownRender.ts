import { marked } from 'marked'

marked.setOptions({
  mangle: false,
  headerIds: false,
  breaks: true,
})

const htmlReplacer: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

export function renderMarkdown(markdown: string): string {
  const escaped = String(markdown || '').replace(/[&<>"']/g, (char) => htmlReplacer[char])
  return marked(escaped)
}
