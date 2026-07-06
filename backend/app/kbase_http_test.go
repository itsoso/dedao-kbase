package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKBaseHTTPHandlerRequiresBearerTokenForAPI(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "wrong-token")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status with wrong token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("status with correct token = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"42"`) {
		t.Fatalf("books response missing sample book: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerServesSearchAndSystemKBExport(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	exportPath := filepath.Join(root, "artifacts", "system_kb_export.json")
	if err := os.MkdirAll(filepath.Dir(exportPath), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	exportPayload := map[string]any{
		"type":        "system_kb_v2_export",
		"schema_id":   "llm-wiki-v2-system-kb-export",
		"version":     "test-version",
		"source":      "dedao-kbase",
		"compiled_at": "2026-06-27T10:00:00Z",
		"stats":       map[string]any{"claim_count": 1},
		"pages":       []any{},
		"entities":    []any{},
		"claims":      []any{},
		"relations":   []any{},
	}
	data, err := json.Marshal(exportPayload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := os.WriteFile(exportPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:              store,
		AuthToken:          "secret-token",
		SystemKBExportPath: exportPath,
	})

	searchResp := requestKBase(handler, http.MethodGet, "/api/search?q=MACD&limit=5", "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), `"results"`) || !strings.Contains(searchResp.Body.String(), `"42"`) {
		t.Fatalf("search response missing results: %s", searchResp.Body.String())
	}

	manifestResp := requestKBase(handler, http.MethodGet, "/api/system-kb/manifest", "secret-token")
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"version":"test-version"`) {
		t.Fatalf("manifest response missing version: %s", manifestResp.Body.String())
	}

	exportResp := requestKBase(handler, http.MethodGet, "/api/system-kb/export", "secret-token")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export status = %d, body=%s", exportResp.Code, exportResp.Body.String())
	}
	if !strings.Contains(exportResp.Body.String(), `"type":"system_kb_v2_export"`) {
		t.Fatalf("export response missing payload: %s", exportResp.Body.String())
	}
}

func TestKBaseHTTPHandlerServesWebAssets(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	webDir := filepath.Join(root, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<main class="reader-loading">reader</main>`), 0o644); err != nil {
		t.Fatalf("WriteFile index returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte(`console.log("reader")`), 0o644); err != nil {
		t.Fatalf("WriteFile asset returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		StaticDir: webDir,
	})

	indexResp := requestKBase(handler, http.MethodGet, "/", "")
	if indexResp.Code != http.StatusOK {
		t.Fatalf("index status = %d, body=%s", indexResp.Code, indexResp.Body.String())
	}
	if !strings.Contains(indexResp.Body.String(), `reader-loading`) {
		t.Fatalf("index response missing reader shell: %s", indexResp.Body.String())
	}

	assetResp := requestKBase(handler, http.MethodGet, "/assets/app.js", "")
	if assetResp.Code != http.StatusOK {
		t.Fatalf("asset status = %d, body=%s", assetResp.Code, assetResp.Body.String())
	}
	if !strings.Contains(assetResp.Body.String(), `console.log`) {
		t.Fatalf("asset response missing script: %s", assetResp.Body.String())
	}

	readerRouteResp := requestKBase(handler, http.MethodGet, "/ebook/42", "")
	if readerRouteResp.Code != http.StatusOK {
		t.Fatalf("reader route status = %d, body=%s", readerRouteResp.Code, readerRouteResp.Body.String())
	}
	if !strings.Contains(readerRouteResp.Body.String(), `reader-loading`) {
		t.Fatalf("reader route did not fall back to index: %s", readerRouteResp.Body.String())
	}

	missingAssetResp := requestKBase(handler, http.MethodGet, "/assets/missing.js", "")
	if missingAssetResp.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missingAssetResp.Code)
	}

	apiResp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if apiResp.Code != http.StatusUnauthorized {
		t.Fatalf("api status without token = %d, want 401", apiResp.Code)
	}
}

func TestKBaseHTTPHandlerImportsWeChatArticleIntoBookKnowledge(t *testing.T) {
	articleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
  <body>
    <h1 id="activity-name">健康验证方法</h1>
    <a id="js_name">健康知识</a>
    <em id="publish_time">2026-07-06</em>
    <div id="js_content"><p>用指标和来源交叉验证结论。</p></div>
  </body>
</html>`)
	}))
	defer articleServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WeChat:    NewWeChatSourceService(WeChatSourceConfig{}),
	})

	body := bytes.NewBufferString(`{"url":"` + articleServer.URL + `/s/test","book_id":"wechat-health"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/import", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"wechat-health"`) {
		t.Fatalf("import response missing book id: %s", resp.Body.String())
	}

	pkg, err := store.LoadPackage("wechat-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Title != "健康验证方法" {
		t.Fatalf("book title = %q", pkg.Book.Title)
	}
	if len(pkg.Chunks) != 1 || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected chunks: %#v", pkg.Chunks)
	}
	if len(pkg.Citations) != 1 || pkg.Citations[0].SourceHTML != articleServer.URL+"/s/test" {
		t.Fatalf("unexpected citations: %#v", pkg.Citations)
	}
}

func TestKBaseHTTPHandlerProxiesAndImportsWCPlusArticles(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/gzh/list":
			fmt.Fprint(w, `{"success":true,"data":{"gzhs":[{"biz":"biz-1","nickname":"医学参考","article_count":2}],"total":1}}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-1","nickname":"医学参考"},"articles":[{"id":"wx-1","title":"验证文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/wx1","digest":"摘要","publish_time":"2026-07-06"}],"total":1}}`)
		case "/api/article/content":
			fmt.Fprintf(w, `{"success":true,"data":{"id":"%s","title":"验证文章 %s","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/%s","content":"# 验证文章\n\n指标交叉验证。","publish_time":"2026-07-06"}}`, r.URL.Query().Get("id"), r.URL.Query().Get("id"), r.URL.Query().Get("id"))
		case "/api/task/all":
			fmt.Fprint(w, `{"success":true,"data":{"tasks":[{"task_id":"task-1","biz":"biz-1","nickname":"医学参考","status":"running"}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	listResp := requestKBase(handler, http.MethodGet, "/api/wcplus/gzh/list?offset=0&num=10", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("gzh list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"biz":"biz-1"`) {
		t.Fatalf("gzh list response missing account: %s", listResp.Body.String())
	}

	contentResp := requestKBase(handler, http.MethodGet, "/api/wcplus/article/content?nickname="+url.QueryEscape("医学参考")+"&id=wx-1", "secret-token")
	if contentResp.Code != http.StatusOK {
		t.Fatalf("content status = %d, body=%s", contentResp.Code, contentResp.Body.String())
	}
	if !strings.Contains(contentResp.Body.String(), `"content"`) || !strings.Contains(contentResp.Body.String(), "指标交叉验证") {
		t.Fatalf("content response missing article content: %s", contentResp.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/article", bytes.NewBufferString(`{"nickname":"医学参考","id":"wx-1","book_id":"wcplus-health"}`))
	importReq.Header.Set("Authorization", "Bearer secret-token")
	importResp := httptest.NewRecorder()
	handler.ServeHTTP(importResp, importReq)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", importResp.Code, importResp.Body.String())
	}
	pkg, err := store.LoadPackage("wcplus-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Extractor != "wcplus-source-adapter" || !strings.Contains(pkg.Chunks[0].Text, "指标交叉验证") {
		t.Fatalf("unexpected imported package: %#v", pkg)
	}

	batchReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/account", bytes.NewBufferString(`{"biz":"biz-1","nickname":"医学参考","limit":1}`))
	batchReq.Header.Set("Authorization", "Bearer secret-token")
	batchResp := httptest.NewRecorder()
	handler.ServeHTTP(batchResp, batchReq)
	if batchResp.Code != http.StatusOK {
		t.Fatalf("batch import status = %d, body=%s", batchResp.Code, batchResp.Body.String())
	}
	if !strings.Contains(batchResp.Body.String(), `"imported_count":1`) {
		t.Fatalf("batch import response missing count: %s", batchResp.Body.String())
	}

	taskResp := requestKBase(handler, http.MethodGet, "/api/wcplus/task/all", "secret-token")
	if taskResp.Code != http.StatusOK {
		t.Fatalf("task status = %d, body=%s", taskResp.Code, taskResp.Body.String())
	}
	if !strings.Contains(taskResp.Body.String(), `"task_id":"task-1"`) {
		t.Fatalf("task response missing task: %s", taskResp.Body.String())
	}
}

func TestKBaseHTTPHandlerImportsRawWCPlusArticle(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/raw", bytes.NewBufferString(`{
		"title":"人工导入文章",
		"nickname":"医学参考",
		"url":"https://mp.weixin.qq.com/s/manual",
		"content":"# 人工导入文章\n\n用指标和来源交叉验证结论。",
		"book_id":"wcplus-manual-health"
	}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("raw import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"wcplus-manual-health"`) {
		t.Fatalf("raw import response missing book id: %s", resp.Body.String())
	}

	pkg, err := store.LoadPackage("wcplus-manual-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Extractor != "wcplus-source-adapter" || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected imported package: %#v", pkg)
	}
}

func TestKBaseHTTPHandlerProxiesAdvancedWCPlusAPIs(t *testing.T) {
	var sawQueueRun bool
	var sawBatchCreate bool
	var sawBatchDelete bool
	var sawXLSXExport bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/search/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if got := r.URL.Query().Get("q"); got != "血压" {
				t.Fatalf("search q = %q", got)
			}
			fmt.Fprint(w, `{"Results":[{"ID":"wx-1","Title":"血压验证"}],"Total":1}`)
		case "/api/gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Gzhs":[{"Biz":"biz-1","Nickname":"医学参考"}],"Total":1}`)
		case "/api/search_gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-2","Nickname":"候选公众号"}],"Total":1}`)
		case "/api/article/search_title":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-2","Title":"标题搜索"}],"Total":1}`)
		case "/api/article/all_articles":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-3","Title":"全库文章"}],"Total":1}`)
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
		case "/api/task/control":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode task control body: %v", err)
			}
			if payload["command"] != "run" {
				t.Fatalf("task control body = %#v", payload)
			}
			sawQueueRun = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		case "/api/batch_task/create_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchCreate = true
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"batch-1","status":"ready"}}`)
		case "/api/batch_task/delete_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchDelete = true
			fmt.Fprint(w, `{"success":true,"data":{"deleted":1}}`)
		case "/api/article/all_articles/export_xlsx":
			sawXLSXExport = true
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			fmt.Fprint(w, "xlsx-bytes")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	statusResp := requestKBase(handler, http.MethodGet, "/api/wcplus/status", "secret-token")
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status status = %d, body=%s", statusResp.Code, statusResp.Body.String())
	}
	if !strings.Contains(statusResp.Body.String(), `"ok":true`) {
		t.Fatalf("status response missing ok: %s", statusResp.Body.String())
	}

	searchResp := requestKBase(handler, http.MethodGet, "/api/wcplus/search?q="+url.QueryEscape("血压"), "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), "血压验证") {
		t.Fatalf("search response missing result: %s", searchResp.Body.String())
	}

	for _, path := range []string{
		"/api/wcplus/gzh/search?q=test",
		"/api/wcplus/search-gzh?q=test",
		"/api/wcplus/article/search-title?q=test",
		"/api/wcplus/article/all?offset=0&num=10",
		"/api/wcplus/report/reading-data?biz=biz-1",
		"/api/wcplus/report/statistic-data?biz=biz-1",
		"/api/wcplus/article/gzh?id=wx-1",
		"/api/wcplus/like-articles?offset=0&num=10",
		"/api/wcplus/request/gzh?biz=biz-1",
		"/api/wcplus/export/text?biz=biz-1&nickname=test",
		"/api/wcplus/export/gzh-csv?biz=biz-1&nickname=test",
	} {
		resp := requestKBase(handler, http.MethodGet, path, "secret-token")
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, resp.Code, resp.Body.String())
		}
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/task/control", bytes.NewBufferString(`{"command":"run"}`))
	queueReq.Header.Set("Authorization", "Bearer secret-token")
	queueResp := httptest.NewRecorder()
	handler.ServeHTTP(queueResp, queueReq)
	if queueResp.Code != http.StatusOK || !sawQueueRun {
		t.Fatalf("queue run status = %d, body=%s", queueResp.Code, queueResp.Body.String())
	}

	batchCreateReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-task/create", bytes.NewBufferString(`{"nickname":"医学参考"}`))
	batchCreateReq.Header.Set("Authorization", "Bearer secret-token")
	batchCreateResp := httptest.NewRecorder()
	handler.ServeHTTP(batchCreateResp, batchCreateReq)
	if batchCreateResp.Code != http.StatusOK || !sawBatchCreate {
		t.Fatalf("batch create status = %d, body=%s", batchCreateResp.Code, batchCreateResp.Body.String())
	}

	batchDeleteReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-task/delete", bytes.NewBufferString(`{"status":"ready"}`))
	batchDeleteReq.Header.Set("Authorization", "Bearer secret-token")
	batchDeleteResp := httptest.NewRecorder()
	handler.ServeHTTP(batchDeleteResp, batchDeleteReq)
	if batchDeleteResp.Code != http.StatusOK || !sawBatchDelete {
		t.Fatalf("batch delete status = %d, body=%s", batchDeleteResp.Code, batchDeleteResp.Body.String())
	}

	xlsxReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/export/all-articles-xlsx", bytes.NewBufferString(`{"range_mode":"recent","recent_num":10,"fields":["title"]}`))
	xlsxReq.Header.Set("Authorization", "Bearer secret-token")
	xlsxResp := httptest.NewRecorder()
	handler.ServeHTTP(xlsxResp, xlsxReq)
	if xlsxResp.Code != http.StatusOK || !sawXLSXExport || xlsxResp.Body.String() != "xlsx-bytes" {
		t.Fatalf("xlsx export status = %d, body=%q", xlsxResp.Code, xlsxResp.Body.String())
	}
	if got := xlsxResp.Header().Get("Content-Type"); !strings.Contains(got, "spreadsheetml") {
		t.Fatalf("xlsx content type = %q", got)
	}
}

func TestKBaseHTTPHandlerChecksEnvAndBatchImportsWCPlusNicknames(t *testing.T) {
	var created []map[string]any
	var queueStarted bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/gzh/list":
			fmt.Fprint(w, `{"Gzhs":[],"Total":0}`)
		case "/api/search_gzh/search":
			keyword := r.URL.Query().Get("keyword")
			if keyword == "" {
				keyword = r.URL.Query().Get("q")
			}
			switch keyword {
			case "医学参考":
				fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-med","Nickname":"医学参考"}],"Total":1}`)
			default:
				fmt.Fprint(w, `{"Candidates":[],"Total":0}`)
			}
		case "/api/batch_task/create_task":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create task body: %v", err)
			}
			created = append(created, payload)
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"batch-1","status":"ready"}}`)
		case "/api/task/control":
			queueStarted = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	envResp := requestKBase(handler, http.MethodGet, "/api/wcplus/env/check", "secret-token")
	if envResp.Code != http.StatusOK {
		t.Fatalf("env check status = %d, body=%s", envResp.Code, envResp.Body.String())
	}
	if !strings.Contains(envResp.Body.String(), `"ok":true`) || !strings.Contains(envResp.Body.String(), `"gzh_list"`) {
		t.Fatalf("env check response missing details: %s", envResp.Body.String())
	}

	body := `{"nicknames":["医学参考","不存在"],"articleListType":"amount","articleListAmount":20,"start_queue":true,"exact_match":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-import/gzh", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if len(created) != 1 || created[0]["crawlerType"] != "gzh_article_link" {
		t.Fatalf("unexpected created tasks: %#v", created)
	}
	if !queueStarted {
		t.Fatalf("queue was not started")
	}
	if !strings.Contains(resp.Body.String(), `"success"`) || !strings.Contains(resp.Body.String(), `"failed"`) {
		t.Fatalf("batch import response missing lists: %s", resp.Body.String())
	}
}

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
