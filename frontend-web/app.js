const app = document.querySelector("#app");

const tokenKeys = [
  "kbase.token",
  "kbaseToken",
  "KBASE_AUTH_TOKEN",
];

const wechatState = {
  articleURL: "",
  bookID: "",
  accountQuery: "",
  accounts: [],
  selectedAccount: null,
  accountArticles: [],
  articleBegin: 0,
  articleCount: 10,
  preview: null,
  imported: null,
  loading: "",
  message: "",
};

const wcplusState = {
  accounts: [],
  selectedAccount: null,
  articles: [],
  searchQuery: "",
  searchMode: "fulltext",
  searchResults: [],
  tasks: [],
  preview: null,
  serviceStatus: null,
  envCheck: null,
  utilityResult: null,
  batchResult: null,
  accountOffset: 0,
  accountNum: 20,
  articleOffset: 0,
  articleNum: 20,
  importLimit: 10,
  exportRecentNum: 100,
  taskCrawlerType: "gzh_article_link",
  taskArticleListType: "all",
  taskArticleListAmount: 20,
  batchNicknames: "",
  batchExactMatch: true,
  batchArticleListType: "all",
  batchArticleListAmount: 0,
  batchImportToKBase: false,
  batchWaitForCompletion: false,
  batchImportLimit: 10,
  rawTitle: "",
  rawNickname: "",
  rawURL: "",
  rawBookID: "",
  rawContent: "",
  rawImported: null,
  loading: "",
  message: "",
};

const knowledgeState = {
  books: [],
  selectedBook: null,
  package: null,
  query: "",
  results: [],
  loading: "",
  message: "",
};

let isWCPlusBootstrapped = false;

function getToken() {
  for (const key of tokenKeys) {
    const value = window.localStorage.getItem(key);
    const clean = String(value || "").trim();
    if (!clean) {
      continue;
    }
    if (isSafeBearerToken(clean)) {
      return clean;
    }
    console.warn("skip invalid kbase token", key);
    window.localStorage.removeItem(key);
  }
  return "";
}

function isSafeBearerToken(token) {
  const clean = String(token || "").trim();
  if (!clean || /\s/.test(clean)) {
    return false;
  }
  return /^[\x21-\x7e]+$/.test(clean);
}

function clearStoredToken() {
  for (const key of tokenKeys) {
    window.localStorage.removeItem(key);
  }
}

function storeToken(token) {
  const clean = String(token || "").trim();
  if (!isSafeBearerToken(clean)) {
    clearStoredToken();
    return "";
  }
  for (const key of tokenKeys) {
    window.localStorage.setItem(key, clean);
  }
  return clean;
}

function setAuthorizationHeader(headers, token) {
  const clean = String(token || "").trim();
  if (!isSafeBearerToken(clean)) {
    if (clean) {
      clearStoredToken();
      console.warn("skip invalid kbase token", "authorization");
    }
    return false;
  }
  headers.set("Authorization", `Bearer ${clean}`);
  return true;
}

async function refreshBrowserSessionToken() {
  const response = await fetch("/browser/session-token", {
    headers: {
      Accept: "application/json",
    },
    credentials: "same-origin",
    cache: "no-store",
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = null;
    }
  }
  if (!response.ok) {
    const message = payload && typeof payload === "object"
      ? (payload.error || payload.message || JSON.stringify(payload))
      : (text || `HTTP ${response.status}`);
    throw new Error(message);
  }
  return storeToken(payload?.token || "");
}

async function ensureBrowserSessionToken() {
  const existing = getToken();
  if (existing) {
    return existing;
  }
  try {
    return await refreshBrowserSessionToken();
  } catch (error) {
    console.warn("Unable to load kbase browser session token", error);
    return "";
  }
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function escapeAttribute(value) {
  return escapeHTML(value).replaceAll("\n", " ");
}

async function apiFetch(path, options = {}, didRefreshAuth = false) {
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const token = getToken() || await ensureBrowserSessionToken();
  if (token) {
    setAuthorizationHeader(headers, token);
  }

  const response = await fetch(path, {
    ...options,
    headers,
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = text;
    }
  }
  if (!response.ok) {
    if (response.status === 401 && !didRefreshAuth) {
      try {
        const refreshed = await refreshBrowserSessionToken();
        if (refreshed) {
          return apiFetch(path, options, true);
        }
      } catch (error) {
        console.warn("Unable to refresh kbase browser session token", error);
      }
    }
    const message = typeof payload === "object" && payload
      ? (payload.error || payload.message || JSON.stringify(payload))
      : (payload || `HTTP ${response.status}`);
    throw new Error(message);
  }
  return payload;
}

async function apiDownload(path, options = {}, filename = "download.bin", didRefreshAuth = false) {
  const headers = new Headers(options.headers || {});
  const token = getToken() || await ensureBrowserSessionToken();
  if (token) {
    setAuthorizationHeader(headers, token);
  }
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, {
    ...options,
    headers,
  });
  if (!response.ok) {
    if (response.status === 401 && !didRefreshAuth) {
      try {
        const refreshed = await refreshBrowserSessionToken();
        if (refreshed) {
          return apiDownload(path, options, filename, true);
        }
      } catch (error) {
        console.warn("Unable to refresh kbase browser session token", error);
      }
    }
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
  return blob.size;
}

const readerRouteSuffixes = [
  "overview",
  "chat",
  "prompts",
  "chapters",
  "claims",
  "chunks",
  "jobs",
  "system-kb",
  "skills",
  "ops",
];

function normalizeReaderBookID(bookID) {
  const value = String(bookID || "").trim();
  for (const suffix of readerRouteSuffixes) {
    const marker = `-${suffix}`;
    if (value.endsWith(marker)) {
      const base = value.slice(0, -marker.length);
      if (/^\d+$/.test(base)) {
        return base;
      }
    }
  }
  return value;
}

function getBookID() {
  const prefix = "/ebook/";
  if (!window.location.pathname.startsWith(prefix)) {
    return "";
  }
  const raw = window.location.pathname.slice(prefix.length).split("/")[0];
  try {
    return normalizeReaderBookID(decodeURIComponent(raw));
  } catch {
    return normalizeReaderBookID(raw);
  }
}

async function fetchBook(bookID) {
  return apiFetch(`/api/books/${encodeURIComponent(bookID)}`);
}

function renderShell(content, current = "") {
  app.className = "web-shell";
  app.innerHTML = `
    <header class="web-topbar">
      <a class="web-brand" href="/">dedao kbase</a>
      <nav class="web-nav" aria-label="主导航">
        <a class="${current === "home" ? "active" : ""}" href="/">首页</a>
        <a class="${current === "wechat" ? "active" : ""}" href="/wechat-source">微信来源</a>
        <a class="${current === "wcplus" ? "active" : ""}" href="/wcplus-source">WC Plus</a>
        <a class="${current === "knowledge" ? "active" : ""}" href="/book-knowledge">书籍知识库</a>
      </nav>
    </header>
    ${content}
  `;
}

function renderHome() {
  renderShell(`
    <main class="web-home">
      <section class="web-home__hero">
        <p class="web-kicker">Source Workbench</p>
        <h1>把外部内容整理成可验证知识库</h1>
        <p>从公众号文章开始，下载、预览并导入到书籍知识库，再交给 Health 和 Proofroom 使用。</p>
        <div class="web-home__actions">
          <a class="button button-primary" href="/wechat-source">导入微信公众号文章</a>
          <a class="button button-ghost" href="/wcplus-source">打开 WC Plus 工作台</a>
          <a class="button button-ghost" href="/book-knowledge">查看书籍知识库</a>
        </div>
      </section>
    </main>
  `, "home");
}

function renderBookKnowledge() {
  const bookRows = knowledgeState.books.map((book, index) => {
    const active = book.book_id === knowledgeState.selectedBook?.book_id ? " active" : "";
    return `
      <button class="knowledge-web__book${active}" type="button" data-book-index="${index}">
        <strong>${escapeHTML(book.title || book.book_id)}</strong>
        <span>${escapeHTML([book.status || "draft", book.extractor || ""].filter(Boolean).join(" · "))}</span>
      </button>
    `;
  }).join("");
  const pkg = knowledgeState.package || {};
  const currentBook = pkg.book || knowledgeState.selectedBook || {};
  const resultRows = knowledgeState.results.map((result) => `
    <article class="knowledge-web__result">
      <div class="web-kicker">${escapeHTML(result.kind || "result")}</div>
      <h3>${escapeHTML(result.title || result.id || "片段")}</h3>
      <p>${escapeHTML(result.snippet || "")}</p>
    </article>
  `).join("");
  const chapterRows = (pkg.chapters || []).slice(0, 16).map((chapter) => `
    <li>
      <span>${escapeHTML(chapter.title || chapter.chapter_id)}</span>
      <small>${escapeHTML(chapter.summary || "")}</small>
    </li>
  `).join("");
  const status = knowledgeState.loading
    ? `<div class="web-status">处理中：${escapeHTML(knowledgeState.loading)}</div>`
    : (knowledgeState.message ? `<div class="web-status">${escapeHTML(knowledgeState.message)}</div>` : "");

  renderShell(`
    <main class="knowledge-web">
      <section class="knowledge-web__header">
        <div>
          <p class="web-kicker">Book Knowledge</p>
          <h1>书籍知识库</h1>
        </div>
        <button id="knowledge-refresh" class="button button-ghost" type="button">刷新</button>
      </section>
      ${status}

      <div class="knowledge-web__layout">
        <aside class="knowledge-web__sidebar">
          <form id="knowledge-search-form" class="source-form">
            <label>
              <span>搜索</span>
              <input name="query" value="${escapeAttribute(knowledgeState.query)}" placeholder="搜索标题、claims 或 chunks">
            </label>
            <button class="button button-primary" type="submit">Search</button>
          </form>
          <div class="knowledge-web__books">
            ${bookRows || "<p class=\"web-muted\">暂无知识库条目，可先从微信来源导入。</p>"}
          </div>
        </aside>

        <section class="knowledge-web__main">
          ${currentBook.book_id ? `
            <div class="knowledge-web__title-row">
              <div>
                <p class="web-kicker">${escapeHTML(currentBook.book_id)}</p>
                <h2>${escapeHTML(currentBook.title || currentBook.book_id)}</h2>
              </div>
              <a class="button button-primary" href="/ebook/${encodeURIComponent(currentBook.book_id)}">阅读</a>
            </div>
            <div class="knowledge-web__stats">
              <span>${(pkg.chapters || []).length} 章</span>
              <span>${(pkg.claims || []).length} claims</span>
              <span>${(pkg.chunks || []).length} chunks</span>
            </div>
            <div class="knowledge-web__content">
              <section>
                <p class="web-kicker">Chapters</p>
                <ul>${chapterRows || "<li><span>暂无章节</span></li>"}</ul>
              </section>
              <section>
                <p class="web-kicker">Search Results</p>
                ${resultRows || "<p class=\"web-muted\">输入关键词后查看检索结果。</p>"}
              </section>
            </div>
          ` : "<p class=\"web-muted\">请选择书籍或导入新来源。</p>"}
        </section>
      </div>
    </main>
  `, "knowledge");
  bindBookKnowledgeEvents();
}

function renderReader(payload) {
  const book = payload.book || {};
  const chapters = Array.isArray(payload.chapters) ? payload.chapters : [];
  const claims = Array.isArray(payload.claims) ? payload.claims : [];
  const chunks = Array.isArray(payload.chunks) ? payload.chunks : [];
  const title = book.title || book.book_id || "未命名书籍";
  const meta = [
    book.author,
    book.extractor,
    book.updated_at,
  ].filter(Boolean).join(" / ");
  const chapterItems = chapters.slice(0, 12).map((chapter) => (
    `<li>${escapeHTML(chapter.title || chapter.chapter_id || "章节")}</li>`
  )).join("");
  const claimItems = claims.slice(0, 8).map((claim) => (
    `<li>${escapeHTML(claim.text || claim.claim || claim.summary || "")}</li>`
  )).join("");
  const chunkItems = chunks.slice(0, 4).map((chunk) => (
    `<p>${escapeHTML(chunk.text || chunk.content || "")}</p>`
  )).join("");

  app.className = "reader-shell";
  app.innerHTML = `
    <main class="reader-page">
      <article class="reader-page__article">
        <p class="reader-page__eyebrow">KBase Reader</p>
        <h1>${escapeHTML(title)}</h1>
        <div class="reader-page__meta">${escapeHTML(meta || book.book_id || "")}</div>
        <section class="reader-page__section">
          <h2>目录</h2>
          ${chapterItems ? `<ul>${chapterItems}</ul>` : "<p>暂无目录数据。</p>"}
        </section>
        <section class="reader-page__section">
          <h2>重点</h2>
          ${claimItems ? `<ul>${claimItems}</ul>` : "<p>暂无重点摘录。</p>"}
        </section>
        <section class="reader-page__section">
          <h2>正文摘录</h2>
          ${chunkItems || "<p>暂无正文摘录。</p>"}
        </section>
      </article>
    </main>
  `;
}

function renderError(message) {
  app.className = "reader-shell";
  app.innerHTML = `
    <main class="reader-error">
      <section class="reader-error__card" role="alert">
        <h1>页面暂时无法打开</h1>
        <p>${escapeHTML(message)}</p>
      </section>
    </main>
  `;
}

function renderWeChatSource() {
  const accountRows = wechatState.accounts.map((account, index) => {
    const active = account.fakeid === wechatState.selectedAccount?.fakeid ? " active" : "";
    return `
      <button class="wechat-source__account${active}" type="button" data-account-index="${index}">
        <span>${escapeHTML(account.nickname || "未命名公众号")}</span>
        <small>${escapeHTML(account.alias || account.fakeid)}</small>
      </button>
    `;
  }).join("");
  const articleRows = wechatState.accountArticles.map((article, index) => `
    <article class="wechat-source__article">
      ${article.cover ? `<img src="${escapeAttribute(article.cover)}" alt="">` : "<div class=\"wechat-source__cover\"></div>"}
      <div>
        <h3>${escapeHTML(article.title || "未命名文章")}</h3>
        <p>${escapeHTML(article.digest || formatArticleTime(article.update_time) || "暂无摘要")}</p>
        <div class="wechat-source__row-actions">
          <button type="button" class="button button-ghost" data-preview-article="${index}">预览</button>
          <button type="button" class="button button-primary" data-import-article="${index}">导入知识库</button>
          ${article.link ? `<a class="button button-link" href="${escapeAttribute(article.link)}" target="_blank" rel="noreferrer">原文</a>` : ""}
        </div>
      </div>
    </article>
  `).join("");
  const status = wechatState.loading
    ? `<div class="web-status">处理中：${escapeHTML(wechatState.loading)}</div>`
    : (wechatState.message ? `<div class="web-status">${escapeHTML(wechatState.message)}</div>` : "");

  renderShell(`
    <main class="wechat-source">
      <section class="wechat-source__header">
        <div>
          <p class="web-kicker">WeChat Source</p>
          <h1>微信公众号来源</h1>
        </div>
        ${status}
      </section>

      <div class="wechat-source__layout">
        <section class="wechat-source__panel">
          <form id="wechat-preview-form" class="source-form">
            <label>
              <span>文章链接</span>
              <input name="articleURL" value="${escapeAttribute(wechatState.articleURL)}" placeholder="https://mp.weixin.qq.com/s/..." autocomplete="off">
            </label>
            <label>
              <span>知识库 ID（可选）</span>
              <input name="bookID" value="${escapeAttribute(wechatState.bookID)}" placeholder="留空自动生成 wechat-...">
            </label>
            <div class="source-form__actions">
              <button class="button button-ghost" type="submit">预览文章</button>
              <button id="wechat-import" class="button button-primary" type="button">导入知识库</button>
            </div>
          </form>

          <form id="wechat-account-form" class="source-form source-form--compact">
            <label>
              <span>搜索公众号</span>
              <input name="accountQuery" value="${escapeAttribute(wechatState.accountQuery)}" placeholder="输入公众号名称">
            </label>
            <button class="button button-primary" type="submit">搜索公众号</button>
          </form>

          <div class="wechat-source__accounts">
            ${accountRows || "<p class=\"web-muted\">搜索后可选择公众号并加载最近文章。</p>"}
          </div>
        </section>

        <section class="wechat-source__panel wechat-source__main">
          <div class="wechat-source__section-head">
            <div>
              <p class="web-kicker">Recent Articles</p>
              <h2>最近文章</h2>
            </div>
            <div class="wechat-source__pager">
              <button class="button button-ghost" type="button" id="wechat-prev" ${wechatState.articleBegin <= 0 ? "disabled" : ""}>上一页</button>
              <button class="button button-ghost" type="button" id="wechat-next" ${wechatState.selectedAccount ? "" : "disabled"}>下一页</button>
            </div>
          </div>
          <div class="wechat-source__articles">
            ${articleRows || "<p class=\"web-muted\">选择公众号后显示文章；也可以直接粘贴文章链接导入。</p>"}
          </div>
        </section>

        <aside class="wechat-source__panel wechat-source__preview">
          ${renderWeChatPreview()}
        </aside>
      </div>

      ${renderWCPlusSource()}
    </main>
  `, "wechat");
  bindWeChatSourceEvents();
  bindWCPlusEvents();
}

function renderWCPlusPage() {
  const status = wcplusState.loading
    ? `<div class="web-status">处理中：${escapeHTML(wcplusState.loading)}</div>`
    : (wcplusState.message ? `<div class="web-status">${escapeHTML(wcplusState.message)}</div>` : "");
  renderShell(`
    <main class="wechat-source wcplus-page">
      <section class="wechat-source__header">
        <div>
          <p class="web-kicker">WC Plus Source</p>
          <h1>WC Plus 公众号工作台</h1>
        </div>
        ${status}
      </section>
      ${renderWCPlusSource(false)}
    </main>
  `, "wcplus");
  bindWCPlusEvents();
}

function refreshWCPlusView() {
  if (window.location.pathname.startsWith("/wcplus-source")) {
    renderWCPlusPage();
    return;
  }
  renderWeChatSource();
}

function firstValue(value, keys) {
  for (const key of keys) {
    const found = value?.[key];
    if (found !== undefined && found !== null && String(found).trim() !== "") {
      return found;
    }
  }
  return "";
}

function firstArray(value, keys) {
  for (const key of keys) {
    if (Array.isArray(value?.[key])) {
      return value[key];
    }
  }
  return [];
}

function wcplusAccountBiz(account) {
  return String(firstValue(account, ["biz", "Biz", "fakeid", "FakeID"]) || "");
}

function wcplusAccountNickname(account) {
  return String(firstValue(account, ["nickname", "Nickname", "name", "Name"]) || "");
}

function wcplusAccountArticleCount(account) {
  return firstValue(account, ["article_count", "ArticleCount", "Articles", "articles"]);
}

function wcplusArticleID(article) {
  return String(firstValue(article, ["id", "ID", "article_id", "ArticleID", "ArticleId", "articleId", "appmsgid", "AppMsgID", "app_msg_id", "msgid", "MsgID", "aid", "Aid"]) || "");
}

function wcplusArticleTitle(article) {
  return String(firstValue(article, ["title", "Title"]) || "");
}

function wcplusArticleNickname(article) {
  return String(firstValue(article, ["nickname", "Nickname", "gzh_nickname", "GzhNickname"]) || "");
}

function wcplusArticleDigest(article) {
  return String(firstValue(article, ["digest", "Digest", "summary", "Summary"]) || "");
}

function wcplusArticleURL(article) {
  return String(firstValue(article, ["url", "URL", "link", "Link", "content_url", "ContentURL", "source_url", "SourceURL"]) || "");
}

function wcplusArticlePublishTime(article) {
  return String(firstValue(article, ["publish_time", "PublishTime", "p_date_text", "PDateText", "pDateText", "date", "Date"]) || "");
}

function renderWCPlusSource(showOwnStatus = true) {
  const accountRows = wcplusState.accounts.map((account, index) => {
    const active = wcplusAccountBiz(account) === wcplusAccountBiz(wcplusState.selectedAccount) ? " active" : "";
    const nickname = wcplusAccountNickname(account) || "未命名公众号";
    const articleCount = wcplusAccountArticleCount(account);
    return `
      <button class="wcplus-source__account${active}" type="button" data-wcplus-account-index="${index}">
        <span>${escapeHTML(nickname)}</span>
        <small>${escapeHTML([wcplusAccountBiz(account), articleCount ? `${articleCount} 篇` : ""].filter(Boolean).join(" · "))}</small>
      </button>
    `;
  }).join("");
  const articleRows = wcplusState.articles.map((article, index) => {
    const id = wcplusArticleID(article);
    const articleURL = wcplusArticleURL(article);
    return `
    <article class="wcplus-source__article">
      <div>
        <h3>${escapeHTML(wcplusArticleTitle(article) || id || "未命名文章")}</h3>
        <p>${escapeHTML([wcplusArticleDigest(article), wcplusArticlePublishTime(article)].filter(Boolean).join(" · ") || articleURL || "暂无摘要")}</p>
      </div>
      <div class="wcplus-source__row-actions">
        <button type="button" class="button button-ghost" data-wcplus-preview="${index}" ${id || articleURL ? "" : "disabled"}>预览</button>
        <button type="button" class="button button-primary" data-wcplus-import="${index}" ${id || articleURL ? "" : "disabled"}>导入知识库</button>
      </div>
    </article>
  `}).join("");
  const searchRows = wcplusState.searchResults.map((item, index) => {
    const articleID = wcplusArticleID(item);
    const articleURL = wcplusArticleURL(item);
    const accountBiz = wcplusAccountBiz(item);
    const title = wcplusArticleTitle(item) || wcplusAccountNickname(item) || articleID || accountBiz || "结果";
    const subline = [wcplusArticleDigest(item), wcplusArticlePublishTime(item), wcplusArticleURL(item), accountBiz].filter(Boolean).join(" · ");
    return `
      <article class="wcplus-source__search-result">
        <div>
          <h4>${escapeHTML(title)}</h4>
          <p>${escapeHTML(subline || "命中结果")}</p>
        </div>
        <div class="wcplus-source__row-actions">
          ${accountBiz ? `<button type="button" class="button button-ghost" data-wcplus-select-result-account="${index}">选择</button>` : ""}
          ${articleID || articleURL ? `<button type="button" class="button button-ghost" data-wcplus-preview-result="${index}">预览</button>` : ""}
          ${articleID || articleURL ? `<button type="button" class="button button-primary" data-wcplus-import-result="${index}">导入</button>` : ""}
        </div>
      </article>
    `;
  }).join("");
  const taskRows = wcplusState.tasks.map((task, index) => `
    <div class="wcplus-source__task">
      <div>
        <strong>${escapeHTML(task.nickname || task.biz || task.task_id)}</strong>
        <span>${escapeHTML(task.status || "unknown")}</span>
      </div>
      <div class="wcplus-source__row-actions">
        <button type="button" class="button button-ghost" data-wcplus-task-start="${index}">开始</button>
        <button type="button" class="button button-ghost" data-wcplus-task-stop="${index}">停止</button>
      </div>
    </div>
  `).join("");
  const rawImportedBook = wcplusState.rawImported?.book;
  const rawImportedHTML = rawImportedBook
    ? `<div class="wcplus-source__imported">
        <strong>已导入：${escapeHTML(rawImportedBook.title || rawImportedBook.book_id || "WC Plus 文章")}</strong>
        <a href="/book-knowledge" data-link>打开书籍知识库</a>
      </div>`
    : "";
  const status = wcplusState.loading
    ? `<div class="web-status">处理中：${escapeHTML(wcplusState.loading)}</div>`
    : (wcplusState.message ? `<div class="web-status">${escapeHTML(wcplusState.message)}</div>` : "");
  const serviceStatus = wcplusState.serviceStatus
    ? `<span class="wcplus-source__badge ${wcplusState.serviceStatus.ok ? "is-ok" : "is-bad"}">${wcplusState.serviceStatus.ok ? "已连接" : "未连接"}</span>`
    : "";
  const envCheckHTML = wcplusState.envCheck ? `
    <section class="wcplus-source__env">
      <div class="wcplus-source__toolbar is-tight">
        <div>
          <p class="web-kicker">Environment</p>
          <h3>环境诊断</h3>
        </div>
        <span class="wcplus-source__badge ${wcplusState.envCheck.ok ? "is-ok" : "is-bad"}">${wcplusState.envCheck.ok ? "通过" : "需处理"}</span>
      </div>
      <div class="wcplus-source__env-list">
        <div class="wcplus-source__env-row wcplus-source__diagnostic-line">
          <strong>服务地址</strong>
          <code>${escapeHTML(wcplusState.envCheck.base_url || "-")}</code>
          <small>这是 kbase 服务端实际访问的 WC Plus API 地址。</small>
        </div>
        ${(Array.isArray(wcplusState.envCheck.checks) ? wcplusState.envCheck.checks : []).map((item) => `
          <div class="wcplus-source__env-row">
            <strong>${escapeHTML(item.name || "check")}</strong>
            <span class="${item.ok ? "is-ok" : "is-bad"}">${item.ok ? "OK" : "FAIL"}</span>
            <small>${escapeHTML(item.message || "-")}</small>
          </div>
        `).join("")}
      </div>
      ${Array.isArray(wcplusState.envCheck.advice) && wcplusState.envCheck.advice.length ? `
        <ul class="wcplus-source__env-advice">
          ${wcplusState.envCheck.advice.map((item) => `<li>${escapeHTML(item)}</li>`).join("")}
        </ul>
      ` : ""}
      <div class="wcplus-source__row-actions">
        <button class="button button-ghost" type="button" data-wcplus-copy-diagnostics>复制诊断</button>
      </div>
    </section>
  ` : "";
  const batchResultHTML = wcplusState.batchResult ? `
    <section class="wcplus-source__batch-result">
      <div class="wcplus-source__toolbar is-tight">
        <div>
          <p class="web-kicker">Batch Result</p>
          <h3>批量结果</h3>
        </div>
        <span class="wcplus-source__badge">成功 ${Array.isArray(wcplusState.batchResult.success) ? wcplusState.batchResult.success.length : 0} / 失败 ${Array.isArray(wcplusState.batchResult.failed) ? wcplusState.batchResult.failed.length : 0} / 入库 ${wcplusState.batchResult.imported_count || 0}</span>
      </div>
      <label>
        <span>成功清单</span>
        <textarea readonly>${escapeHTML(wcplusState.batchResult.success_text || "无成功项")}</textarea>
      </label>
      <label>
        <span>失败清单</span>
        <textarea readonly>${escapeHTML(wcplusState.batchResult.failed_text || "无失败项")}</textarea>
      </label>
      ${Array.isArray(wcplusState.batchResult.import_errors) && wcplusState.batchResult.import_errors.length ? `
        <label>
          <span>入库错误</span>
          <textarea readonly>${escapeHTML(wcplusState.batchResult.import_errors.join("\n"))}</textarea>
        </label>
      ` : ""}
      <div class="wcplus-source__row-actions">
        <button class="button button-ghost" type="button" data-wcplus-copy-batch="success">复制成功</button>
        <button class="button button-ghost" type="button" data-wcplus-copy-batch="failed">复制失败</button>
      </div>
    </section>
  ` : "";
  return `
    <section class="wcplus-source">
      <div class="wcplus-source__toolbar">
        <div>
          <p class="web-kicker">WC Plus Local API</p>
          <h2>WC Plus 本地服务</h2>
          <p class="web-muted">启动时自动检查环境；服务不可达时仍可用下方手动导入知识库。</p>
        </div>
        <div class="wcplus-source__actions">
          ${serviceStatus}
          <button id="wcplus-check-status" class="button button-ghost" type="button">检查状态</button>
          <button id="wcplus-check-env" class="button button-ghost" type="button">环境检查</button>
          <button id="wcplus-load-accounts" class="button button-primary" type="button">加载公众号</button>
          <button id="wcplus-load-tasks" class="button button-ghost" type="button">下载任务</button>
          <button id="wcplus-run-queue" class="button button-ghost" type="button">启动队列</button>
        </div>
      </div>
      <div class="wcplus-source__utility">
        <span>辅助查询</span>
        <button class="button button-ghost" type="button" data-wcplus-utility="reading" ${wcplusState.selectedAccount ? "" : "disabled"}>阅读数据</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="statistics" ${wcplusState.selectedAccount ? "" : "disabled"}>统计数据</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="owner" ${wcplusState.preview || wcplusState.articles.length ? "" : "disabled"}>公众号详情</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="likes">收藏文章</button>
        <button class="button button-ghost" type="button" data-wcplus-utility="request" ${wcplusState.selectedAccount ? "" : "disabled"}>请求公众号</button>
      </div>
      ${showOwnStatus ? status : ""}
      ${envCheckHTML}
      <div class="wcplus-source__grid">
        <aside class="wcplus-source__panel">
          <form id="wcplus-search-form" class="source-form source-form--flat">
            <label>
              <span>搜索 WC Plus</span>
              <input name="query" value="${escapeAttribute(wcplusState.searchQuery)}" placeholder="标题、全文或公众号">
            </label>
            <label>
              <span>范围</span>
              <select name="mode">
                <option value="fulltext" ${wcplusState.searchMode === "fulltext" ? "selected" : ""}>全文</option>
                <option value="title" ${wcplusState.searchMode === "title" ? "selected" : ""}>标题</option>
                <option value="account" ${wcplusState.searchMode === "account" ? "selected" : ""}>已入库公众号</option>
                <option value="candidate" ${wcplusState.searchMode === "candidate" ? "selected" : ""}>可导入公众号</option>
                <option value="all" ${wcplusState.searchMode === "all" ? "selected" : ""}>全库文章</option>
              </select>
            </label>
            <button class="button button-primary" type="submit">搜索</button>
          </form>
          <form id="wcplus-account-options-form" class="wcplus-source__mini-form">
            <label>
              <span>每页</span>
              <input name="accountNum" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.accountNum)}">
            </label>
            <div class="wcplus-source__pager">
              <button class="button button-ghost" type="button" data-wcplus-account-page="-1" ${wcplusState.accountOffset <= 0 ? "disabled" : ""}>上一页</button>
              <button class="button button-ghost" type="button" data-wcplus-account-page="1">下一页</button>
            </div>
          </form>
          <div class="wcplus-source__accounts">
            ${accountRows || "<p class=\"web-muted\">启动 WC Plus 本地服务后，可加载已同步公众号。</p>"}
          </div>
        </aside>
        <section class="wcplus-source__panel wcplus-source__articles">
          <div class="wcplus-source__toolbar is-tight">
            <div>
              <p class="web-kicker">Articles</p>
              <h3>${escapeHTML(wcplusAccountNickname(wcplusState.selectedAccount) || "公众号文章")}</h3>
            </div>
            <div class="wcplus-source__actions">
              <button id="wcplus-create-task" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>同步公众号</button>
              <button id="wcplus-create-batch-task" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>批量任务</button>
              <button id="wcplus-export-text" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>导出 TXT</button>
              <button id="wcplus-export-csv" class="button button-ghost" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>导出 CSV</button>
              <button id="wcplus-import-account" class="button button-primary" type="button" ${wcplusState.selectedAccount ? "" : "disabled"}>批量导入</button>
            </div>
          </div>
          <form id="wcplus-work-options-form" class="wcplus-source__options">
            <label>
              <span>任务类型</span>
              <select name="taskCrawlerType">
                <option value="gzh_article_link" ${wcplusState.taskCrawlerType === "gzh_article_link" ? "selected" : ""}>公众号链接</option>
                <option value="article" ${wcplusState.taskCrawlerType === "article" ? "selected" : ""}>文章内容</option>
                <option value="gzh" ${wcplusState.taskCrawlerType === "gzh" ? "selected" : ""}>公众号信息</option>
              </select>
            </label>
            <label>
              <span>抓取范围</span>
              <select name="taskArticleListType">
                <option value="all" ${wcplusState.taskArticleListType === "all" ? "selected" : ""}>全部</option>
                <option value="amount" ${wcplusState.taskArticleListType === "amount" ? "selected" : ""}>指定篇数</option>
              </select>
            </label>
            <label>
              <span>抓取篇数</span>
              <input name="articleListAmount" type="number" min="0" max="1000" value="${escapeAttribute(wcplusState.taskArticleListAmount)}">
            </label>
            <label>
              <span>导入篇数</span>
              <input name="importLimit" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.importLimit)}">
            </label>
            <label>
              <span>最近导出</span>
              <input name="exportRecentNum" type="number" min="1" max="5000" value="${escapeAttribute(wcplusState.exportRecentNum)}">
            </label>
            <label>
              <span>文章每页</span>
              <input name="articleNum" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.articleNum)}">
            </label>
          </form>
          <div class="wcplus-source__pager is-article">
            <button class="button button-ghost" type="button" data-wcplus-article-page="-1" ${wcplusState.articleOffset <= 0 ? "disabled" : ""}>上一页</button>
            <button class="button button-ghost" type="button" data-wcplus-article-page="1" ${wcplusState.selectedAccount ? "" : "disabled"}>下一页</button>
          </div>
          <div class="wcplus-source__search-results">
            ${searchRows || ""}
          </div>
          <div class="wcplus-source__article-list">
            ${articleRows || "<p class=\"web-muted\">选择公众号后显示已下载文章。</p>"}
          </div>
        </section>
        <aside class="wcplus-source__panel wcplus-source__preview">
          ${renderWCPlusPreview()}
        </aside>
      </div>
      <section class="wcplus-source__tasks">
        <div class="wcplus-source__toolbar is-tight">
          <div>
            <p class="web-kicker">Tasks</p>
            <h3>下载任务</h3>
          </div>
          <div class="wcplus-source__actions">
            <button id="wcplus-clean-batch-tasks" class="button button-ghost" type="button">清理 ready/error</button>
            <button id="wcplus-export-xlsx" class="button button-primary" type="button">导出全库 XLSX</button>
          </div>
        </div>
        <form id="wcplus-batch-nickname-form" class="wcplus-source__batch-form">
          <label>
            <span>批量导入公众号昵称</span>
            <textarea name="nicknames" placeholder="每行一个公众号昵称，严格精确匹配">${escapeHTML(wcplusState.batchNicknames)}</textarea>
          </label>
          <label>
            <span>抓取范围</span>
            <select name="batchArticleListType">
              <option value="all" ${wcplusState.batchArticleListType === "all" ? "selected" : ""}>全部</option>
              <option value="amount" ${wcplusState.batchArticleListType === "amount" ? "selected" : ""}>指定篇数</option>
            </select>
          </label>
          <label>
            <span>抓取篇数</span>
            <input name="batchArticleListAmount" type="number" min="0" max="1000" value="${escapeAttribute(wcplusState.batchArticleListAmount)}">
          </label>
          <label class="wcplus-source__inline-check">
            <input name="exactMatch" type="checkbox" ${wcplusState.batchExactMatch ? "checked" : ""}>
            <span>昵称精确匹配</span>
          </label>
          <label class="wcplus-source__inline-check">
            <input name="importToKBase" type="checkbox" ${wcplusState.batchImportToKBase ? "checked" : ""}>
            <span>同步后导入书籍知识库</span>
          </label>
          <label class="wcplus-source__inline-check">
            <input name="waitForCompletion" type="checkbox" ${wcplusState.batchWaitForCompletion ? "checked" : ""}>
            <span>等待任务完成后入库</span>
          </label>
          <label>
            <span>入库篇数</span>
            <input name="batchImportLimit" type="number" min="1" max="100" value="${escapeAttribute(wcplusState.batchImportLimit)}">
          </label>
          <button class="button button-primary" type="submit">创建链接任务并启动队列</button>
        </form>
        ${batchResultHTML}
        <form id="wcplus-raw-import-form" class="wcplus-source__manual-form">
          <label>
            <span>原文标题</span>
            <input name="rawTitle" value="${escapeAttribute(wcplusState.rawTitle)}" placeholder="文章标题">
          </label>
          <label>
            <span>公众号/作者</span>
            <input name="rawNickname" value="${escapeAttribute(wcplusState.rawNickname)}" placeholder="公众号或作者">
          </label>
          <label>
            <span>原文链接</span>
            <input name="rawURL" value="${escapeAttribute(wcplusState.rawURL)}" placeholder="https://mp.weixin.qq.com/s/...">
          </label>
          <label>
            <span>知识库 ID（可选）</span>
            <input name="rawBookID" value="${escapeAttribute(wcplusState.rawBookID)}" placeholder="留空自动生成">
          </label>
          <label class="is-wide">
            <span>导入 TXT / Markdown 文件</span>
            <input name="rawFile" type="file" accept=".txt,.md,.markdown,text/plain,text/markdown">
          </label>
          <label class="is-wide">
            <span>正文 Markdown / 纯文本</span>
            <textarea name="rawContent" placeholder="粘贴 WC Plus 导出的正文、Markdown 或清洗后的纯文本">${escapeHTML(wcplusState.rawContent)}</textarea>
          </label>
          <div class="wcplus-source__manual-actions">
            <button class="button button-primary" type="submit">手动导入知识库</button>
          </div>
        </form>
        ${rawImportedHTML}
        ${taskRows || "<p class=\"web-muted\">点击“下载任务”查看 WC Plus 同步任务。</p>"}
      </section>
    </section>
  `;
}

async function bootstrapWCPlusSource() {
  if (isWCPlusBootstrapped) {
    return;
  }
  isWCPlusBootstrapped = true;
  wcplusState.loading = "启动时自动检查环境";
  wcplusState.message = "启动时自动检查环境，加载诊断、任务和公众号列表。";
  refreshWCPlusView();

  const accountQuery = new URLSearchParams({
    offset: String(wcplusState.accountOffset),
    num: String(wcplusState.accountNum),
  });
  const [envResult, taskResult, accountResult] = await Promise.allSettled([
    apiFetch("/api/wcplus/env/check"),
    apiFetch("/api/wcplus/task/all"),
    apiFetch(`/api/wcplus/gzh/list?${accountQuery.toString()}`),
  ]);

  const failures = [];
  if (envResult.status === "fulfilled") {
    wcplusState.envCheck = envResult.value;
    wcplusState.serviceStatus = { ok: Boolean(envResult.value?.ok) };
    if (!envResult.value?.ok) {
      failures.push("环境检查");
    }
  } else {
    wcplusState.serviceStatus = { ok: false };
    failures.push("环境检查");
  }

  if (taskResult.status === "fulfilled") {
    wcplusState.tasks = Array.isArray(taskResult.value.tasks) ? taskResult.value.tasks : [];
  } else {
    failures.push("任务列表");
  }

  if (accountResult.status === "fulfilled") {
    wcplusState.accounts = Array.isArray(accountResult.value.accounts) ? accountResult.value.accounts : [];
    wcplusState.selectedAccount = wcplusState.accounts[0] || null;
    if (wcplusState.selectedAccount) {
      await loadWCPlusArticles(false);
    }
  } else {
    failures.push("公众号列表");
  }

  wcplusState.loading = "";
  if (failures.length) {
    wcplusState.message = `启动检查完成，但 ${failures.join("、")} 需要处理；可继续使用手动导入知识库。`;
  } else {
    wcplusState.message = `启动检查完成：${wcplusState.accounts.length} 个公众号，${wcplusState.tasks.length} 个任务。`;
  }
  refreshWCPlusView();
}

function renderWCPlusPreview() {
  const article = wcplusState.preview;
  const utility = wcplusState.utilityResult;
  const utilityHTML = utility ? `
    <div class="wcplus-source__utility-result">
      <p class="web-kicker">Auxiliary Result</p>
      <h4>${escapeHTML(utility.title || "辅助查询结果")}</h4>
      <pre>${escapeHTML(JSON.stringify(utility.payload || {}, null, 2).slice(0, 3200))}</pre>
    </div>
  ` : "";
  if (!article) {
    return `
      <p class="web-kicker">WC Plus Preview</p>
      <h3>等待文章预览</h3>
      <p class="web-muted">从 WC Plus 已下载文章中选择预览，确认内容后可导入书籍知识库。</p>
      ${utilityHTML}
    `;
  }
  return `
    <p class="web-kicker">WC Plus Preview</p>
    <h3>${escapeHTML(wcplusArticleTitle(article) || wcplusArticleID(article) || "未命名文章")}</h3>
    <div class="wechat-source__meta">
      <span>${escapeHTML(wcplusArticleNickname(article) || "未知公众号")}</span>
      <span>${escapeHTML(wcplusArticlePublishTime(article) || "")}</span>
    </div>
    <pre>${escapeHTML(String(firstValue(article, ["content", "Content"]) || "").slice(0, 2200))}</pre>
    ${utilityHTML}
  `;
}

function renderWeChatPreview() {
  const article = wechatState.preview;
  if (!article) {
    return `
      <p class="web-kicker">Preview</p>
      <h2>等待预览</h2>
      <p class="web-muted">文章预览会展示标题、公众号、摘要和正文片段。导入后会生成单篇文章知识包。</p>
      ${wechatState.imported ? renderImportedPackage() : ""}
    `;
  }
  return `
    <p class="web-kicker">Preview</p>
    <h2>${escapeHTML(article.title || "未命名文章")}</h2>
    <div class="wechat-source__meta">
      <span>${escapeHTML(article.account_name || "未知公众号")}</span>
      <span>${escapeHTML(article.published_at || "")}</span>
    </div>
    ${article.digest ? `<p class="wechat-source__digest">${escapeHTML(article.digest)}</p>` : ""}
    <pre>${escapeHTML((article.markdown || article.text || "").slice(0, 2600))}</pre>
    ${wechatState.imported ? renderImportedPackage() : ""}
  `;
}

function renderImportedPackage() {
  const book = wechatState.imported?.book || {};
  const id = book.book_id || "";
  return `
    <div class="wechat-source__imported">
      <p class="web-kicker">Imported</p>
      <strong>${escapeHTML(book.title || id || "已导入")}</strong>
      ${id ? `<a href="/ebook/${encodeURIComponent(id)}">打开阅读页</a>` : ""}
    </div>
  `;
}

function bindWeChatSourceEvents() {
  document.querySelector("#wechat-preview-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wechatState.articleURL = String(data.get("articleURL") || "").trim();
    wechatState.bookID = String(data.get("bookID") || "").trim();
    await previewWeChatArticle(wechatState.articleURL);
  });

  document.querySelector("#wechat-import")?.addEventListener("click", async () => {
    await importWeChatArticle(wechatState.articleURL);
  });

  document.querySelector("#wechat-account-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wechatState.accountQuery = String(data.get("accountQuery") || "").trim();
    await searchWeChatAccounts();
  });

  for (const button of document.querySelectorAll("[data-account-index]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-account-index") || "0");
      wechatState.selectedAccount = wechatState.accounts[index] || null;
      wechatState.articleBegin = 0;
      await loadWeChatAccountArticles();
    });
  }

  for (const button of document.querySelectorAll("[data-preview-article]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-preview-article") || "0");
      const article = wechatState.accountArticles[index];
      if (article?.link) {
        wechatState.articleURL = article.link;
        await previewWeChatArticle(article.link);
      }
    });
  }

  for (const button of document.querySelectorAll("[data-import-article]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-import-article") || "0");
      const article = wechatState.accountArticles[index];
      if (article?.link) {
        wechatState.articleURL = article.link;
        await importWeChatArticle(article.link);
      }
    });
  }

  document.querySelector("#wechat-prev")?.addEventListener("click", async () => {
    wechatState.articleBegin = Math.max(0, wechatState.articleBegin - wechatState.articleCount);
    await loadWeChatAccountArticles();
  });

  document.querySelector("#wechat-next")?.addEventListener("click", async () => {
    wechatState.articleBegin += wechatState.articleCount;
    await loadWeChatAccountArticles();
  });
}

function bindWCPlusEvents() {
  document.querySelector("#wcplus-account-options-form")?.addEventListener("change", () => {
    readWCPlusOptionsFromDOM();
  });
  document.querySelector("#wcplus-work-options-form")?.addEventListener("change", () => {
    readWCPlusOptionsFromDOM();
  });
  document.querySelector("#wcplus-check-status")?.addEventListener("click", () => {
    checkWCPlusStatus();
  });
  document.querySelector("#wcplus-check-env")?.addEventListener("click", () => {
    checkWCPlusEnvironment();
  });
  document.querySelector("#wcplus-load-accounts")?.addEventListener("click", () => {
    loadWCPlusAccounts();
  });
  document.querySelector("#wcplus-load-tasks")?.addEventListener("click", () => {
    loadWCPlusTasks();
  });
  document.querySelector("#wcplus-run-queue")?.addEventListener("click", () => {
    runWCPlusQueue();
  });
  document.querySelector("#wcplus-search-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wcplusState.searchQuery = String(data.get("query") || "").trim();
    wcplusState.searchMode = String(data.get("mode") || "fulltext");
    await searchWCPlus();
  });
  document.querySelector("#wcplus-create-task")?.addEventListener("click", () => {
    createWCPlusTask();
  });
  document.querySelector("#wcplus-create-batch-task")?.addEventListener("click", () => {
    createWCPlusBatchTask();
  });
  document.querySelector("#wcplus-import-account")?.addEventListener("click", () => {
    importWCPlusAccount();
  });
  document.querySelector("#wcplus-export-text")?.addEventListener("click", () => {
    exportWCPlusText();
  });
  document.querySelector("#wcplus-export-csv")?.addEventListener("click", () => {
    exportWCPlusCSV();
  });
  document.querySelector("#wcplus-clean-batch-tasks")?.addEventListener("click", () => {
    cleanWCPlusBatchTasks();
  });
  document.querySelector("#wcplus-export-xlsx")?.addEventListener("click", () => {
    exportWCPlusAllArticlesXLSX();
  });
  document.querySelector("#wcplus-batch-nickname-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    wcplusState.batchNicknames = String(data.get("nicknames") || "");
    wcplusState.batchExactMatch = data.get("exactMatch") === "on";
    wcplusState.batchArticleListType = String(data.get("batchArticleListType") || "all");
    wcplusState.batchArticleListAmount = boundedNumber(data.get("batchArticleListAmount"), 0, 1000, 0);
    wcplusState.batchImportToKBase = data.get("importToKBase") === "on";
    wcplusState.batchWaitForCompletion = data.get("waitForCompletion") === "on";
    wcplusState.batchImportLimit = boundedNumber(data.get("batchImportLimit"), 1, 100, 10);
    await batchImportWCPlusNicknames();
  });
  for (const button of document.querySelectorAll("[data-wcplus-copy-batch]")) {
    button.addEventListener("click", async () => {
      await copyWCPlusBatchText(String(button.getAttribute("data-wcplus-copy-batch") || ""));
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-utility]")) {
    button.addEventListener("click", async () => {
      await runWCPlusUtility(String(button.getAttribute("data-wcplus-utility") || ""));
    });
  }
  document.querySelector("[data-wcplus-copy-diagnostics]")?.addEventListener("click", async () => {
    await copyWCPlusDiagnostics();
  });
  document.querySelector("#wcplus-raw-import-form input[name=\"rawFile\"]")?.addEventListener("change", async (event) => {
    const [file] = Array.from(event.currentTarget.files || []);
    if (file) {
      try {
        await loadWCPlusRawFile(file);
      } catch (error) {
        wcplusState.message = error instanceof Error ? error.message : String(error);
        refreshWCPlusView();
      }
    }
  });
  document.querySelector("#wcplus-raw-import-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    readWCPlusRawFormFromDOM();
    await importRawWCPlusArticle();
  });
  for (const button of document.querySelectorAll("[data-wcplus-account-page]")) {
    button.addEventListener("click", async () => {
      const delta = Number(button.getAttribute("data-wcplus-account-page") || "0");
      await pageWCPlusAccounts(delta);
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-article-page]")) {
    button.addEventListener("click", async () => {
      const delta = Number(button.getAttribute("data-wcplus-article-page") || "0");
      await pageWCPlusArticles(delta);
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-account-index]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-account-index") || "0");
      wcplusState.selectedAccount = wcplusState.accounts[index] || null;
      wcplusState.articleOffset = 0;
      await loadWCPlusArticles();
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-preview]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-preview") || "0");
      const article = wcplusState.articles[index];
      if (article) {
        await previewWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-select-result-account]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-select-result-account") || "0");
      wcplusState.selectedAccount = wcplusState.searchResults[index] || null;
      wcplusState.articleOffset = 0;
      await loadWCPlusArticles();
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-preview-result]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-preview-result") || "0");
      const article = wcplusState.searchResults[index];
      if (article) {
        await previewWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-import-result]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-import-result") || "0");
      const article = wcplusState.searchResults[index];
      if (article) {
        await importWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-import]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-import") || "0");
      const article = wcplusState.articles[index];
      if (article) {
        await importWCPlusArticle(article);
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-task-start]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-task-start") || "0");
      const task = wcplusState.tasks[index];
      if (task) {
        await controlWCPlusTask(task, "start");
      }
    });
  }
  for (const button of document.querySelectorAll("[data-wcplus-task-stop]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-wcplus-task-stop") || "0");
      const task = wcplusState.tasks[index];
      if (task) {
        await controlWCPlusTask(task, "stop");
      }
    });
  }
}

function boundedNumber(value, min, max, fallback) {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return Math.min(max, Math.max(min, parsed));
}

function readWCPlusOptionsFromDOM() {
  const accountOptions = document.querySelector("#wcplus-account-options-form");
  if (accountOptions) {
    const data = new FormData(accountOptions);
    wcplusState.accountNum = boundedNumber(data.get("accountNum"), 1, 100, wcplusState.accountNum);
  }
  const workOptions = document.querySelector("#wcplus-work-options-form");
  if (workOptions) {
    const data = new FormData(workOptions);
    wcplusState.taskCrawlerType = String(data.get("taskCrawlerType") || "gzh_article_link");
    wcplusState.taskArticleListType = String(data.get("taskArticleListType") || "all");
    wcplusState.taskArticleListAmount = boundedNumber(data.get("articleListAmount"), 0, 1000, wcplusState.taskArticleListAmount);
    wcplusState.importLimit = boundedNumber(data.get("importLimit"), 1, 100, wcplusState.importLimit);
    wcplusState.exportRecentNum = boundedNumber(data.get("exportRecentNum"), 1, 5000, wcplusState.exportRecentNum);
    wcplusState.articleNum = boundedNumber(data.get("articleNum"), 1, 100, wcplusState.articleNum);
  }
}

function readWCPlusRawFormFromDOM() {
  const rawForm = document.querySelector("#wcplus-raw-import-form");
  if (!rawForm) {
    return;
  }
  const data = new FormData(rawForm);
  wcplusState.rawTitle = String(data.get("rawTitle") || "").trim();
  wcplusState.rawNickname = String(data.get("rawNickname") || "").trim();
  wcplusState.rawURL = String(data.get("rawURL") || "").trim();
  wcplusState.rawBookID = String(data.get("rawBookID") || "").trim();
  wcplusState.rawContent = String(data.get("rawContent") || "");
}

async function pageWCPlusAccounts(delta) {
  readWCPlusOptionsFromDOM();
  wcplusState.accountOffset = Math.max(0, wcplusState.accountOffset + (delta * wcplusState.accountNum));
  await loadWCPlusAccounts();
}

async function pageWCPlusArticles(delta) {
  readWCPlusOptionsFromDOM();
  wcplusState.articleOffset = Math.max(0, wcplusState.articleOffset + (delta * wcplusState.articleNum));
  await loadWCPlusArticles();
}

async function previewWeChatArticle(rawURL) {
  if (!rawURL) {
    wechatState.message = "请先输入文章链接。";
    renderWeChatSource();
    return;
  }
  wechatState.loading = "预览文章";
  wechatState.message = "";
  renderWeChatSource();
  try {
    const query = new URLSearchParams({ url: rawURL });
    const payload = await apiFetch(`/api/wechat/article?${query.toString()}`);
    wechatState.preview = payload.article || null;
    wechatState.message = "文章预览已更新。";
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function importWeChatArticle(rawURL) {
  if (!rawURL) {
    wechatState.message = "请先输入文章链接。";
    renderWeChatSource();
    return;
  }
  wechatState.loading = "导入知识库";
  wechatState.message = "";
  renderWeChatSource();
  try {
    wechatState.imported = await apiFetch("/api/wechat/import", {
      method: "POST",
      body: JSON.stringify({
        url: rawURL,
        book_id: wechatState.bookID,
      }),
    });
    wechatState.preview = wechatState.preview || {
      title: wechatState.imported?.book?.title || "",
      source_url: rawURL,
      markdown: "",
      text: "",
    };
    wechatState.message = "已导入书籍知识库。";
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function searchWeChatAccounts() {
  if (!wechatState.accountQuery) {
    wechatState.message = "请输入公众号名称。";
    renderWeChatSource();
    return;
  }
  wechatState.loading = "搜索公众号";
  wechatState.message = "";
  renderWeChatSource();
  try {
    const query = new URLSearchParams({ q: wechatState.accountQuery });
    const payload = await apiFetch(`/api/wechat/search?${query.toString()}`);
    wechatState.accounts = Array.isArray(payload.accounts) ? payload.accounts : [];
    wechatState.selectedAccount = wechatState.accounts[0] || null;
    wechatState.articleBegin = 0;
    if (wechatState.selectedAccount) {
      await loadWeChatAccountArticles(false);
    } else {
      wechatState.accountArticles = [];
    }
    wechatState.message = wechatState.accounts.length ? "请选择公众号或直接导入文章。" : "未找到公众号。";
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function loadWeChatAccountArticles(renderBefore = true) {
  const fakeid = wechatState.selectedAccount?.fakeid || "";
  if (!fakeid) {
    return;
  }
  wechatState.loading = "加载最近文章";
  wechatState.message = "";
  if (renderBefore) {
    renderWeChatSource();
  }
  try {
    const query = new URLSearchParams({
      fakeid,
      begin: String(wechatState.articleBegin),
      count: String(wechatState.articleCount),
    });
    const payload = await apiFetch(`/api/wechat/articles?${query.toString()}`);
    wechatState.accountArticles = Array.isArray(payload.articles) ? payload.articles : [];
    wechatState.message = `已加载 ${wechatState.accountArticles.length} 篇文章。`;
  } catch (error) {
    wechatState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wechatState.loading = "";
    renderWeChatSource();
  }
}

async function loadWCPlusAccounts() {
  readWCPlusOptionsFromDOM();
  wcplusState.loading = "加载 WC Plus 公众号";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      offset: String(wcplusState.accountOffset),
      num: String(wcplusState.accountNum),
    });
    const payload = await apiFetch(`/api/wcplus/gzh/list?${query.toString()}`);
    wcplusState.accounts = Array.isArray(payload.accounts) ? payload.accounts : [];
    wcplusState.selectedAccount = wcplusState.accounts[0] || null;
    wcplusState.articles = [];
    if (wcplusState.selectedAccount) {
      await loadWCPlusArticles(false);
    }
    wcplusState.message = `已加载 ${wcplusState.accounts.length} 个公众号。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function loadWCPlusArticles(renderBefore = true) {
  readWCPlusOptionsFromDOM();
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    return;
  }
  wcplusState.loading = "加载 WC Plus 文章";
  wcplusState.message = "";
  if (renderBefore) {
    refreshWCPlusView();
  }
  try {
    const query = new URLSearchParams({
      biz,
      nickname: wcplusAccountNickname(account),
      offset: String(wcplusState.articleOffset),
      num: String(wcplusState.articleNum),
    });
    const payload = await apiFetch(`/api/wcplus/gzh/articles?${query.toString()}`);
    wcplusState.articles = Array.isArray(payload.articles) ? payload.articles : [];
    wcplusState.message = `已加载 ${wcplusState.articles.length} 篇 WC Plus 文章。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function previewWCPlusArticle(article) {
  const nickname = wcplusArticleNickname(article) || wcplusAccountNickname(wcplusState.selectedAccount);
  const id = wcplusArticleID(article);
  const articleURL = wcplusArticleURL(article);
  if ((!nickname || !id) && !articleURL) {
    wcplusState.message = "文章缺少 nickname/id 或 URL。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "预览 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = id ? new URLSearchParams({ nickname, id }) : new URLSearchParams({ url: articleURL });
    wcplusState.preview = await apiFetch(`/api/wcplus/article/content?${query.toString()}`);
    wcplusState.message = "WC Plus 文章预览已更新。";
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function importWCPlusArticle(article) {
  const nickname = wcplusArticleNickname(article) || wcplusAccountNickname(wcplusState.selectedAccount);
  const id = wcplusArticleID(article);
  const articleURL = wcplusArticleURL(article);
  if ((!nickname || !id) && !articleURL) {
    wcplusState.message = "文章缺少 nickname/id 或 URL。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "导入 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const payload = await apiFetch("/api/wcplus/import/article", {
      method: "POST",
      body: JSON.stringify(id ? { nickname, id } : { url: articleURL }),
    });
    wcplusState.message = `已导入：${payload.book?.title || wcplusArticleTitle(article) || id || articleURL}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function importRawWCPlusArticle() {
  if (!wcplusState.rawTitle) {
    wcplusState.message = "请先输入原文标题。";
    refreshWCPlusView();
    return;
  }
  if (!wcplusState.rawContent.trim()) {
    wcplusState.message = "请先粘贴正文内容。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "手动导入 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    wcplusState.rawImported = await apiFetch("/api/wcplus/import/raw", {
      method: "POST",
      body: JSON.stringify({
        title: wcplusState.rawTitle,
        nickname: wcplusState.rawNickname,
        url: wcplusState.rawURL,
        book_id: wcplusState.rawBookID,
        content: wcplusState.rawContent,
      }),
    });
    wcplusState.message = `已手动导入：${wcplusState.rawImported?.book?.title || wcplusState.rawTitle}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function importWCPlusAccount() {
  readWCPlusOptionsFromDOM();
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "批量导入 WC Plus 文章";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const payload = await apiFetch("/api/wcplus/import/account", {
      method: "POST",
      body: JSON.stringify({
        biz,
        nickname: wcplusAccountNickname(account),
        limit: wcplusState.importLimit,
      }),
    });
    wcplusState.message = `批量导入完成：${payload.imported_count || 0} 篇。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function checkWCPlusStatus() {
  wcplusState.loading = "检查 WC Plus 状态";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    wcplusState.serviceStatus = await apiFetch("/api/wcplus/status");
    wcplusState.message = wcplusState.serviceStatus?.ok ? "WC Plus 本地服务已连接。" : "WC Plus 本地服务未连接。";
  } catch (error) {
    wcplusState.serviceStatus = { ok: false };
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function checkWCPlusEnvironment() {
  wcplusState.loading = "检查 WC Plus 环境";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/env/check");
    wcplusState.envCheck = result;
    wcplusState.serviceStatus = { ok: Boolean(result.ok) };
    const failed = Array.isArray(result.checks)
      ? result.checks.filter((item) => !item.ok).map((item) => item.name).join(", ")
      : "";
    wcplusState.message = result.ok ? "环境检查通过。" : `环境检查未通过：${failed || "请检查服务状态"}`;
  } catch (error) {
    wcplusState.serviceStatus = { ok: false };
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function searchWCPlus() {
  if (!wcplusState.searchQuery && wcplusState.searchMode !== "all") {
    wcplusState.message = "请输入搜索关键词。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "搜索 WC Plus";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      q: wcplusState.searchQuery,
      offset: "0",
      num: "30",
      sort: "p_date",
      direction: "desc",
    });
    const endpointByMode = {
      fulltext: "/api/wcplus/search",
      title: "/api/wcplus/article/search-title",
      account: "/api/wcplus/gzh/search",
      candidate: "/api/wcplus/search-gzh",
      all: "/api/wcplus/article/all",
    };
    const payload = await apiFetch(`${endpointByMode[wcplusState.searchMode] || endpointByMode.fulltext}?${query.toString()}`);
    if (wcplusState.searchMode === "account" || wcplusState.searchMode === "candidate") {
      wcplusState.searchResults = firstArray(payload, ["accounts", "Accounts", "gzhs", "Gzhs", "candidates", "Candidates"]);
    } else {
      wcplusState.searchResults = firstArray(payload, ["results", "Results", "articles", "Articles", "items", "Items"]);
    }
    wcplusState.message = `搜索完成：${wcplusState.searchResults.length} 条结果。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function runWCPlusUtility(kind) {
  const biz = wcplusAccountBiz(wcplusState.selectedAccount);
  const articleID = wcplusArticleID(wcplusState.preview || wcplusState.articles[0] || {});
  const query = new URLSearchParams();
  const utilityByKind = {
    reading: {
      title: "阅读数据",
      endpoint: "/api/wcplus/report/reading-data",
      needsBiz: true,
    },
    statistics: {
      title: "统计数据",
      endpoint: "/api/wcplus/report/statistic-data",
      needsBiz: true,
    },
    owner: {
      title: "公众号详情",
      endpoint: "/api/wcplus/article/gzh",
      needsArticleID: true,
    },
    likes: {
      title: "收藏文章",
      endpoint: "/api/wcplus/like-articles",
      defaults: { offset: "0", num: String(wcplusState.articleNum || 20) },
    },
    request: {
      title: "请求公众号",
      endpoint: "/api/wcplus/request/gzh",
      needsBiz: true,
    },
  };
  const utility = utilityByKind[kind];
  if (!utility) {
    wcplusState.message = "未知辅助查询。";
    refreshWCPlusView();
    return;
  }
  if (utility.needsBiz && !biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  if (utility.needsArticleID && !articleID) {
    wcplusState.message = "请先预览或加载一篇带 id 的文章。";
    refreshWCPlusView();
    return;
  }
  if (utility.needsBiz) {
    query.set("biz", biz);
  }
  if (utility.needsArticleID) {
    query.set("id", articleID);
  }
  for (const [key, value] of Object.entries(utility.defaults || {})) {
    query.set(key, value);
  }

  wcplusState.loading = utility.title;
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const suffix = query.toString() ? `?${query.toString()}` : "";
    const payload = await apiFetch(`${utility.endpoint}${suffix}`);
    wcplusState.utilityResult = {
      title: utility.title,
      payload,
    };
    wcplusState.message = `${utility.title}已更新。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function batchImportWCPlusNicknames() {
  const nicknames = wcplusState.batchNicknames
    .split(/\r?\n/)
    .map((value) => value.trim())
    .filter(Boolean);
  if (!nicknames.length) {
    wcplusState.message = "请先输入公众号昵称。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "批量导入公众号昵称";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const articleListAmount = wcplusState.batchArticleListType === "amount" ? wcplusState.batchArticleListAmount : 0;
    const result = await apiFetch("/api/wcplus/batch-import/gzh", {
      method: "POST",
      body: JSON.stringify({
        nicknames,
        articleListType: wcplusState.batchArticleListType,
        articleListAmount,
        start_queue: true,
        exact_match: wcplusState.batchExactMatch,
        import_to_kbase: wcplusState.batchImportToKBase,
        wait_for_completion: wcplusState.batchWaitForCompletion,
        import_limit: wcplusState.batchImportLimit,
        poll_attempts: wcplusState.batchWaitForCompletion ? 30 : 0,
        poll_interval_millis: wcplusState.batchWaitForCompletion ? 2000 : 0,
      }),
    });
    wcplusState.batchResult = result;
    const successCount = Array.isArray(result.success) ? result.success.length : 0;
    const failedCount = Array.isArray(result.failed) ? result.failed.length : 0;
    const importedCount = result.imported_count || 0;
    wcplusState.message = `批量任务完成：成功 ${successCount}，失败 ${failedCount}${result.started ? "，队列已启动" : ""}${wcplusState.batchImportToKBase ? `，入库 ${importedCount} 篇` : ""}。`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function copyWCPlusBatchText(kind) {
  const result = wcplusState.batchResult || {};
  const text = kind === "success" ? result.success_text : result.failed_text;
  if (!text) {
    wcplusState.message = kind === "success" ? "暂无成功清单。" : "暂无失败清单。";
    refreshWCPlusView();
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    wcplusState.message = "已复制到剪贴板。";
  } catch {
    wcplusState.message = "浏览器不允许写入剪贴板，请手动复制文本框内容。";
  }
  refreshWCPlusView();
}

function wcplusDiagnosticText() {
  const check = wcplusState.envCheck || {};
  const lines = [
    `WC Plus environment: ${check.ok ? "OK" : "NEEDS_ACTION"}`,
    `base_url: ${check.base_url || "-"}`,
  ];
  if (Array.isArray(check.checks) && check.checks.length) {
    lines.push("", "checks:");
    for (const item of check.checks) {
      lines.push(`- ${item.name || "check"}: ${item.ok ? "OK" : "FAIL"} ${item.message || ""}`.trim());
    }
  }
  if (Array.isArray(check.advice) && check.advice.length) {
    lines.push("", "advice:");
    for (const item of check.advice) {
      lines.push(`- ${item}`);
    }
  }
  const batch = wcplusState.batchResult;
  if (batch) {
    lines.push(
      "",
      `batch_success: ${Array.isArray(batch.success) ? batch.success.length : 0}`,
      `batch_failed: ${Array.isArray(batch.failed) ? batch.failed.length : 0}`,
    );
    if (batch.failed_text) {
      lines.push("", "failed_text:", batch.failed_text);
    }
  }
  return lines.join("\n");
}

async function copyWCPlusDiagnostics() {
  if (!wcplusState.envCheck) {
    wcplusState.message = "请先执行环境检查。";
    refreshWCPlusView();
    return;
  }
  try {
    await navigator.clipboard.writeText(wcplusDiagnosticText());
    wcplusState.message = "诊断信息已复制。";
  } catch {
    wcplusState.message = "浏览器不允许写入剪贴板，请手动复制环境诊断内容。";
  }
  refreshWCPlusView();
}

async function loadWCPlusRawFile(file) {
  readWCPlusRawFormFromDOM();
  const text = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error || new Error("读取文件失败"));
    reader.readAsText(file);
  });
  wcplusState.rawContent = String(text);
  if (!wcplusState.rawTitle) {
    wcplusState.rawTitle = file.name.replace(/\.(txt|md|markdown)$/i, "");
  }
  wcplusState.message = `已读取文件：${file.name}`;
  refreshWCPlusView();
}

async function loadWCPlusTasks() {
  wcplusState.loading = "加载 WC Plus 下载任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const payload = await apiFetch("/api/wcplus/task/all");
    wcplusState.tasks = Array.isArray(payload.tasks) ? payload.tasks : [];
    wcplusState.message = `已加载 ${wcplusState.tasks.length} 个任务。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function createWCPlusTask() {
  readWCPlusOptionsFromDOM();
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "创建 WC Plus 同步任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const task = await apiFetch("/api/wcplus/task/new", {
      method: "POST",
      body: JSON.stringify({
        biz,
        nickname: wcplusAccountNickname(account),
        crawlerType: wcplusState.taskCrawlerType,
        articleListType: wcplusState.taskArticleListType,
        articleListAmount: wcplusState.taskArticleListAmount,
      }),
    });
    wcplusState.message = `已创建同步任务：${task.task_id || wcplusAccountNickname(account) || biz}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function createWCPlusBatchTask() {
  readWCPlusOptionsFromDOM();
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "创建 WC Plus 批量任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/batch-task/create", {
      method: "POST",
      body: JSON.stringify({
        biz,
        nickname: wcplusAccountNickname(account),
        crawlerType: wcplusState.taskCrawlerType,
        articleListType: wcplusState.taskArticleListType,
        articleListAmount: wcplusState.taskArticleListAmount,
      }),
    });
    wcplusState.message = `批量任务已创建：${firstValue(result, ["task_id", "TaskID", "status", "Status"]) || "已提交"}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function controlWCPlusTask(task, action) {
  if (!task?.task_id) {
    wcplusState.message = "任务缺少 task_id。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "更新 WC Plus 任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const updated = await apiFetch("/api/wcplus/task/control", {
      method: "POST",
      body: JSON.stringify({ task_id: task.task_id, action }),
    });
    wcplusState.message = `任务状态：${updated.status || action}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function runWCPlusQueue() {
  wcplusState.loading = "启动 WC Plus 队列";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/task/control", {
      method: "POST",
      body: JSON.stringify({ command: "run" }),
    });
    wcplusState.message = `队列已启动：${firstValue(result, ["status", "Status", "message", "Message"]) || "running"}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function cleanWCPlusBatchTasks() {
  wcplusState.loading = "清理 WC Plus 批量任务";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const result = await apiFetch("/api/wcplus/batch-task/delete", {
      method: "POST",
      body: JSON.stringify({ status: ["ready", "error"] }),
    });
    wcplusState.message = `批量任务已清理：${firstValue(result, ["deleted", "Deleted", "count", "Count"]) || "完成"}`;
    await loadWCPlusTasks(false);
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function exportWCPlusText() {
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "导出 WC Plus TXT";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      biz,
      nickname: wcplusAccountNickname(account),
      only_main: "true",
      need_img: "false",
      open_dir: "false",
    });
    const result = await apiFetch(`/api/wcplus/export/text?${query.toString()}`);
    wcplusState.message = `TXT 导出已触发：${JSON.stringify(result)}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function exportWCPlusCSV() {
  const account = wcplusState.selectedAccount;
  const biz = wcplusAccountBiz(account);
  if (!biz) {
    wcplusState.message = "请先选择公众号。";
    refreshWCPlusView();
    return;
  }
  wcplusState.loading = "导出 WC Plus CSV";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const query = new URLSearchParams({
      biz,
      nickname: wcplusAccountNickname(account),
      open_dir: "false",
    });
    const result = await apiFetch(`/api/wcplus/export/gzh-csv?${query.toString()}`);
    wcplusState.message = `CSV 导出已触发：${JSON.stringify(result)}`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

async function exportWCPlusAllArticlesXLSX() {
  readWCPlusOptionsFromDOM();
  wcplusState.loading = "导出 WC Plus 全库 XLSX";
  wcplusState.message = "";
  refreshWCPlusView();
  try {
    const size = await apiDownload("/api/wcplus/export/all-articles-xlsx", {
      method: "POST",
      body: JSON.stringify({
        sort: "p_date",
        direction: "desc",
        only_headline: false,
        range_mode: "recent",
        recent_num: wcplusState.exportRecentNum,
        fields: [
          "gzh_nickname",
          "title",
          "author",
          "p_date_text",
          "read_num",
          "like_num",
          "comment_num",
          "digest",
          "content_url",
          "source_url",
          "content",
        ],
      }),
    }, "wcplus-all-articles.xlsx");
    wcplusState.message = `XLSX 已下载：${size} bytes。`;
  } catch (error) {
    wcplusState.message = error instanceof Error ? error.message : String(error);
  } finally {
    wcplusState.loading = "";
    refreshWCPlusView();
  }
}

function bindBookKnowledgeEvents() {
  document.querySelector("#knowledge-refresh")?.addEventListener("click", () => {
    loadBookKnowledge();
  });
  document.querySelector("#knowledge-search-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    knowledgeState.query = String(data.get("query") || "").trim();
    await searchBookKnowledge();
  });
  for (const button of document.querySelectorAll("[data-book-index]")) {
    button.addEventListener("click", async () => {
      const index = Number(button.getAttribute("data-book-index") || "0");
      const book = knowledgeState.books[index] || null;
      if (book) {
        await selectKnowledgeBook(book);
      }
    });
  }
}

async function loadBookKnowledge() {
  knowledgeState.loading = "加载书籍";
  knowledgeState.message = "";
  renderBookKnowledge();
  try {
    const payload = await apiFetch("/api/books");
    knowledgeState.books = Array.isArray(payload.books) ? payload.books : [];
    if (knowledgeState.books.length) {
      const preferred = knowledgeState.selectedBook?.book_id
        ? knowledgeState.books.find((book) => book.book_id === knowledgeState.selectedBook.book_id)
        : null;
      await selectKnowledgeBook(preferred || knowledgeState.books[0], false);
    } else {
      knowledgeState.selectedBook = null;
      knowledgeState.package = null;
      knowledgeState.results = [];
    }
    knowledgeState.message = `已加载 ${knowledgeState.books.length} 本。`;
  } catch (error) {
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.loading = "";
    renderBookKnowledge();
  }
}

async function selectKnowledgeBook(book, renderBefore = true) {
  knowledgeState.selectedBook = book;
  knowledgeState.package = null;
  knowledgeState.results = [];
  knowledgeState.loading = "加载详情";
  if (renderBefore) {
    renderBookKnowledge();
  }
  try {
    knowledgeState.package = await apiFetch(`/api/books/${encodeURIComponent(book.book_id)}`);
  } catch (error) {
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.loading = "";
    if (renderBefore) {
      renderBookKnowledge();
    }
  }
}

async function searchBookKnowledge() {
  if (!knowledgeState.query) {
    knowledgeState.results = [];
    renderBookKnowledge();
    return;
  }
  knowledgeState.loading = "检索";
  knowledgeState.message = "";
  renderBookKnowledge();
  try {
    const query = new URLSearchParams({
      q: knowledgeState.query,
      book_id: knowledgeState.selectedBook?.book_id || "",
      limit: "20",
    });
    const payload = await apiFetch(`/api/search?${query.toString()}`);
    knowledgeState.results = Array.isArray(payload.results) ? payload.results : [];
    knowledgeState.message = `找到 ${knowledgeState.results.length} 条结果。`;
  } catch (error) {
    knowledgeState.message = error instanceof Error ? error.message : String(error);
  } finally {
    knowledgeState.loading = "";
    renderBookKnowledge();
  }
}

function formatArticleTime(value) {
  if (!value) {
    return "";
  }
  const date = new Date(Number(value) * 1000);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleDateString("zh-CN");
}

async function boot() {
  if (window.location.pathname.startsWith("/wechat-source") || window.location.pathname.startsWith("/sources/wechat")) {
    renderWeChatSource();
    return;
  }
  if (window.location.pathname.startsWith("/wcplus-source")) {
    renderWCPlusPage();
    await bootstrapWCPlusSource();
    return;
  }
  if (window.location.pathname.startsWith("/book-knowledge")) {
    renderBookKnowledge();
    await loadBookKnowledge();
    return;
  }

  const bookID = getBookID();
  if (!bookID) {
    renderHome();
    return;
  }
  try {
    const payload = await fetchBook(bookID);
    renderReader(payload);
  } catch (error) {
    renderError(error instanceof Error ? error.message : String(error));
  }
}

boot();
