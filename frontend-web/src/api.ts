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

export class KBaseClient {
  private baseUrl: string
  private token: string

  constructor(baseUrl: string, token: string) {
    this.baseUrl = baseUrl.trim().replace(/\/+$/, '')
    this.token = token.trim()
  }

  async listBooks(): Promise<BookKnowledgeBook[]> {
    const response = await this.request<{ books: BookKnowledgeBook[] }>('/api/books')
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

  async getSystemKBManifest(): Promise<Record<string, unknown>> {
    return this.request<Record<string, unknown>>('/api/system-kb/manifest')
  }

  async getSystemKBExport(): Promise<Record<string, unknown>> {
    return this.request<Record<string, unknown>>('/api/system-kb/export')
  }

  private async request<T>(path: string): Promise<T> {
    const url = `${this.baseUrl || window.location.origin}${path}`
    const response = await fetch(url, {
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: 'application/json',
      },
    })
    if (!response.ok) {
      const body = await response.text()
      throw new Error(`HTTP ${response.status}: ${body || response.statusText}`)
    }
    return response.json() as Promise<T>
  }
}
