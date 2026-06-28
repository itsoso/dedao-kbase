export interface BookKnowledgeBook {
  book_id: string
  title: string
  author?: string
  status?: string
  extractor?: string
  source_html?: string
  updated_at?: string
}

export interface BookKnowledgeChapter {
  chapter_id: string
  book_id: string
  order: number
  title: string
  summary?: string
  chunk_ids?: string[]
}

export interface BookKnowledgeChunk {
  chunk_id: string
  book_id: string
  chapter_id: string
  order: number
  text: string
  tokens?: number
}

export interface BookKnowledgeClaim {
  claim_id: string
  book_id: string
  chapter_id?: string
  title: string
  summary: string
  evidence_level?: string
  review_status?: string
  citations?: string[]
}

export interface BookKnowledgePackage {
  book: BookKnowledgeBook
  chapters: BookKnowledgeChapter[]
  chunks: BookKnowledgeChunk[]
  claims: BookKnowledgeClaim[]
  citations?: unknown[]
}

export interface BookKnowledgeBooksPage {
  books: BookKnowledgeBook[]
  page: number
  page_size: number
  total: number
  total_pages: number
}

export interface BookKnowledgeSearchResult {
  kind: string
  book_id: string
  book_title?: string
  chapter_id?: string
  chunk_id?: string
  claim_id?: string
  title?: string
  snippet: string
  score: number
}

export interface BookKnowledgePrompt {
  prompt_id: string
  category: string
  title: string
  description?: string
  prompt: string
  output_format?: string
  dynamic?: boolean
}

export interface BookKnowledgeChatSource {
  kind: string
  id: string
  title?: string
  chapter_id?: string
}

export interface BookKnowledgeChatContextStats {
  chapters: number
  claims: number
  chunks: number
  chars: number
}

export interface BookKnowledgeChatResponse {
  history_id?: string
  answer: string
  model: string
  mode: string
  sources: BookKnowledgeChatSource[]
  context_stats: BookKnowledgeChatContextStats
  created_at?: string
}

export interface BookKnowledgeChatHistoryItem {
  id: string
  book_id: string
  book_title: string
  mode: string
  question: string
  model: string
  answer: string
  sources: BookKnowledgeChatSource[]
  context_stats: BookKnowledgeChatContextStats
  created_at: string
}

export interface BookKnowledgeChatRequest {
  mode: string
  question: string
  model?: string
  max_context_chars?: number
}

export interface BrowserSession {
  token?: string
}

export const getBrowserSession = async (): Promise<BrowserSession | null> => {
  const response = await fetch('/browser/session-token', {
    credentials: 'same-origin',
    cache: 'no-store',
    headers: {
      Accept: 'application/json',
    },
  })
  if (response.status === 401 || response.status === 404) {
    return null
  }
  if (!response.ok) {
    const body = await response.text()
    throw new Error(`HTTP ${response.status}: ${body || response.statusText}`)
  }
  return response.json() as Promise<BrowserSession>
}

export class KBaseClient {
  private baseUrl: string
  private token: string

  constructor(baseUrl: string, token: string) {
    this.baseUrl = baseUrl.trim().replace(/\/+$/, '')
    this.token = token.trim()
  }

  async listBooksPage(page = 1, pageSize = 30, query = '', sort = 'updated_at_desc'): Promise<BookKnowledgeBooksPage> {
    const params = [
      `page=${encodeURIComponent(String(page))}`,
      `page_size=${encodeURIComponent(String(pageSize))}`,
      `sort=${encodeURIComponent(sort)}`,
    ]
    if (query.trim()) {
      params.push(`q=${encodeURIComponent(query.trim())}`)
    }
    return this.request<BookKnowledgeBooksPage>(`/api/books?${params.join('&')}`)
  }

  async listBooks(): Promise<BookKnowledgeBook[]> {
    const response = await this.listBooksPage()
    return response.books || []
  }

  async getBook(bookID: string): Promise<BookKnowledgePackage> {
    return this.request<BookKnowledgePackage>(`/api/books/${encodeURIComponent(bookID)}`)
  }

  async searchKnowledge(query: string, bookID: string, limit = 20): Promise<BookKnowledgeSearchResult[]> {
    const params = [`q=${encodeURIComponent(query)}`, `limit=${encodeURIComponent(String(limit))}`]
    if (bookID) {
      params.push(`book_id=${encodeURIComponent(bookID)}`)
    }
    const response = await this.request<{ results: BookKnowledgeSearchResult[] }>(`/api/search?${params.join('&')}`)
    return response.results || []
  }

  async getBookPrompts(bookID: string): Promise<BookKnowledgePrompt[]> {
    const response = await this.request<{ prompts: BookKnowledgePrompt[] }>(
      `/api/books/${encodeURIComponent(bookID)}/prompts`,
    )
    return response.prompts || []
  }

  async chatWithBook(bookID: string, body: BookKnowledgeChatRequest): Promise<BookKnowledgeChatResponse> {
    return this.request<BookKnowledgeChatResponse>(`/api/books/${encodeURIComponent(bookID)}/chat`, {
      method: 'POST',
      body: JSON.stringify(body),
    })
  }

  async getBookChatHistory(bookID: string, limit = 50): Promise<BookKnowledgeChatHistoryItem[]> {
    const response = await this.request<{ history: BookKnowledgeChatHistoryItem[] }>(
      `/api/books/${encodeURIComponent(bookID)}/chat-history?limit=${encodeURIComponent(String(limit))}`,
    )
    return response.history || []
  }

  async getSystemKBManifest(): Promise<Record<string, unknown>> {
    return this.request<Record<string, unknown>>('/api/system-kb/manifest')
  }

  async getSystemKBExport(): Promise<Record<string, unknown>> {
    return this.request<Record<string, unknown>>('/api/system-kb/export')
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const url = `${this.baseUrl || window.location.origin}${path}`
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.token}`,
      Accept: 'application/json',
    }
    if (init.body) {
      headers['Content-Type'] = 'application/json'
    }
    const response = await fetch(url, {
      ...init,
      headers: {
        ...headers,
      },
    })
    if (!response.ok) {
      const body = await response.text()
      throw new Error(`HTTP ${response.status}: ${body || response.statusText}`)
    }
    return response.json() as Promise<T>
  }
}
