package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWCPlusSourceListsAccountsArticlesAndContent(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/gzh/list":
			if got := r.URL.Query().Get("offset"); got != "5" {
				t.Fatalf("offset = %q", got)
			}
			if got := r.URL.Query().Get("num"); got != "10" {
				t.Fatalf("num = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"gzhs":[{"biz":"biz-1","nickname":"医学参考","img":"https://example.test/account.png","alias":"med-ref","desc":"医学知识","article_count":2}],"total":1}}`)
		case "/api/report/gzh_articles":
			if got := r.URL.Query().Get("biz"); got != "biz-1" {
				t.Fatalf("biz = %q", got)
			}
			if got := r.URL.Query().Get("sort"); got != "p_date" {
				t.Fatalf("default sort = %q", got)
			}
			if got := r.URL.Query().Get("direction"); got != "desc" {
				t.Fatalf("default direction = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-1","nickname":"医学参考","Img":"https://example.test/account.png"},"articles":[{"id":"wx-1","title":"验证文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/wx1","digest":"摘要","publish_time":"2026-07-06"}],"total":1}}`)
		case "/api/article/content":
			if got := r.URL.Query().Get("nickname"); got != "医学参考" {
				t.Fatalf("nickname = %q", got)
			}
			if got := r.URL.Query().Get("id"); got != "wx-1" {
				t.Fatalf("id = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"id":"wx-1","title":"验证文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/wx1","content":"# 验证文章\n\n指标交叉验证。","publish_time":"2026-07-06"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})

	accounts, err := service.ListAccounts(context.Background(), WCPlusListOptions{Offset: 5, Num: 10})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Biz != "biz-1" || accounts.Accounts[0].ImageURL != "https://example.test/account.png" || accounts.Total != 1 {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}

	articles, err := service.ListAccountArticles(context.Background(), WCPlusArticleListOptions{Biz: "biz-1", Offset: 0, Num: 20})
	if err != nil {
		t.Fatalf("ListAccountArticles returned error: %v", err)
	}
	if len(articles.Articles) != 1 || articles.Articles[0].Title != "验证文章" || articles.Account.Nickname != "医学参考" || articles.Account.ImageURL != "https://example.test/account.png" {
		t.Fatalf("unexpected articles: %#v", articles)
	}

	content, err := service.GetArticleContent(context.Background(), "医学参考", "wx-1")
	if err != nil {
		t.Fatalf("GetArticleContent returned error: %v", err)
	}
	if content.Title != "验证文章" || !strings.Contains(content.Content, "指标交叉验证") {
		t.Fatalf("unexpected content: %#v", content)
	}
}

func TestWCPlusSourceAcceptsFlexibleListShapes(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/gzh/list":
			switch r.URL.Query().Get("q") {
			case "items":
				fmt.Fprint(w, `{"success":true,"data":{"items":[{"biz":"biz-items","nickname":"Items 公众号"}],"total":1}}`)
			default:
				fmt.Fprint(w, `{"success":true,"data":[{"biz":"biz-array","nickname":"Array 公众号"}]}`)
			}
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"list":[{"id":"wx-list","title":"List 文章"}],"total":1}}`)
		case "/api/task/all":
			fmt.Fprint(w, `{"success":true,"data":{"items":[{"task_id":"task-items","status":"ready"}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	accounts, err := service.ListAccounts(context.Background(), WCPlusListOptions{Query: "array"})
	if err != nil {
		t.Fatalf("ListAccounts array returned error: %v", err)
	}
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Biz != "biz-array" {
		t.Fatalf("unexpected array accounts: %#v", accounts)
	}

	accounts, err = service.ListAccounts(context.Background(), WCPlusListOptions{Query: "items"})
	if err != nil {
		t.Fatalf("ListAccounts items returned error: %v", err)
	}
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Biz != "biz-items" {
		t.Fatalf("unexpected items accounts: %#v", accounts)
	}

	articles, err := service.ListAccountArticles(context.Background(), WCPlusArticleListOptions{Biz: "biz-list"})
	if err != nil {
		t.Fatalf("ListAccountArticles list returned error: %v", err)
	}
	if len(articles.Articles) != 1 || articles.Articles[0].ID != "wx-list" {
		t.Fatalf("unexpected list articles: %#v", articles)
	}

	tasks, err := service.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks items returned error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskID != "task-items" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestWCPlusSourceNormalizesArticleAliasFields(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"articles":[{"ArticleId":"alias-1","Title":"别名文章","GzhNickname":"别名公众号","link":"https://mp.weixin.qq.com/s/alias","p_date_text":"2026-07-08"}],"total":1}}`)
		case "/api/search/search":
			fmt.Fprint(w, `{"success":true,"data":{"results":[{"appmsgid":"alias-2","title":"搜索别名","source_url":"https://mp.weixin.qq.com/s/search-alias"}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	list, err := service.ListAccountArticles(context.Background(), WCPlusArticleListOptions{Biz: "biz-alias"})
	if err != nil {
		t.Fatalf("ListAccountArticles returned error: %v", err)
	}
	if len(list.Articles) != 1 {
		t.Fatalf("articles length = %d", len(list.Articles))
	}
	article := list.Articles[0]
	if article.ID != "alias-1" || article.Nickname != "别名公众号" || article.URL != "https://mp.weixin.qq.com/s/alias" {
		t.Fatalf("unexpected aliased article: %#v", article)
	}

	payload, err := service.GetJSON(context.Background(), "/api/search/search", mapToValues(map[string]string{"q": "别名"}))
	if err != nil {
		t.Fatalf("GetJSON returned error: %v", err)
	}
	items := wcplusArrayValue(payload, "results", "Results", "articles", "Articles", "items", "Items")
	if len(items) != 1 {
		t.Fatalf("search items length = %d", len(items))
	}
	searchArticle := wcplusArticleFromAny(items[0])
	if searchArticle.ID != "alias-2" || searchArticle.URL != "https://mp.weixin.qq.com/s/search-alias" {
		t.Fatalf("unexpected search alias article: %#v", searchArticle)
	}
}

func TestWCPlusSourceImportsArticleIntoBookKnowledge(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/article/content":
			fmt.Fprint(w, `{"success":true,"data":{"id":"wx-1","title":"验证文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/wx1","content":"# 验证文章\n\n指标交叉验证。","publish_time":"2026-07-06"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	store := NewBookKnowledgeStore(t.TempDir())
	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	pkg, err := service.ImportArticle(context.Background(), store, WCPlusImportArticleRequest{
		Nickname: "医学参考",
		ID:       "wx-1",
		BookID:   "wcplus-health",
	})
	if err != nil {
		t.Fatalf("ImportArticle returned error: %v", err)
	}
	if pkg.Book.BookID != "wcplus-health" || pkg.Book.Extractor != "wcplus-source-adapter" {
		t.Fatalf("unexpected book metadata: %#v", pkg.Book)
	}
	if len(pkg.Chunks) != 1 || !strings.Contains(pkg.Chunks[0].Text, "指标交叉验证") {
		t.Fatalf("unexpected chunks: %#v", pkg.Chunks)
	}
	if len(pkg.Citations) != 1 || pkg.Citations[0].SourceHTML != "https://mp.weixin.qq.com/s/wx1" {
		t.Fatalf("unexpected citations: %#v", pkg.Citations)
	}
}

func TestWCPlusSourceGetsAndImportsArticleContentByURL(t *testing.T) {
	var sawURLContent bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/article/content":
			if got := r.URL.Query().Get("url"); got != "https://mp.weixin.qq.com/s/url-only" {
				t.Fatalf("url = %q", got)
			}
			if got := r.URL.Query().Get("nickname"); got != "" {
				t.Fatalf("nickname should be empty for URL lookup, got %q", got)
			}
			sawURLContent = true
			fmt.Fprint(w, `{"success":true,"data":{"id":"url-only","title":"URL 文章","nickname":"URL 公众号","url":"https://mp.weixin.qq.com/s/url-only","content":"# URL 文章\n\n只通过链接也能导入。","publish_time":"2026-07-08"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	store := NewBookKnowledgeStore(t.TempDir())
	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	content, err := service.GetArticleContentByURL(context.Background(), "https://mp.weixin.qq.com/s/url-only")
	if err != nil {
		t.Fatalf("GetArticleContentByURL returned error: %v", err)
	}
	if !sawURLContent || content.ID != "url-only" || !strings.Contains(content.Content, "只通过链接") {
		t.Fatalf("unexpected URL content: %#v", content)
	}

	pkg, err := service.ImportArticle(context.Background(), store, WCPlusImportArticleRequest{
		URL:    "https://mp.weixin.qq.com/s/url-only",
		BookID: "wcplus-url-only",
	})
	if err != nil {
		t.Fatalf("ImportArticle URL returned error: %v", err)
	}
	if pkg.Book.BookID != "wcplus-url-only" || !strings.Contains(pkg.Chunks[0].Text, "只通过链接") {
		t.Fatalf("unexpected imported URL package: %#v", pkg)
	}
}

func TestWCPlusSourceImportAccountArticlesFallsBackToURLContent(t *testing.T) {
	var sawURLContent bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-url","nickname":"URL 公众号"},"articles":[{"title":"URL 列表文章","link":"https://mp.weixin.qq.com/s/account-url-only"}],"total":1}}`)
		case "/api/article/content":
			if got := r.URL.Query().Get("url"); got != "https://mp.weixin.qq.com/s/account-url-only" {
				t.Fatalf("url = %q", got)
			}
			if got := r.URL.Query().Get("id"); got != "" {
				t.Fatalf("id should be empty for URL fallback, got %q", got)
			}
			sawURLContent = true
			fmt.Fprint(w, `{"success":true,"data":{"title":"URL 列表文章","nickname":"URL 公众号","url":"https://mp.weixin.qq.com/s/account-url-only","content":"# URL 列表文章\n\n账号批量导入也支持 URL-only。","publish_time":"2026-07-08"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	store := NewBookKnowledgeStore(t.TempDir())
	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	result, err := service.ImportAccountArticles(context.Background(), store, WCPlusImportAccountRequest{
		Biz:          "biz-url",
		Nickname:     "URL 公众号",
		Limit:        1,
		BookIDPrefix: "wcplus-url-account",
	})
	if err != nil {
		t.Fatalf("ImportAccountArticles returned error: %v", err)
	}
	if !sawURLContent {
		t.Fatalf("URL content fallback was not called")
	}
	if result.ImportedCount != 1 || len(result.Errors) != 0 || len(result.Books) != 1 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	saved, err := store.LoadPackage(result.Books[0].BookID)
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !strings.Contains(saved.Chunks[0].Text, "账号批量导入也支持 URL-only") {
		t.Fatalf("unexpected saved chunk: %#v", saved.Chunks)
	}
}

func TestWCPlusSourceImportsRawArticleIntoBookKnowledge(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	service := NewWCPlusSourceService(WCPlusSourceConfig{})

	pkg, err := service.ImportRawArticle(context.Background(), store, WCPlusRawImportRequest{
		Title:       "人工导入文章",
		Nickname:    "医学参考",
		URL:         "https://mp.weixin.qq.com/s/manual",
		Content:     "# 人工导入文章\n\n用指标和来源交叉验证结论。",
		PublishTime: "2026-07-06",
		BookID:      "wcplus-manual-health",
	})
	if err != nil {
		t.Fatalf("ImportRawArticle returned error: %v", err)
	}
	if pkg.Book.BookID != "wcplus-manual-health" || pkg.Book.Title != "人工导入文章" {
		t.Fatalf("unexpected book metadata: %#v", pkg.Book)
	}
	if pkg.Book.Author != "医学参考" || pkg.Book.SourceHTML != "https://mp.weixin.qq.com/s/manual" {
		t.Fatalf("unexpected book source: %#v", pkg.Book)
	}
	if len(pkg.Chunks) != 1 || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected chunks: %#v", pkg.Chunks)
	}

	saved, err := store.LoadPackage("wcplus-manual-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if saved.Book.Title != "人工导入文章" {
		t.Fatalf("saved title = %q", saved.Book.Title)
	}

	if _, err := service.ImportRawArticle(context.Background(), store, WCPlusRawImportRequest{
		Title:   "缺正文",
		Content: "   ",
	}); err == nil || !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("missing content error = %v", err)
	}
}

func TestWCPlusSourceCreatesAndControlsTasks(t *testing.T) {
	var sawCreate bool
	var sawControl bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/task/all":
			fmt.Fprint(w, `{"success":true,"data":{"tasks":[{"task_id":"task-1","biz":"biz-1","nickname":"医学参考","crawlerType":"article","status":"running","status_error":"waiting for parameter","status_article_total_amount":30,"status_article_finished_amount":12,"status_reading_data_total_amount":20,"status_reading_data_finished_amount":5,"created_at":100,"updated_at":200}]}}`)
		case "/api/task/new":
			sawCreate = true
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if payload["biz"] != "biz-1" {
				t.Fatalf("create body = %#v", payload)
			}
			if payload["crawlerType"] != "gzh_article_link" {
				t.Fatalf("crawlerType = %#v, want default gzh_article_link", payload["crawlerType"])
			}
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-2","status":"created"}}`)
		case "/api/task/control":
			sawControl = true
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode control body: %v", err)
			}
			if payload["task_id"] != "task-2" || payload["action"] != "start" {
				t.Fatalf("control body = %#v", payload)
			}
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-2","status":"running"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	tasks, err := service.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks returned error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskID != "task-1" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
	if tasks[0].CrawlerType != "article" || tasks[0].StatusError != "waiting for parameter" {
		t.Fatalf("task details were not normalized: %#v", tasks[0])
	}
	if tasks[0].ArticleTotal != 30 || tasks[0].ArticleFinished != 12 || tasks[0].ReadingTotal != 20 || tasks[0].ReadingFinished != 5 {
		t.Fatalf("task progress was not normalized: %#v", tasks[0])
	}
	if tasks[0].CreatedAt != 100 || tasks[0].UpdatedAt != 200 {
		t.Fatalf("task timestamps were not normalized: %#v", tasks[0])
	}
	created, err := service.CreateTask(context.Background(), WCPlusTaskRequest{Biz: "biz-1", Nickname: "医学参考"})
	if err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	if !sawCreate || created.TaskID != "task-2" {
		t.Fatalf("unexpected create result: %#v", created)
	}
	controlled, err := service.ControlTask(context.Background(), WCPlusTaskControlRequest{TaskID: "task-2", Action: "start"})
	if err != nil {
		t.Fatalf("ControlTask returned error: %v", err)
	}
	if !sawControl || controlled.Status != "running" {
		t.Fatalf("unexpected control result: %#v", controlled)
	}
}

func TestWCPlusSourceParsesRealAccountListShape(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/gzh/list" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("num") != "1" || r.URL.Query().Get("offset") != "0" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, `{
			"gzh":[{
				"Biz":"biz-live-shape",
				"Nickname":"脱敏账号",
				"LocalArticleNum":12,
				"TotalArticleNum":34
			}],
			"articles":12,
			"num":1,
			"offset":0,
			"total":2
		}`)
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	list, err := service.ListAccounts(context.Background(), WCPlusListOptions{Num: 1})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if list.Total != 2 || len(list.Accounts) != 1 {
		t.Fatalf("unexpected account list: %#v", list)
	}
	if list.Accounts[0].Biz != "biz-live-shape" || list.Accounts[0].Nickname != "脱敏账号" {
		t.Fatalf("unexpected account: %#v", list.Accounts[0])
	}
}

func TestWCPlusSourceCreatesTypedTasks(t *testing.T) {
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/task/new":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-typed","status":"created"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	task, err := service.CreateTask(context.Background(), WCPlusTaskRequest{
		Biz:               "biz-1",
		Nickname:          "医学参考",
		CrawlerType:       "article",
		ArticleListType:   "amount",
		ArticleListAmount: 30,
	})
	if err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	if task.TaskID != "task-typed" {
		t.Fatalf("unexpected task: %#v", task)
	}
	if payload["crawlerType"] != "article" || payload["articleListType"] != "amount" || payload["articleListAmount"] != float64(30) {
		t.Fatalf("typed task payload = %#v", payload)
	}
}

func TestWCPlusSourceCreatesCompleteArticleTask(t *testing.T) {
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.URL.Path != "/api/task/new" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-article","status":"created"}}`)
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	_, err := service.CreateTask(context.Background(), WCPlusTaskRequest{
		Biz:                  "biz-article",
		Nickname:             "医学参考",
		ImageURL:             "https://example.test/avatar.png",
		CrawlerType:          "article",
		ArticleRefresh:       true,
		ArticleImageDownload: true,
	})
	if err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	if payload["img"] != "https://example.test/avatar.png" || payload["articleRefresh"] != true || payload["articleImgDownload"] != true {
		t.Fatalf("article task payload = %#v", payload)
	}
}

func TestWCPlusSourceCreatesCompleteReadingDataTask(t *testing.T) {
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.URL.Path != "/api/task/new" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-reading","status":"created"}}`)
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	_, err := service.CreateTask(context.Background(), WCPlusTaskRequest{
		Biz:                  "biz-reading",
		Nickname:             "医学参考",
		CrawlerType:          "reading_data",
		ReadingDataType:      "date",
		ReadingDataStartDate: 1000,
		ReadingDataEndDate:   2000,
		ReadingDataAmount:    50,
		ReadingDataOnlyMain:  true,
		ReadingDataRefresh:   true,
	})
	if err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	if payload["readingDataType"] != "date" || payload["readingDataStartDate"] != float64(1000) || payload["readingDataEndDate"] != float64(2000) {
		t.Fatalf("reading task date payload = %#v", payload)
	}
	if payload["readingDataAmount"] != float64(50) || payload["readingDataOnlyMain"] != true || payload["readingDataRefresh"] != true {
		t.Fatalf("reading task options payload = %#v", payload)
	}
}

func TestWCPlusSourceProxiesSearchReportsExportsAndBatchTasks(t *testing.T) {
	var sawXLSXExport bool
	var sawBatchCreate bool
	var sawBatchDelete bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><head><title>wcplusPro 9.483</title></head></html>`)
		case "/api/gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if got := r.URL.Query().Get("q"); got != "医学" {
				t.Fatalf("gzh search q = %q", got)
			}
			fmt.Fprint(w, `{"Gzhs":[{"Biz":"biz-2","Nickname":"医学参考"}],"Total":1}`)
		case "/api/search_gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-3","Nickname":"候选公众号"}],"Total":1}`)
		case "/api/article/all_articles":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-2","Title":"全库文章"}],"Total":1}`)
		case "/api/article/search_title":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-3","Title":"标题命中"}],"Total":1}`)
		case "/api/search/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Results":[{"ID":"wx-4","Title":"全文命中"}],"Total":1}`)
		case "/api/report/reading_data":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Rows":[{"date":"2026-07-06","read_num":42}]}`)
		case "/api/report/statistic_data":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"total_read":42}`)
		case "/api/article/gzh":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Biz":"biz-1","Nickname":"医学参考"}`)
		case "/api/like_article/get_all":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[]}`)
		case "/api/req_data/get_gzh":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Gzh":{"Biz":"biz-1","Nickname":"医学参考"}}`)
		case "/api/article/export_text":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `2`)
		case "/api/gzh/export_csv":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `3`)
		case "/api/batch_task/create_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchCreate = true
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch create body: %v", err)
			}
			if payload["nickname"] != "医学参考" {
				t.Fatalf("batch create body = %#v", payload)
			}
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"batch-1","status":"ready"}}`)
		case "/api/batch_task/delete_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchDelete = true
			fmt.Fprint(w, `{"success":true,"data":{"deleted":2}}`)
		case "/api/article/all_articles/export_xlsx":
			sawXLSXExport = true
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode export body: %v", err)
			}
			if payload["range_mode"] != "recent" {
				t.Fatalf("xlsx export body = %#v", payload)
			}
			fmt.Fprint(w, "xlsx-bytes")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.OK || status.StatusCode != http.StatusOK || status.Version != "9.483" {
		t.Fatalf("unexpected status: %#v", status)
	}

	gzhSearch, err := service.GetJSON(context.Background(), "/api/gzh/search", mapToValues(map[string]string{"q": "医学"}))
	if err != nil {
		t.Fatalf("GetJSON gzh search returned error: %v", err)
	}
	if !strings.Contains(fmt.Sprintf("%v", gzhSearch), "医学参考") {
		t.Fatalf("unexpected gzh search result: %#v", gzhSearch)
	}

	for _, path := range []string{
		"/api/search_gzh/search",
		"/api/article/all_articles",
		"/api/article/search_title",
		"/api/search/search",
		"/api/report/reading_data",
		"/api/report/statistic_data",
		"/api/article/gzh",
		"/api/like_article/get_all",
		"/api/req_data/get_gzh",
		"/api/article/export_text",
		"/api/gzh/export_csv",
	} {
		if _, err := service.GetJSON(context.Background(), path, mapToValues(map[string]string{"biz": "biz-1", "nickname": "医学参考"})); err != nil {
			t.Fatalf("GetJSON %s returned error: %v", path, err)
		}
	}

	batch, err := service.PostJSON(context.Background(), "/api/batch_task/create_task", map[string]any{"nickname": "医学参考"})
	if err != nil {
		t.Fatalf("PostJSON batch create returned error: %v", err)
	}
	if !sawBatchCreate || !strings.Contains(fmt.Sprintf("%v", batch), "batch-1") {
		t.Fatalf("unexpected batch create result: %#v", batch)
	}
	deleted, err := service.PostJSON(context.Background(), "/api/batch_task/delete_task", map[string]any{"status": "ready"})
	if err != nil {
		t.Fatalf("PostJSON batch delete returned error: %v", err)
	}
	if !sawBatchDelete || !strings.Contains(fmt.Sprintf("%v", deleted), "deleted") {
		t.Fatalf("unexpected batch delete result: %#v", deleted)
	}

	body, contentType, err := service.PostRaw(context.Background(), "/api/article/all_articles/export_xlsx", map[string]any{
		"range_mode": "recent",
		"recent_num": 10,
		"fields":     []string{"title", "content"},
	})
	if err != nil {
		t.Fatalf("PostRaw export returned error: %v", err)
	}
	if !sawXLSXExport || string(body) != "xlsx-bytes" || !strings.Contains(contentType, "spreadsheetml") {
		t.Fatalf("unexpected raw export result: body=%q contentType=%q", string(body), contentType)
	}
}

func TestWCPlusSourceConfigFromEnvSupportsWCPlusProBaseURL(t *testing.T) {
	t.Setenv("WCPLUS_BASE_URL", "")
	t.Setenv("WCPLUSPRO_BASE_URL", "http://127.0.0.1:5999")

	cfg := WCPlusSourceConfigFromEnv()
	if cfg.BaseURL != "http://127.0.0.1:5999" {
		t.Fatalf("BaseURL = %q, want WCPLUSPRO_BASE_URL", cfg.BaseURL)
	}

	t.Setenv("WCPLUS_BASE_URL", "http://127.0.0.1:5888")
	cfg = WCPlusSourceConfigFromEnv()
	if cfg.BaseURL != "http://127.0.0.1:5888" {
		t.Fatalf("BaseURL = %q, want WCPLUS_BASE_URL precedence", cfg.BaseURL)
	}
}

func TestWCPlusSourceBatchImportsNicknamesWithExactMatch(t *testing.T) {
	var created []map[string]any
	var queueStarted bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/search_gzh/search":
			keyword := r.URL.Query().Get("keyword")
			if keyword == "" {
				keyword = r.URL.Query().Get("q")
			}
			switch keyword {
			case "医学参考":
				fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-med","Nickname":"医学参考"}],"Total":1}`)
			case "相似账号":
				fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-other","Nickname":"相似账号 Pro"}],"Total":1}`)
			default:
				fmt.Fprint(w, `{"Candidates":[],"Total":0}`)
			}
		case "/api/task/new":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create task body: %v", err)
			}
			created = append(created, payload)
			if payload["crawlerType"] != "gzh_article_link" || payload["articleListType"] != "amount" || payload["articleListAmount"] != float64(20) {
				t.Fatalf("unexpected create task body: %#v", payload)
			}
			fmt.Fprintf(w, `{"success":true,"data":{"task_id":"task-%d","status":"ready"}}`, len(created))
		case "/api/task/control":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode control body: %v", err)
			}
			if payload["command"] != "run" {
				t.Fatalf("control body = %#v", payload)
			}
			queueStarted = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	result, err := service.BatchImportNicknames(context.Background(), WCPlusBatchImportRequest{
		Nicknames:         []string{"医学参考", "医学参考", "相似账号", "不存在"},
		ArticleListType:   "amount",
		ArticleListAmount: 20,
		StartQueue:        true,
		ExactMatch:        true,
	})
	if err != nil {
		t.Fatalf("BatchImportNicknames returned error: %v", err)
	}
	if len(created) != 1 || created[0]["biz"] != "biz-med" || created[0]["nickname"] != "医学参考" {
		t.Fatalf("unexpected created tasks: %#v", created)
	}
	if !queueStarted {
		t.Fatalf("queue was not started")
	}
	if len(result.Success) != 1 || result.Success[0].Nickname != "医学参考" {
		t.Fatalf("unexpected success result: %#v", result.Success)
	}
	if len(result.Failed) != 2 {
		t.Fatalf("unexpected failed result: %#v", result.Failed)
	}
	if !strings.Contains(result.SuccessText, "医学参考") || !strings.Contains(result.FailedText, "相似账号") {
		t.Fatalf("missing text exports: %#v", result)
	}
}

func TestWCPlusSourceBatchImportsNicknamesToKnowledgeAfterSync(t *testing.T) {
	var taskPolls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/search_gzh/search":
			fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-med","Nickname":"医学参考"}],"Total":1}`)
		case "/api/task/new":
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-1","status":"ready"}}`)
		case "/api/task/control":
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		case "/api/task/all":
			taskPolls++
			if taskPolls == 1 {
				fmt.Fprint(w, `{"success":true,"data":{"tasks":[{"task_id":"task-1","biz":"biz-med","nickname":"医学参考","status":"running"}]}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"data":{"tasks":[]}}`)
		case "/api/report/gzh_articles":
			if got := r.URL.Query().Get("biz"); got != "biz-med" {
				t.Fatalf("biz = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-med","nickname":"医学参考"},"articles":[{"title":"同步后文章","link":"https://mp.weixin.qq.com/s/synced"}],"total":1}}`)
		case "/api/article/content":
			if got := r.URL.Query().Get("url"); got != "https://mp.weixin.qq.com/s/synced" {
				t.Fatalf("url = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"title":"同步后文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/synced","content":"# 同步后文章\n\n自动进入书籍知识库。","publish_time":"2026-07-08"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	store := NewBookKnowledgeStore(t.TempDir())
	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	result, err := service.BatchImportNicknamesToKnowledge(context.Background(), store, WCPlusBatchImportRequest{
		Nicknames:          []string{"医学参考"},
		ArticleListType:    "amount",
		ArticleListAmount:  1,
		StartQueue:         true,
		ExactMatch:         true,
		ImportToKBase:      true,
		ImportLimit:        1,
		WaitForCompletion:  true,
		PollAttempts:       2,
		PollIntervalMillis: int((time.Millisecond).Milliseconds()),
		BookIDPrefix:       "wcplus-batch",
	})
	if err != nil {
		t.Fatalf("BatchImportNicknamesToKnowledge returned error: %v", err)
	}
	if taskPolls != 2 {
		t.Fatalf("taskPolls = %d, want 2", taskPolls)
	}
	if result.ImportedCount != 1 || len(result.ImportedBooks) != 1 || len(result.ImportErrors) != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	saved, err := store.LoadPackage(result.ImportedBooks[0].BookID)
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !strings.Contains(saved.Chunks[0].Text, "自动进入书籍知识库") {
		t.Fatalf("unexpected saved chunk: %#v", saved.Chunks)
	}
}

func TestWCPlusSourceDoesNotCompleteDisappearedTaskWithoutArticleVerification(t *testing.T) {
	var taskPolls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/search_gzh/search":
			fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-empty","Nickname":"空公众号"}],"Total":1}`)
		case "/api/task/new":
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-empty","status":"ready"}}`)
		case "/api/task/control":
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		case "/api/task/all":
			taskPolls++
			if taskPolls == 1 {
				fmt.Fprint(w, `{"success":true,"data":{"tasks":[{"task_id":"task-empty","status":"running"}]}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"data":{"tasks":[]}}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-empty","nickname":"空公众号"},"articles":[],"total":0}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	store := NewBookKnowledgeStore(t.TempDir())
	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	result, err := service.BatchImportNicknamesToKnowledge(context.Background(), store, WCPlusBatchImportRequest{
		Nicknames:          []string{"空公众号"},
		StartQueue:         true,
		ExactMatch:         true,
		ImportToKBase:      true,
		WaitForCompletion:  true,
		PollAttempts:       2,
		PollIntervalMillis: 1,
	})
	if err != nil {
		t.Fatalf("BatchImportNicknamesToKnowledge returned error: %v", err)
	}
	if result.ImportedCount != 0 {
		t.Fatalf("ImportedCount = %d, want 0", result.ImportedCount)
	}
	if len(result.ImportErrors) != 1 || !strings.Contains(result.ImportErrors[0], "outcome could not be verified") {
		t.Fatalf("ImportErrors = %#v", result.ImportErrors)
	}
}

func TestWCPlusSourceChecksEnvironment(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/gzh/list":
			fmt.Fprint(w, `{"Gzhs":[],"Total":0}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL})
	result := service.CheckEnvironment(context.Background())
	if !result.OK || len(result.Checks) != 2 {
		t.Fatalf("unexpected env check result: %#v", result)
	}
	if result.BaseURL != apiServer.URL {
		t.Fatalf("BaseURL = %q, want %q", result.BaseURL, apiServer.URL)
	}
}

func mapToValues(values map[string]string) url.Values {
	query := url.Values{}
	for key, value := range values {
		query.Set(key, value)
	}
	return query
}
