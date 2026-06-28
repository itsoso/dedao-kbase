package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestKBaseHTTPHandlerListsBooksWithPagination(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for i, title := range []string{"Book Alpha", "Book Beta", "Book Gamma"} {
		pkg := sampleBookKnowledgePackageWithID(fmt.Sprintf("book-%d", i+1), title)
		if err := store.SavePackage(pkg); err != nil {
			t.Fatalf("SavePackage returned error: %v", err)
		}
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books?page=2&page_size=1&q=Book&sort=title_asc", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("books status = %d, body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Books      []BookKnowledgeBook `json:"books"`
		Page       int                 `json:"page"`
		PageSize   int                 `json:"page_size"`
		Total      int                 `json:"total"`
		TotalPages int                 `json:"total_pages"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 2 || payload.PageSize != 1 || payload.Total != 3 || payload.TotalPages != 3 {
		t.Fatalf("pagination payload = %#v", payload)
	}
	if len(payload.Books) != 1 || payload.Books[0].BookID != "book-2" {
		t.Fatalf("books page = %#v, want second title-sorted book", payload.Books)
	}
}

func TestKBaseHTTPHandlerServesBookPromptsChatAndHistory(t *testing.T) {
	var gotTokenPlanAuth string
	tokenPlanServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokenPlanAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/compatible-mode/v1/chat/completions" {
			t.Fatalf("TokenPlan path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Web 对话回答"}}]}`))
	}))
	defer tokenPlanServer.Close()

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"TOKENPLAN_API_KEY=sk-web-test",
		"TOKENPLAN_BASE_URL=" + tokenPlanServer.URL + "/compatible-mode/v1",
		"TOKENPLAN_MODEL=MiniMax-M2.5",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "")
	t.Setenv("TOKENPLAN_API_KEY", "")
	t.Setenv("TOKENPLAN_BASE_URL", "")
	t.Setenv("TOKENPLAN_MODEL", "")
	t.Setenv("DEDAO_TOKENPLAN_ENV_FILE", envFile)

	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	promptsResp := requestKBase(handler, http.MethodGet, "/api/books/42/prompts", "secret-token")
	if promptsResp.Code != http.StatusOK {
		t.Fatalf("prompts status = %d, body=%s", promptsResp.Code, promptsResp.Body.String())
	}
	if !strings.Contains(promptsResp.Body.String(), `"prompts"`) ||
		!strings.Contains(promptsResp.Body.String(), `"understand-core"`) {
		t.Fatalf("prompts response missing templates: %s", promptsResp.Body.String())
	}

	chatResp := requestKBaseJSON(handler, http.MethodPost, "/api/books/42/chat", "secret-token", `{"mode":"chat","question":"如何理解 MACD？","model":"qwen3.7-max"}`)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"answer":"Web 对话回答"`) ||
		!strings.Contains(chatResp.Body.String(), `"model":"qwen3.7-max"`) ||
		!strings.Contains(chatResp.Body.String(), `"sources"`) ||
		!strings.Contains(chatResp.Body.String(), `"context_stats"`) {
		t.Fatalf("chat response missing answer metadata: %s", chatResp.Body.String())
	}
	if gotTokenPlanAuth != "Bearer sk-web-test" {
		t.Fatalf("TokenPlan auth = %q, want fake bearer", gotTokenPlanAuth)
	}

	historyResp := requestKBase(handler, http.MethodGet, "/api/books/42/chat-history?limit=10", "secret-token")
	if historyResp.Code != http.StatusOK {
		t.Fatalf("history status = %d, body=%s", historyResp.Code, historyResp.Body.String())
	}
	if !strings.Contains(historyResp.Body.String(), `"history"`) ||
		!strings.Contains(historyResp.Body.String(), `"Web 对话回答"`) {
		t.Fatalf("history response missing saved chat: %s", historyResp.Body.String())
	}
}

func TestKBaseHTTPHandlerServesPageAnalysis(t *testing.T) {
	var gotTokenPlanAuth string
	var gotTokenPlanBody string
	tokenPlanServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokenPlanAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/compatible-mode/v1/chat/completions" {
			t.Fatalf("TokenPlan path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotTokenPlanBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"当前页面重点是主动回忆。"}}]}`))
	}))
	defer tokenPlanServer.Close()

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"TOKENPLAN_API_KEY=sk-page-web-test",
		"TOKENPLAN_BASE_URL=" + tokenPlanServer.URL + "/compatible-mode/v1",
		"TOKENPLAN_MODEL=MiniMax-M2.5",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "")
	t.Setenv("TOKENPLAN_API_KEY", "")
	t.Setenv("TOKENPLAN_BASE_URL", "")
	t.Setenv("TOKENPLAN_MODEL", "")
	t.Setenv("DEDAO_TOKENPLAN_ENV_FILE", envFile)

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	resp := requestKBaseJSON(handler, http.MethodPost, "/api/analyze-page", "secret-token", `{
		"source":"course",
		"title":"有效学习",
		"url":"/course/course-enid",
		"mode":"study",
		"question":"分析当前页面",
		"model":"qwen3.7-max",
		"context_sections":[
			{"title":"课程信息","content":"讲师: 王老师"},
			{"title":"当前文章","content":"主动回忆比重复阅读更有效。"}
		]
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("page analysis status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"answer":"当前页面重点是主动回忆。"`) ||
		!strings.Contains(resp.Body.String(), `"model":"qwen3.7-max"`) ||
		!strings.Contains(resp.Body.String(), `"source":"course"`) ||
		!strings.Contains(resp.Body.String(), `"context_stats"`) {
		t.Fatalf("page analysis response missing metadata: %s", resp.Body.String())
	}
	if gotTokenPlanAuth != "Bearer sk-page-web-test" {
		t.Fatalf("TokenPlan auth = %q, want fake bearer", gotTokenPlanAuth)
	}
	if !strings.Contains(gotTokenPlanBody, "有效学习") ||
		!strings.Contains(gotTokenPlanBody, "主动回忆比重复阅读更有效") {
		t.Fatalf("TokenPlan request missing page context: %s", gotTokenPlanBody)
	}
}

func TestKBaseHTTPHandlerServesJobs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	listResp := requestKBase(handler, http.MethodGet, "/api/jobs", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("jobs list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"jobs"`) {
		t.Fatalf("jobs list response missing jobs: %s", listResp.Body.String())
	}

	createResp := requestKBaseJSON(handler, http.MethodPost, "/api/jobs", "secret-token", `{"type":"notebooklm_export","book_id":"42"}`)
	if createResp.Code != http.StatusAccepted {
		t.Fatalf("job create status = %d, body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Job BookKnowledgeJob `json:"job"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal create response returned error: %v", err)
	}
	if created.Job.ID == "" || created.Job.Type != "notebooklm_export" || created.Job.BookID != "42" {
		t.Fatalf("created job = %#v", created.Job)
	}

	var loaded BookKnowledgeJob
	for i := 0; i < 50; i++ {
		getResp := requestKBase(handler, http.MethodGet, "/api/jobs/"+created.Job.ID, "secret-token")
		if getResp.Code != http.StatusOK {
			t.Fatalf("job get status = %d, body=%s", getResp.Code, getResp.Body.String())
		}
		var payload struct {
			Job BookKnowledgeJob `json:"job"`
		}
		if err := json.Unmarshal(getResp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("Unmarshal get response returned error: %v", err)
		}
		loaded = payload.Job
		if loaded.Status == BookKnowledgeJobStatusSucceeded || loaded.Status == BookKnowledgeJobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if loaded.Status != BookKnowledgeJobStatusSucceeded {
		t.Fatalf("job status = %s, error=%s, logs=%v", loaded.Status, loaded.Error, loaded.Logs)
	}
	if !strings.Contains(fmt.Sprint(loaded.Result), "notebooklm-prompt.md") {
		t.Fatalf("job result missing NotebookLM files: %#v", loaded.Result)
	}

	listResp = requestKBase(handler, http.MethodGet, "/api/jobs?limit=10", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("jobs list status after create = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), created.Job.ID) ||
		!strings.Contains(listResp.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("jobs list response missing completed job: %s", listResp.Body.String())
	}

	unauthorizedResp := requestKBaseJSON(handler, http.MethodPost, "/api/jobs", "", `{"type":"notebooklm_export","book_id":"42"}`)
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("job create without bearer = %d, want 401", unauthorizedResp.Code)
	}
}

func TestKBaseHTTPHandlerServesDedaoSession(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/session", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("session without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/session", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("session status = %d, body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"logged_in"`) || !strings.Contains(body, `"user_count"`) {
		t.Fatalf("session response missing safe status fields: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "cookie") {
		t.Fatalf("session response must not expose cookies: %s", body)
	}
}

func TestKBaseHTTPHandlerServesDedaoAuth(t *testing.T) {
	auth := &fakeDedaoAuthProvider{
		qr: DedaoLoginQRCode{
			Token:        "login-token",
			QRCode:       "data:image/png;base64,abc",
			QRCodeString: "qr-string",
		},
		check: DedaoLoginCheck{
			Status: 1,
			User: &DedaoSessionUser{
				UIDHazy: "uid-1",
				Name:    "学习者",
				Avatar:  "https://example.test/avatar.png",
			},
			Session: DedaoSession{
				LoggedIn: true,
				ActiveUser: &DedaoSessionUser{
					UIDHazy: "uid-1",
					Name:    "学习者",
				},
				UserCount: 1,
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		DedaoAuth: auth,
	})

	unauthorizedResp := requestKBase(handler, http.MethodPost, "/api/dedao/auth/qrcode", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("auth qrcode without bearer = %d, want 401", unauthorizedResp.Code)
	}

	qrResp := requestKBase(handler, http.MethodPost, "/api/dedao/auth/qrcode", "secret-token")
	if qrResp.Code != http.StatusOK {
		t.Fatalf("auth qrcode status = %d, body=%s", qrResp.Code, qrResp.Body.String())
	}
	if cacheControl := qrResp.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("auth qrcode Cache-Control = %q, want no-store", cacheControl)
	}
	qrBody := qrResp.Body.String()
	if !strings.Contains(qrBody, `"token":"login-token"`) ||
		!strings.Contains(qrBody, `"qr_code":"data:image/png;base64,abc"`) ||
		!strings.Contains(qrBody, `"qr_code_string":"qr-string"`) {
		t.Fatalf("qrcode response missing safe QR payload: %s", qrBody)
	}
	if strings.Contains(strings.ToLower(qrBody), "cookie") {
		t.Fatalf("qrcode response must not expose cookies: %s", qrBody)
	}

	checkResp := requestKBaseJSON(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{"token":"login-token","qr_code_string":"qr-string"}`)
	if checkResp.Code != http.StatusOK {
		t.Fatalf("auth check status = %d, body=%s", checkResp.Code, checkResp.Body.String())
	}
	if cacheControl := checkResp.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("auth check Cache-Control = %q, want no-store", cacheControl)
	}
	checkBody := checkResp.Body.String()
	if auth.gotToken != "login-token" || auth.gotQRCodeString != "qr-string" {
		t.Fatalf("auth check arguments = token %q qr %q", auth.gotToken, auth.gotQRCodeString)
	}
	if !strings.Contains(checkBody, `"status":1`) ||
		!strings.Contains(checkBody, `"uid_hazy":"uid-1"`) ||
		!strings.Contains(checkBody, `"session"`) {
		t.Fatalf("auth check response missing safe status/user/session: %s", checkBody)
	}
	if strings.Contains(strings.ToLower(checkBody), "cookie") {
		t.Fatalf("auth check response must not expose cookies: %s", checkBody)
	}

	badResp := requestKBaseJSON(handler, http.MethodPost, "/api/dedao/auth/check", "secret-token", `{"token":"","qr_code_string":""}`)
	if badResp.Code != http.StatusBadRequest {
		t.Fatalf("auth check missing token status = %d, want 400", badResp.Code)
	}
}

func TestKBaseHTTPHandlerServesDedaoEbooks(t *testing.T) {
	content := &fakeDedaoContentProvider{
		ebooks: DedaoEbookPage{
			Ebooks: []DedaoEbook{
				{
					ID:         67929,
					Enid:       "ebook-enid",
					Title:      "强化学习教程",
					Author:     "王琦",
					Intro:      "适合系统学习强化学习的电子书。",
					Icon:       "https://example.test/cover.jpg",
					Price:      "99.00",
					Progress:   42,
					PublishNum: 18,
					LastRead:   "第 3 章",
				},
			},
			Page:       2,
			PageSize:   1,
			Total:      3,
			TotalPages: 3,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("ebooks without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks?page=2&page_size=1&q=强化学习", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("ebooks status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotPage != 2 || content.gotPageSize != 1 || content.gotQuery != "强化学习" {
		t.Fatalf("ebooks request args = page %d pageSize %d query %q", content.gotPage, content.gotPageSize, content.gotQuery)
	}
	var payload DedaoEbookPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 2 || payload.PageSize != 1 || payload.Total != 3 || payload.TotalPages != 3 || payload.IsMore != 1 {
		t.Fatalf("ebooks pagination = %#v", payload)
	}
	if len(payload.Ebooks) != 1 || payload.Ebooks[0].Title != "强化学习教程" || payload.Ebooks[0].Enid != "ebook-enid" {
		t.Fatalf("ebooks payload = %#v", payload.Ebooks)
	}
	body := strings.ToLower(resp.Body.String())
	for _, forbidden := range []string{"cookie", "drm_token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("ebooks response must not expose %s: %s", forbidden, resp.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerServesDedaoCourses(t *testing.T) {
	content := &fakeDedaoContentProvider{
		courses: DedaoCoursePage{
			Courses: []DedaoCourse{
				{
					ID:         101,
					ClassID:    202,
					Enid:       "course-enid",
					Title:      "商业分析课",
					Intro:      "从案例学习商业分析。",
					Icon:       "https://example.test/course.jpg",
					Price:      "199.00",
					Progress:   67,
					PublishNum: 36,
					CourseNum:  48,
					LastRead:   "第 12 讲",
				},
			},
			Page:       3,
			PageSize:   1,
			Total:      9,
			TotalPages: 9,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/courses", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("courses without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses?page=3&page_size=1&q=商业", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("courses status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotCoursePage != 3 || content.gotCoursePageSize != 1 || content.gotCourseQuery != "商业" {
		t.Fatalf("courses request args = page %d pageSize %d query %q", content.gotCoursePage, content.gotCoursePageSize, content.gotCourseQuery)
	}
	var payload DedaoCoursePage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 3 || payload.PageSize != 1 || payload.Total != 9 || payload.TotalPages != 9 || payload.IsMore != 1 {
		t.Fatalf("courses pagination = %#v", payload)
	}
	if len(payload.Courses) != 1 || payload.Courses[0].Title != "商业分析课" || payload.Courses[0].ClassID != 202 {
		t.Fatalf("courses payload = %#v", payload.Courses)
	}
	body := strings.ToLower(resp.Body.String())
	for _, forbidden := range []string{"cookie", "drm_token", "dd_url"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("courses response must not expose %s: %s", forbidden, resp.Body.String())
		}
	}
}

func TestKBaseHTTPHandlerServesDedaoOdobs(t *testing.T) {
	content := &fakeDedaoContentProvider{
		odobs: DedaoOdobPage{
			Odobs: []DedaoOdob{
				{
					ID:            301,
					ClassID:       301,
					Enid:          "odob-enid",
					Title:         "每天听本书",
					Intro:         "一本书的精华解读。",
					Author:        "得到听书",
					Icon:          "https://example.test/odob.jpg",
					Price:         "29.90",
					Progress:      55,
					Duration:      1800,
					AudioAliasID:  "audio-alias",
					AudioTitle:    "听书音频",
					AudioPlayURL:  "https://example.test/audio.mp3",
					HasPlayAuth:   true,
					AudioDuration: 1800,
				},
			},
			Page:       2,
			PageSize:   1,
			Total:      4,
			TotalPages: 4,
			IsMore:     1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/odobs", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("odobs without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/odobs?page=2&page_size=1&q=听书", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("odobs status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotOdobPage != 2 || content.gotOdobPageSize != 1 || content.gotOdobQuery != "听书" {
		t.Fatalf("odobs request args = page %d pageSize %d query %q", content.gotOdobPage, content.gotOdobPageSize, content.gotOdobQuery)
	}
	var payload DedaoOdobPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Page != 2 || payload.PageSize != 1 || payload.Total != 4 || payload.TotalPages != 4 || payload.IsMore != 1 {
		t.Fatalf("odobs pagination = %#v", payload)
	}
	if len(payload.Odobs) != 1 || payload.Odobs[0].Title != "每天听本书" || payload.Odobs[0].AudioAliasID != "audio-alias" {
		t.Fatalf("odobs payload = %#v", payload.Odobs)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoOdobDetail(t *testing.T) {
	content := &fakeDedaoContentProvider{
		odobDetail: DedaoOdobDetail{
			Enid:           "odob-enid",
			ID:             301,
			Title:          "每天听本书",
			Icon:           "https://example.test/odob.jpg",
			Duration:       1800,
			AudioPrice:     "29.90",
			AudioSummary:   "一本书的精华解读。",
			IsBuy:          true,
			InBookrack:     true,
			Progress:       55,
			Tags:           []string{"商业", "认知"},
			LearnCountDesc: "1.2 万人学习",
			Agency: DedaoOdobAgency{
				Name:       "得到听书",
				MemberName: "讲书人",
			},
			TopicSummary: []DedaoOdobTopicSummary{
				{Title: "核心观点", SubTitle: "三条主线"},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/odobs/odob-enid", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("odob detail status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotOdobDetailEnid != "odob-enid" {
		t.Fatalf("odob detail enid = %q", content.gotOdobDetailEnid)
	}
	var payload DedaoOdobDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Title != "每天听本书" || payload.Agency.Name != "得到听书" || len(payload.TopicSummary) != 1 {
		t.Fatalf("odob detail payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoCourseDetail(t *testing.T) {
	content := &fakeDedaoContentProvider{
		courseDetail: DedaoCourseDetail{
			Course: DedaoCourseDetailMeta{
				Enid:          "course-enid",
				ID:            101,
				Title:         "商业分析课",
				Intro:         "从案例学习商业分析。",
				Highlight:     "建立结构化分析框架。",
				LecturerName:  "张三",
				LecturerTitle: "商业顾问",
				Logo:          "https://example.test/course.jpg",
				ArticleCount:  36,
			},
			Articles: []DedaoArticle{
				{
					Enid:        "article-enid",
					ID:          501,
					Title:       "第一讲：问题定义",
					Summary:     "先定义问题，再选择工具。",
					PublishTime: 1710000000,
					IsRead:      true,
					OrderNum:    1,
				},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	unauthorizedResp := requestKBase(handler, http.MethodGet, "/api/dedao/courses/course-enid", "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("course detail without bearer = %d, want 401", unauthorizedResp.Code)
	}

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses/course-enid", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("course detail status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotCourseDetailEnid != "course-enid" {
		t.Fatalf("course detail enid = %q", content.gotCourseDetailEnid)
	}
	var payload DedaoCourseDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Course.Title != "商业分析课" || len(payload.Articles) != 1 || payload.Articles[0].Title != "第一讲：问题定义" {
		t.Fatalf("course detail payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoCourseArticles(t *testing.T) {
	content := &fakeDedaoContentProvider{
		articles: DedaoArticlePage{
			Articles: []DedaoArticle{
				{
					Enid:        "article-enid",
					ID:          501,
					Title:       "第一讲：问题定义",
					Summary:     "先定义问题，再选择工具。",
					PublishTime: 1710000000,
					OrderNum:    1,
				},
			},
			Count: 2,
			MaxID: 10,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/courses/course-enid/articles?count=2&max_id=10", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("course articles status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotArticlesEnid != "course-enid" || content.gotArticlesCount != 2 || content.gotArticlesMaxID != 10 {
		t.Fatalf("course articles args = enid %q count %d maxID %d", content.gotArticlesEnid, content.gotArticlesCount, content.gotArticlesMaxID)
	}
	var payload DedaoArticlePage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Count != 2 || payload.MaxID != 10 || len(payload.Articles) != 1 || payload.Articles[0].Enid != "article-enid" {
		t.Fatalf("course articles payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoArticleMarkdown(t *testing.T) {
	content := &fakeDedaoContentProvider{
		articleMarkdown: DedaoArticleMarkdown{
			Enid:     "article-enid",
			Type:     "course",
			Title:    "第一讲：问题定义",
			Markdown: "## 问题定义\n\n先定义问题，再选择工具。",
		},
		odobMarkdown: DedaoArticleMarkdown{
			Enid:     "audio-alias",
			Type:     "odob",
			Title:    "每天听本书",
			Markdown: "## 听书文稿\n\n一本书的精华解读。",
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/articles/article-enid?type=course", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("article markdown status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotArticleMarkdownEnid != "article-enid" {
		t.Fatalf("article markdown enid = %q", content.gotArticleMarkdownEnid)
	}
	var payload DedaoArticleMarkdown
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Markdown == "" || !strings.Contains(payload.Markdown, "问题定义") {
		t.Fatalf("article markdown payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())

	odobResp := requestKBase(handler, http.MethodGet, "/api/dedao/articles/audio-alias?type=odob", "secret-token")
	if odobResp.Code != http.StatusOK {
		t.Fatalf("odob markdown status = %d, body=%s", odobResp.Code, odobResp.Body.String())
	}
	if content.gotOdobMarkdownEnid != "audio-alias" {
		t.Fatalf("odob markdown enid = %q", content.gotOdobMarkdownEnid)
	}
	var odobPayload DedaoArticleMarkdown
	if err := json.Unmarshal(odobResp.Body.Bytes(), &odobPayload); err != nil {
		t.Fatalf("Unmarshal odob returned error: %v", err)
	}
	if odobPayload.Type != "odob" || !strings.Contains(odobPayload.Markdown, "听书文稿") {
		t.Fatalf("odob markdown payload = %#v", odobPayload)
	}
	assertDedaoResponseOmitsSecrets(t, odobResp.Body.String())

	badTypeResp := requestKBase(handler, http.MethodGet, "/api/dedao/articles/article-enid?type=video", "secret-token")
	if badTypeResp.Code != http.StatusBadRequest {
		t.Fatalf("article unsupported type status = %d, want 400", badTypeResp.Code)
	}
}

func TestKBaseHTTPHandlerServesDedaoEbookDetail(t *testing.T) {
	content := &fakeDedaoContentProvider{
		ebookDetail: DedaoEbookDetail{
			Enid:           "ebook-enid",
			ID:             67929,
			Title:          "强化学习教程",
			OperatingTitle: "强化学习教程",
			Cover:          "https://example.test/cover.jpg",
			BookAuthor:     "王琦",
			AuthorInfo:     "长期研究强化学习。",
			BookIntro:      "一本系统学习强化学习的教材。",
			PressName:      "测试出版社",
			ClassifyName:   "计算机",
			ReadTime:       12,
			Catalog: []DedaoEbookCatalogItem{
				{
					Level:     1,
					Text:      "第一章 导论",
					Href:      "chapter-1#0",
					ChapterID: "chapter-1",
					PlayOrder: 1,
				},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks/ebook-enid", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("ebook detail status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotEbookDetailEnid != "ebook-enid" {
		t.Fatalf("ebook detail enid = %q", content.gotEbookDetailEnid)
	}
	var payload DedaoEbookDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Title != "强化学习教程" || len(payload.Catalog) != 1 || payload.Catalog[0].ChapterID != "chapter-1" {
		t.Fatalf("ebook detail payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesDedaoEbookChapterPages(t *testing.T) {
	content := &fakeDedaoContentProvider{
		ebookPages: DedaoEbookChapterPages{
			Enid:      "ebook-enid",
			ChapterID: "chapter-1",
			Index:     4,
			Count:     8,
			Offset:    2,
			IsEnd:     true,
			Pages: []DedaoEbookPageSVG{
				{
					PageNum:     5,
					BeginOffset: 2,
					EndOffset:   88,
					SVG:         `<svg xmlns="http://www.w3.org/2000/svg"><text>第一章</text></svg>`,
				},
			},
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoContent: content,
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks/ebook-enid/chapters/chapter-1/pages?index=4&count=99&offset=2", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("ebook pages status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if content.gotEbookPagesEnid != "ebook-enid" || content.gotEbookPagesChapterID != "chapter-1" ||
		content.gotEbookPagesIndex != 4 || content.gotEbookPagesCount != 8 || content.gotEbookPagesOffset != 2 {
		t.Fatalf(
			"ebook pages args = enid %q chapter %q index %d count %d offset %d",
			content.gotEbookPagesEnid,
			content.gotEbookPagesChapterID,
			content.gotEbookPagesIndex,
			content.gotEbookPagesCount,
			content.gotEbookPagesOffset,
		)
	}
	var payload DedaoEbookChapterPages
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.Count != 8 || len(payload.Pages) != 1 || !strings.Contains(payload.Pages[0].SVG, "<svg") {
		t.Fatalf("ebook pages payload = %#v", payload)
	}
	assertDedaoResponseOmitsSecrets(t, resp.Body.String())
}

func TestKBaseHTTPHandlerServesWebAssets(t *testing.T) {
	root := t.TempDir()
	webDir := filepath.Join(root, "web")
	assetDir := filepath.Join(webDir, "assets")
	if err := os.MkdirAll(assetDir, os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<main id=\"app\">kbase web</main>"), 0o644); err != nil {
		t.Fatalf("WriteFile index returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "app.js"), []byte("console.log('kbase')"), 0o644); err != nil {
		t.Fatalf("WriteFile asset returned error: %v", err)
	}

	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
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
	if !strings.Contains(indexResp.Body.String(), "kbase web") {
		t.Fatalf("index response missing app shell: %s", indexResp.Body.String())
	}

	assetResp := requestKBase(handler, http.MethodGet, "/assets/app.js", "")
	if assetResp.Code != http.StatusOK {
		t.Fatalf("asset status = %d, body=%s", assetResp.Code, assetResp.Body.String())
	}
	if !strings.Contains(assetResp.Body.String(), "console.log") {
		t.Fatalf("asset response missing js: %s", assetResp.Body.String())
	}

	fallbackResp := requestKBase(handler, http.MethodGet, "/books/42", "")
	if fallbackResp.Code != http.StatusOK {
		t.Fatalf("fallback status = %d, body=%s", fallbackResp.Code, fallbackResp.Body.String())
	}
	if !strings.Contains(fallbackResp.Body.String(), "kbase web") {
		t.Fatalf("fallback response missing index: %s", fallbackResp.Body.String())
	}

	apiResp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if apiResp.Code != http.StatusUnauthorized {
		t.Fatalf("api status without token = %d, want 401", apiResp.Code)
	}
}

func TestKBaseHTTPHandlerServesBrowserSessionTokenOnlyFromTrustedProxy(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	directResp := requestKBase(handler, http.MethodGet, "/browser/session-token", "")
	if directResp.Code != http.StatusNotFound {
		t.Fatalf("direct browser token status = %d, want 404", directResp.Code)
	}

	proxyResp := requestKBaseWithHeaders(handler, http.MethodGet, "/browser/session-token", "", "", map[string]string{
		"X-KBase-Browser-Session": "1",
	})
	if proxyResp.Code != http.StatusOK {
		t.Fatalf("proxy browser token status = %d, body=%s", proxyResp.Code, proxyResp.Body.String())
	}
	if !strings.Contains(proxyResp.Body.String(), `"token":"secret-token"`) {
		t.Fatalf("browser token response missing token: %s", proxyResp.Body.String())
	}
	if cacheControl := proxyResp.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}

	postResp := requestKBaseWithHeaders(handler, http.MethodPost, "/browser/session-token", "", "", map[string]string{
		"X-KBase-Browser-Session": "1",
	})
	if postResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("browser token POST status = %d, want 405", postResp.Code)
	}
}

func TestKBaseHTTPHandlerServesSkillDiscoveryWithoutBearer(t *testing.T) {
	root := t.TempDir()
	handler := newTestKBaseHandlerWithSystemKB(t, root)

	discoveryResp := requestKBase(handler, http.MethodGet, "/.well-known/dedao-kbase-skills.json", "")
	if discoveryResp.Code != http.StatusOK {
		t.Fatalf("discovery status = %d, body=%s", discoveryResp.Code, discoveryResp.Body.String())
	}
	if !strings.Contains(discoveryResp.Body.String(), `"dedao.book.search"`) ||
		!strings.Contains(discoveryResp.Body.String(), `"/api/skills/dedao.book.search/manifest.json"`) {
		t.Fatalf("discovery response missing skill metadata: %s", discoveryResp.Body.String())
	}

	listResp := requestKBase(handler, http.MethodGet, "/api/skills", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("skills list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"dedao.system_kb.export"`) {
		t.Fatalf("skills list missing system kb export skill: %s", listResp.Body.String())
	}

	manifestResp := requestKBase(handler, http.MethodGet, "/api/skills/dedao.book.search/manifest.json", "")
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"invoke_url"`) ||
		!strings.Contains(manifestResp.Body.String(), `"bearer"`) {
		t.Fatalf("manifest response missing invocation contract: %s", manifestResp.Body.String())
	}

	openAPIResp := requestKBase(handler, http.MethodGet, "/api/skills/dedao.book.search/openapi.json", "")
	if openAPIResp.Code != http.StatusOK {
		t.Fatalf("openapi status = %d, body=%s", openAPIResp.Code, openAPIResp.Body.String())
	}
	if !strings.Contains(openAPIResp.Body.String(), `"/api/skills/dedao.book.search/invoke"`) {
		t.Fatalf("openapi response missing invoke path: %s", openAPIResp.Body.String())
	}

	skillDocResp := requestKBase(handler, http.MethodGet, "/api/skills/dedao.book.search/SKILL.md", "")
	if skillDocResp.Code != http.StatusOK {
		t.Fatalf("skill doc status = %d, body=%s", skillDocResp.Code, skillDocResp.Body.String())
	}
	if contentType := skillDocResp.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
		t.Fatalf("skill doc content-type = %q, want text/markdown", contentType)
	}
	if !strings.Contains(skillDocResp.Body.String(), "Authorization: Bearer") {
		t.Fatalf("skill doc should describe bearer token usage: %s", skillDocResp.Body.String())
	}

	invokeResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.book.search/invoke", "", `{"arguments":{"query":"MACD"}}`)
	if invokeResp.Code != http.StatusUnauthorized {
		t.Fatalf("invoke status without token = %d, want 401", invokeResp.Code)
	}
}

func TestKBaseHTTPHandlerInvokesSkillsWithBearer(t *testing.T) {
	root := t.TempDir()
	handler := newTestKBaseHandlerWithSystemKB(t, root)

	searchResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.book.search/invoke", "secret-token", `{"arguments":{"query":"MACD","limit":5}}`)
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search invoke status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), `"skill":"dedao.book.search"`) ||
		!strings.Contains(searchResp.Body.String(), `"book_id":"42"`) {
		t.Fatalf("search invoke response missing result: %s", searchResp.Body.String())
	}

	contextResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.book.get_context/invoke", "secret-token", `{"arguments":{"book_id":"42"}}`)
	if contextResp.Code != http.StatusOK {
		t.Fatalf("context invoke status = %d, body=%s", contextResp.Code, contextResp.Body.String())
	}
	if !strings.Contains(contextResp.Body.String(), `"chapters"`) ||
		!strings.Contains(contextResp.Body.String(), `"claims"`) {
		t.Fatalf("context invoke response missing package context: %s", contextResp.Body.String())
	}

	manifestResp := requestKBaseJSON(handler, http.MethodPost, "/api/skills/dedao.system_kb.manifest/invoke", "secret-token", `{}`)
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest invoke status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"version":"test-version"`) {
		t.Fatalf("manifest invoke response missing version: %s", manifestResp.Body.String())
	}

	unknownResp := requestKBase(handler, http.MethodGet, "/api/skills/unknown.skill/manifest.json", "")
	if unknownResp.Code != http.StatusNotFound {
		t.Fatalf("unknown skill status = %d, want 404", unknownResp.Code)
	}
}

type fakeDedaoAuthProvider struct {
	qr              DedaoLoginQRCode
	check           DedaoLoginCheck
	gotToken        string
	gotQRCodeString string
}

func (f *fakeDedaoAuthProvider) NewQRCode() (DedaoLoginQRCode, error) {
	return f.qr, nil
}

func (f *fakeDedaoAuthProvider) CheckLogin(token string, qrCodeString string) (DedaoLoginCheck, error) {
	f.gotToken = token
	f.gotQRCodeString = qrCodeString
	return f.check, nil
}

type fakeDedaoContentProvider struct {
	ebooks                 DedaoEbookPage
	courses                DedaoCoursePage
	odobs                  DedaoOdobPage
	odobDetail             DedaoOdobDetail
	courseDetail           DedaoCourseDetail
	articles               DedaoArticlePage
	articleMarkdown        DedaoArticleMarkdown
	odobMarkdown           DedaoArticleMarkdown
	ebookDetail            DedaoEbookDetail
	ebookPages             DedaoEbookChapterPages
	gotQuery               string
	gotPage                int
	gotPageSize            int
	gotCourseQuery         string
	gotCoursePage          int
	gotCoursePageSize      int
	gotOdobQuery           string
	gotOdobPage            int
	gotOdobPageSize        int
	gotOdobDetailEnid      string
	gotCourseDetailEnid    string
	gotArticlesEnid        string
	gotArticlesCount       int
	gotArticlesMaxID       int
	gotArticleMarkdownEnid string
	gotOdobMarkdownEnid    string
	gotEbookDetailEnid     string
	gotEbookPagesEnid      string
	gotEbookPagesChapterID string
	gotEbookPagesIndex     int
	gotEbookPagesCount     int
	gotEbookPagesOffset    int
}

func (f *fakeDedaoContentProvider) ListEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	f.gotQuery = query
	f.gotPage = page
	f.gotPageSize = pageSize
	return f.ebooks, nil
}

func (f *fakeDedaoContentProvider) ListCourses(query string, page, pageSize int) (DedaoCoursePage, error) {
	f.gotCourseQuery = query
	f.gotCoursePage = page
	f.gotCoursePageSize = pageSize
	return f.courses, nil
}

func (f *fakeDedaoContentProvider) ListOdobs(query string, page, pageSize int) (DedaoOdobPage, error) {
	f.gotOdobQuery = query
	f.gotOdobPage = page
	f.gotOdobPageSize = pageSize
	return f.odobs, nil
}

func (f *fakeDedaoContentProvider) GetOdobDetail(enid string) (DedaoOdobDetail, error) {
	f.gotOdobDetailEnid = enid
	return f.odobDetail, nil
}

func (f *fakeDedaoContentProvider) GetCourseDetail(enid string) (DedaoCourseDetail, error) {
	f.gotCourseDetailEnid = enid
	return f.courseDetail, nil
}

func (f *fakeDedaoContentProvider) ListCourseArticles(enid string, count, maxID int) (DedaoArticlePage, error) {
	f.gotArticlesEnid = enid
	f.gotArticlesCount = count
	f.gotArticlesMaxID = maxID
	return f.articles, nil
}

func (f *fakeDedaoContentProvider) GetCourseArticleMarkdown(enid string) (DedaoArticleMarkdown, error) {
	f.gotArticleMarkdownEnid = enid
	return f.articleMarkdown, nil
}

func (f *fakeDedaoContentProvider) GetOdobArticleMarkdown(enid string) (DedaoArticleMarkdown, error) {
	f.gotOdobMarkdownEnid = enid
	return f.odobMarkdown, nil
}

func (f *fakeDedaoContentProvider) GetEbookDetail(enid string) (DedaoEbookDetail, error) {
	f.gotEbookDetailEnid = enid
	return f.ebookDetail, nil
}

func (f *fakeDedaoContentProvider) GetEbookChapterPages(enid string, chapterID string, index, count, offset int) (DedaoEbookChapterPages, error) {
	f.gotEbookPagesEnid = enid
	f.gotEbookPagesChapterID = chapterID
	f.gotEbookPagesIndex = index
	f.gotEbookPagesCount = count
	f.gotEbookPagesOffset = offset
	return f.ebookPages, nil
}

func assertDedaoResponseOmitsSecrets(t *testing.T, body string) {
	t.Helper()
	lowerBody := strings.ToLower(body)
	for _, forbidden := range []string{"cookie", "token", "drm_token", "dd_url", "dd_article_token"} {
		if strings.Contains(lowerBody, forbidden) {
			t.Fatalf("dedao response must not expose %s: %s", forbidden, body)
		}
	}
}

func newTestKBaseHandlerWithSystemKB(t *testing.T, root string) http.Handler {
	t.Helper()
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
	return NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:              store,
		AuthToken:          "secret-token",
		SystemKBExportPath: exportPath,
	})
}

func sampleBookKnowledgePackageWithID(bookID, title string) BookKnowledgePackage {
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = bookID
	pkg.Book.Title = title
	pkg.Book.UpdatedAt = fmt.Sprintf("2026-06-28T00:00:0%sZ", strings.TrimPrefix(bookID, "book-"))
	for i := range pkg.Chapters {
		pkg.Chapters[i].BookID = bookID
		pkg.Chapters[i].ChapterID = bookID + "-chapter-1"
	}
	for i := range pkg.Chunks {
		pkg.Chunks[i].BookID = bookID
		pkg.Chunks[i].ChapterID = bookID + "-chapter-1"
		pkg.Chunks[i].ChunkID = bookID + "-chunk-1"
	}
	for i := range pkg.Claims {
		pkg.Claims[i].BookID = bookID
		pkg.Claims[i].ChapterID = bookID + "-chapter-1"
		pkg.Claims[i].ClaimID = bookID + "-claim-1"
	}
	for i := range pkg.Citations {
		pkg.Citations[i].BookID = bookID
		pkg.Citations[i].ChapterID = bookID + "-chapter-1"
		pkg.Citations[i].ChunkID = bookID + "-chunk-1"
		pkg.Citations[i].CitationID = bookID + "-citation-1"
	}
	return pkg
}

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	return requestKBaseJSON(handler, method, path, token, "")
}

func requestKBaseJSON(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	return requestKBaseWithHeaders(handler, method, path, token, body, nil)
}

func requestKBaseWithHeaders(handler http.Handler, method, path, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}
