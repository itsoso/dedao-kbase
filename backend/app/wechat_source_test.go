package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWeChatSourceDownloadsArticleAsMarkdown(t *testing.T) {
	articleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
  <head><meta name="description" content="summary text"></head>
  <body>
    <h1 id="activity-name">  AI 医学学习路线  </h1>
    <a id="js_name">医学参考</a>
    <em id="publish_time">2026-07-06</em>
    <div id="js_content">
      <p>第一段说明知识库如何服务验证。</p>
      <p><strong>第二段</strong>包含可检索内容。</p>
      <img data-src="https://mmbiz.qpic.cn/example.jpg" alt="配图">
    </div>
  </body>
</html>`)
	}))
	defer articleServer.Close()

	service := NewWeChatSourceService(WeChatSourceConfig{})
	article, err := service.DownloadArticle(context.Background(), articleServer.URL+"/s/test")
	if err != nil {
		t.Fatalf("DownloadArticle returned error: %v", err)
	}

	if article.Title != "AI 医学学习路线" {
		t.Fatalf("Title = %q", article.Title)
	}
	if article.AccountName != "医学参考" {
		t.Fatalf("AccountName = %q", article.AccountName)
	}
	if article.PublishedAt != "2026-07-06" {
		t.Fatalf("PublishedAt = %q", article.PublishedAt)
	}
	if !strings.Contains(article.Markdown, "# AI 医学学习路线") {
		t.Fatalf("Markdown missing title: %s", article.Markdown)
	}
	if !strings.Contains(article.Markdown, "第一段说明知识库如何服务验证。") {
		t.Fatalf("Markdown missing paragraph: %s", article.Markdown)
	}
	if !strings.Contains(article.Markdown, "![配图](https://mmbiz.qpic.cn/example.jpg)") {
		t.Fatalf("Markdown missing image: %s", article.Markdown)
	}
	if !strings.Contains(article.Text, "第二段包含可检索内容。") {
		t.Fatalf("Text missing normalized body: %q", article.Text)
	}
}

func TestWeChatSourceSearchAndListArticlesUseOfficialAPIs(t *testing.T) {
	var sawSearchCookie bool
	var sawListCookie bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/cgi-bin/searchbiz":
			if got := r.URL.Query().Get("token"); got != "test-token" {
				t.Fatalf("search token = %q", got)
			}
			if got := r.URL.Query().Get("query"); got != "医学" {
				t.Fatalf("search query = %q", got)
			}
			sawSearchCookie = strings.Contains(r.Header.Get("Cookie"), "session=test")
			fmt.Fprint(w, `{"base_resp":{"ret":0},"list":[{"nickname":"医学参考","fakeid":"fake-123","alias":"med-ref"}]}`)
		case "/cgi-bin/appmsg":
			if got := r.URL.Query().Get("token"); got != "test-token" {
				t.Fatalf("list token = %q", got)
			}
			if got := r.URL.Query().Get("fakeid"); got != "fake-123" {
				t.Fatalf("fakeid = %q", got)
			}
			if got := r.URL.Query().Get("begin"); got != "5" {
				t.Fatalf("begin = %q", got)
			}
			if got := r.URL.Query().Get("count"); got != "10" {
				t.Fatalf("count = %q", got)
			}
			sawListCookie = strings.Contains(r.Header.Get("Cookie"), "session=test")
			fmt.Fprint(w, `{"base_resp":{"ret":0},"app_msg_cnt":1,"app_msg_list":[{"title":"文章标题","link":"https://mp.weixin.qq.com/s/demo","digest":"摘要","cover":"https://example.com/c.jpg","update_time":1783353600,"aid":"aid-1","appmsgid":123,"itemidx":1}]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	service := NewWeChatSourceService(WeChatSourceConfig{
		MPBaseURL: apiServer.URL,
		Token:     "test-token",
		Cookie:    "session=test",
	})

	accounts, err := service.SearchOfficialAccounts(context.Background(), "医学")
	if err != nil {
		t.Fatalf("SearchOfficialAccounts returned error: %v", err)
	}
	if !sawSearchCookie {
		t.Fatalf("search request did not include configured cookie")
	}
	if len(accounts) != 1 || accounts[0].FakeID != "fake-123" || accounts[0].Nickname != "医学参考" {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}

	articles, err := service.ListOfficialAccountArticles(context.Background(), "fake-123", 5, 10)
	if err != nil {
		t.Fatalf("ListOfficialAccountArticles returned error: %v", err)
	}
	if !sawListCookie {
		t.Fatalf("list request did not include configured cookie")
	}
	if len(articles) != 1 || articles[0].Title != "文章标题" || articles[0].Link != "https://mp.weixin.qq.com/s/demo" {
		t.Fatalf("unexpected articles: %#v", articles)
	}
}

func TestWeChatSourceRequiresExplicitCredentialsForOfficialAPIs(t *testing.T) {
	service := NewWeChatSourceService(WeChatSourceConfig{})

	if _, err := service.SearchOfficialAccounts(context.Background(), "医学"); err != ErrWeChatCredentialsNotConfigured {
		t.Fatalf("SearchOfficialAccounts error = %v, want ErrWeChatCredentialsNotConfigured", err)
	}
	if _, err := service.ListOfficialAccountArticles(context.Background(), "fake-123", 0, 5); err != ErrWeChatCredentialsNotConfigured {
		t.Fatalf("ListOfficialAccountArticles error = %v, want ErrWeChatCredentialsNotConfigured", err)
	}
}
