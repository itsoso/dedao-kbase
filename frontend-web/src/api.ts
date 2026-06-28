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

export interface BookKnowledgeJob {
  id: string
  type: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  book_id?: string
  target?: string
  ebook_id?: number
  ebook_enid?: string
  odob_id?: number
  odob_enid?: string
  odob_title?: string
  odob_alias_id?: string
  odob_can_play?: boolean
  download_type?: number
  result?: Record<string, unknown>
  error?: string
  logs?: string[]
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
}

export interface BookKnowledgeJobRequest {
  type: string
  book_id?: string
  target?: string
  ebook_id?: number
  ebook_enid?: string
  odob_id?: number
  odob_enid?: string
  odob_title?: string
  odob_alias_id?: string
  odob_can_play?: boolean
  download_type?: number
}

export interface BrowserSession {
  token?: string
}

export interface DedaoSessionUser {
  uid_hazy?: string
  name?: string
  avatar?: string
}

export interface DedaoSession {
  logged_in: boolean
  active_user?: DedaoSessionUser
  user_count: number
}

export interface DedaoLoginQRCode {
  token: string
  qr_code: string
  qr_code_string: string
}

export interface DedaoLoginCheck {
  status: number
  expired?: boolean
  user?: DedaoSessionUser
  session: DedaoSession
}

export interface DedaoEbook {
  enid: string
  id: number
  title: string
  author?: string
  intro?: string
  icon?: string
  price?: string
  progress: number
  publish_num?: number
  last_read?: string
}

export interface DedaoEbookPage {
  ebooks: DedaoEbook[]
  page: number
  page_size: number
  total: number
  total_pages: number
  is_more: number
}

export interface DedaoEbookCatalogItem {
  level: number
  text: string
  href?: string
  chapter_id?: string
  play_order?: number
}

export interface DedaoEbookDetail {
  enid: string
  id: number
  title: string
  operating_title?: string
  cover?: string
  count?: number
  price?: string
  author_info?: string
  book_author?: string
  publish_time?: string
  book_intro?: string
  author_list?: string[]
  press_name?: string
  press_brief?: string
  classify_name?: string
  product_score?: string
  douban_score?: string
  read_time?: number
  is_buy: boolean
  is_on_bookshelf: boolean
  can_trial_read: boolean
  catalog: DedaoEbookCatalogItem[]
}

export interface DedaoEbookPageSVG {
  page_num: number
  begin_offset: number
  end_offset: number
  is_first: boolean
  is_last: boolean
  svg: string
}

export interface DedaoEbookChapterPages {
  enid: string
  chapter_id: string
  index: number
  count: number
  offset: number
  is_end: boolean
  pages: DedaoEbookPageSVG[]
}

export interface DedaoCourse {
  enid: string
  id: number
  class_id: number
  title: string
  intro?: string
  author?: string
  icon?: string
  price?: string
  progress: number
  publish_num?: number
  course_num?: number
  last_read?: string
}

export interface DedaoCoursePage {
  courses: DedaoCourse[]
  page: number
  page_size: number
  total: number
  total_pages: number
  is_more: number
}

export interface DedaoOdob {
  enid: string
  id: number
  class_id?: number
  title: string
  intro?: string
  author?: string
  icon?: string
  price?: string
  progress: number
  duration?: number
  publish_num?: number
  last_read?: string
  audio_alias_id?: string
  audio_title?: string
  audio_icon?: string
  audio_duration?: number
  audio_play_url?: string
  has_play_auth: boolean
}

export interface DedaoOdobPage {
  odobs: DedaoOdob[]
  page: number
  page_size: number
  total: number
  total_pages: number
  is_more: number
}

export interface DedaoOdobAgency {
  name?: string
  intro?: string
  member_name?: string
  member_avatar?: string
  book_count?: number
  user_visit_count?: number
}

export interface DedaoOdobTopicSummary {
  title: string
  sub_title?: string
}

export interface DedaoOdobDetail {
  enid: string
  id: number
  title: string
  icon?: string
  duration?: number
  audio_price?: string
  audio_summary?: string
  publish_time?: number
  is_vip: boolean
  is_buy: boolean
  in_bookrack: boolean
  progress?: number
  tags?: string[]
  learn_count_desc?: string
  agency?: DedaoOdobAgency
  topic_summary?: DedaoOdobTopicSummary[]
}

export interface DedaoCourseDetailMeta {
  enid: string
  id: number
  id_str?: string
  title: string
  intro?: string
  highlight?: string
  lecturer_name?: string
  lecturer_title?: string
  lecturer_intro?: string
  lecturer_avatar?: string
  logo?: string
  index_img?: string
  article_count?: number
  learn_user_count?: number
  price_desc?: string
  is_subscribe: boolean
}

export interface DedaoArticle {
  enid: string
  id: number
  id_str?: string
  title: string
  summary?: string
  logo?: string
  publish_time?: number
  is_read: boolean
  is_free_try: boolean
  order_num?: number
  has_audio: boolean
  has_video: boolean
}

export interface DedaoArticlePage {
  articles: DedaoArticle[]
  count: number
  max_id: number
  is_more: boolean
}

export interface DedaoCourseDetail {
  course: DedaoCourseDetailMeta
  articles: DedaoArticle[]
  has_more: boolean
}

export interface DedaoArticleMarkdown {
  enid: string
  type: string
  title?: string
  markdown: string
}

export interface PageAnalysisSection {
  title: string
  content: string
}

export interface PageAnalysisContextStats {
  sections: number
  chars: number
}

export interface PageAnalysisRequest {
  source: string
  title: string
  url?: string
  mode?: string
  question: string
  model?: string
  max_context_chars?: number
  context_sections: PageAnalysisSection[]
}

export interface PageAnalysisResponse {
  answer: string
  model: string
  mode: string
  source: string
  context_stats: PageAnalysisContextStats
  created_at: string
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

  async listJobs(limit = 50): Promise<BookKnowledgeJob[]> {
    const response = await this.request<{ jobs: BookKnowledgeJob[] }>(
      `/api/jobs?limit=${encodeURIComponent(String(limit))}`,
    )
    return response.jobs || []
  }

  async createJob(body: BookKnowledgeJobRequest): Promise<BookKnowledgeJob> {
    const response = await this.request<{ job: BookKnowledgeJob }>('/api/jobs', {
      method: 'POST',
      body: JSON.stringify(body),
    })
    return response.job
  }

  async getJob(jobID: string): Promise<BookKnowledgeJob> {
    const response = await this.request<{ job: BookKnowledgeJob }>(`/api/jobs/${encodeURIComponent(jobID)}`)
    return response.job
  }

  async getDedaoSession(): Promise<DedaoSession> {
    return this.request<DedaoSession>('/api/dedao/session')
  }

  async createDedaoLoginQRCode(): Promise<DedaoLoginQRCode> {
    return this.request<DedaoLoginQRCode>('/api/dedao/auth/qrcode', {
      method: 'POST',
    })
  }

  async checkDedaoLogin(token: string, qrCodeString: string): Promise<DedaoLoginCheck> {
    return this.request<DedaoLoginCheck>('/api/dedao/auth/check', {
      method: 'POST',
      body: JSON.stringify({
        token,
        qr_code_string: qrCodeString,
      }),
    })
  }

  async listDedaoEbooks(page = 1, pageSize = 15, query = ''): Promise<DedaoEbookPage> {
    const params = [
      `page=${encodeURIComponent(String(page))}`,
      `page_size=${encodeURIComponent(String(pageSize))}`,
    ]
    if (query.trim()) {
      params.push(`q=${encodeURIComponent(query.trim())}`)
    }
    return this.request<DedaoEbookPage>(`/api/dedao/ebooks?${params.join('&')}`)
  }

  async getDedaoEbookDetail(enid: string): Promise<DedaoEbookDetail> {
    return this.request<DedaoEbookDetail>(`/api/dedao/ebooks/${encodeURIComponent(enid)}`)
  }

  async getDedaoEbookChapterPages(
    enid: string,
    chapterID: string,
    index = 0,
    count = 8,
    offset = 0,
  ): Promise<DedaoEbookChapterPages> {
    const params = [
      `index=${encodeURIComponent(String(index))}`,
      `count=${encodeURIComponent(String(count))}`,
      `offset=${encodeURIComponent(String(offset))}`,
    ]
    return this.request<DedaoEbookChapterPages>(
      `/api/dedao/ebooks/${encodeURIComponent(enid)}/chapters/${encodeURIComponent(chapterID)}/pages?${params.join('&')}`,
    )
  }

  async listDedaoCourses(page = 1, pageSize = 15, query = ''): Promise<DedaoCoursePage> {
    const params = [
      `page=${encodeURIComponent(String(page))}`,
      `page_size=${encodeURIComponent(String(pageSize))}`,
    ]
    if (query.trim()) {
      params.push(`q=${encodeURIComponent(query.trim())}`)
    }
    return this.request<DedaoCoursePage>(`/api/dedao/courses?${params.join('&')}`)
  }

  async listDedaoOdobs(page = 1, pageSize = 15, query = ''): Promise<DedaoOdobPage> {
    const params = [
      `page=${encodeURIComponent(String(page))}`,
      `page_size=${encodeURIComponent(String(pageSize))}`,
    ]
    if (query.trim()) {
      params.push(`q=${encodeURIComponent(query.trim())}`)
    }
    return this.request<DedaoOdobPage>(`/api/dedao/odobs?${params.join('&')}`)
  }

  async getDedaoOdobDetail(enid: string): Promise<DedaoOdobDetail> {
    return this.request<DedaoOdobDetail>(`/api/dedao/odobs/${encodeURIComponent(enid)}`)
  }

  async getDedaoCourseDetail(enid: string): Promise<DedaoCourseDetail> {
    return this.request<DedaoCourseDetail>(`/api/dedao/courses/${encodeURIComponent(enid)}`)
  }

  async listDedaoCourseArticles(enid: string, count = 30, maxID = 0): Promise<DedaoArticlePage> {
    const params = [
      `count=${encodeURIComponent(String(count))}`,
      `max_id=${encodeURIComponent(String(maxID))}`,
    ]
    return this.request<DedaoArticlePage>(`/api/dedao/courses/${encodeURIComponent(enid)}/articles?${params.join('&')}`)
  }

  async getDedaoArticleMarkdown(enid: string): Promise<DedaoArticleMarkdown> {
    return this.request<DedaoArticleMarkdown>(`/api/dedao/articles/${encodeURIComponent(enid)}?type=course`)
  }

  async getDedaoOdobArticleMarkdown(enid: string): Promise<DedaoArticleMarkdown> {
    return this.request<DedaoArticleMarkdown>(`/api/dedao/articles/${encodeURIComponent(enid)}?type=odob`)
  }

  async analyzePage(body: PageAnalysisRequest): Promise<PageAnalysisResponse> {
    return this.request<PageAnalysisResponse>('/api/analyze-page', {
      method: 'POST',
      body: JSON.stringify(body),
    })
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
