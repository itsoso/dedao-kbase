package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestKBaseHTTPHandlerSanitizesDedaoEbookAcquisitionErrors(t *testing.T) {
	privateError := errors.New("private-cookie /srv/private/config.json")
	provider := &fakeDedaoEbookAcquisition{searchError: privateError, addError: privateError}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "secret-token", DedaoEbooks: provider,
	})

	search := requestKBase(handler, http.MethodGet, "/api/dedao/search/ebooks?q=test", "secret-token")
	if search.Code != http.StatusBadGateway || !strings.Contains(search.Body.String(), "failed to search dedao ebooks") {
		t.Fatalf("search error status=%d body=%s", search.Code, search.Body.String())
	}
	assertDedaoEbookResponseOmitsSecrets(t, search.Body.String())

	bookshelf := requestKBase(handler, http.MethodPost, "/api/dedao/ebooks/test-enid/bookshelf", "secret-token")
	if bookshelf.Code != http.StatusBadGateway || !strings.Contains(bookshelf.Body.String(), "failed to add dedao ebook to bookshelf") {
		t.Fatalf("bookshelf error status=%d body=%s", bookshelf.Code, bookshelf.Body.String())
	}
	assertDedaoEbookResponseOmitsSecrets(t, bookshelf.Body.String())
}

func TestKBaseHTTPHandlerSearchesDedaoEbooks(t *testing.T) {
	provider := &fakeDedaoEbookAcquisition{
		searchPage: DedaoEbookPage{
			Ebooks: []DedaoEbook{{
				ID: 32355, Enid: "site-ebook-enid", Title: "陆蓉行为金融学讲义", Author: "陆蓉", IsBuy: true,
			}},
			Page: 3, PageSize: 7, Total: 54455, TotalPages: 7780, IsMore: 1,
		},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:       NewBookKnowledgeStore(t.TempDir()),
		AuthToken:   "secret-token",
		DedaoEbooks: provider,
	})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/dedao/search/ebooks?q=test", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("search without bearer = %d, want 401", unauthorized.Code)
	}

	path := "/api/dedao/search/ebooks?page=3&page_size=7&q=" + url.QueryEscape("金融")
	resp := requestKBase(handler, http.MethodGet, path, "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if provider.gotQuery != "金融" || provider.gotPage != 3 || provider.gotPageSize != 7 {
		t.Fatalf("search args = query %q page %d size %d", provider.gotQuery, provider.gotPage, provider.gotPageSize)
	}
	var payload DedaoEbookPage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if len(payload.Ebooks) != 1 || payload.Ebooks[0].Enid != "site-ebook-enid" || payload.TotalPages != 7780 {
		t.Fatalf("search payload = %#v", payload)
	}
	assertDedaoEbookResponseOmitsSecrets(t, resp.Body.String())

	wrongMethod := requestKBase(handler, http.MethodPost, path, "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("search POST status = %d, want 405", wrongMethod.Code)
	}
}

func TestDedaoSiteEbookSearchMappingStripsHighlightsAndSecrets(t *testing.T) {
	got := dedaoEbookPageFromSiteSearch(&services.EbookSearchResult{
		Page: 2, Size: 5, Total: 11, IsMore: 1,
		List: []services.EbookSearchItem{{
			Title: "行为<hl>金融</hl>学", Author: "示例作者", Content: "命中片段", Image: "https://example.test/search.jpg",
			Detail: services.EbookSearchDetail{
				ID: 32355, Enid: "site-ebook-enid", BookName: "行为<hl>金融</hl>学", BookAuthor: "示例作者",
				BookIntro: "安全简介", Cover: "https://example.test/detail.jpg", CurrentPrice: "41.30",
				CanTrialRead: true, IsBuy: true, ReadProgress: 17, ReadingTitle: "第 <hl>1</hl> 章",
				ReadingWordToken: "must-not-leak",
			},
		}},
	}, 2, 5)

	if got.Page != 2 || got.PageSize != 5 || got.Total != 11 || got.TotalPages != 3 || got.IsMore != 1 {
		t.Fatalf("pagination = %#v", got)
	}
	if len(got.Ebooks) != 1 {
		t.Fatalf("ebooks = %#v", got.Ebooks)
	}
	ebook := got.Ebooks[0]
	if ebook.ID != 32355 || ebook.Enid != "site-ebook-enid" || ebook.Title != "行为金融学" || ebook.LastRead != "第 1 章" {
		t.Fatalf("mapped ebook = %#v", ebook)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-leak") || strings.Contains(string(raw), "reading_word_token") || strings.Contains(string(raw), "<hl>") {
		t.Fatalf("safe mapping leaked upstream-only fields: %s", raw)
	}
}

func TestKBaseHTTPHandlerAddsDedaoEbookToBookshelf(t *testing.T) {
	provider := &fakeDedaoEbookAcquisition{
		added: DedaoEbook{ID: 32355, Enid: "site-ebook-enid", Title: "陆蓉行为金融学讲义", IsBuy: true},
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:       NewBookKnowledgeStore(t.TempDir()),
		AuthToken:   "secret-token",
		DedaoEbooks: provider,
	})

	resp := requestKBase(handler, http.MethodPost, "/api/dedao/ebooks/site-ebook-enid/bookshelf", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("bookshelf status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if provider.gotAddedEnid != "site-ebook-enid" || !strings.Contains(resp.Body.String(), `"is_buy":true`) {
		t.Fatalf("bookshelf response=%s enid=%q", resp.Body.String(), provider.gotAddedEnid)
	}
	assertDedaoEbookResponseOmitsSecrets(t, resp.Body.String())

	wrongMethod := requestKBase(handler, http.MethodGet, "/api/dedao/ebooks/site-ebook-enid/bookshelf", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bookshelf GET status = %d, want 405", wrongMethod.Code)
	}
	malformed := requestKBase(handler, http.MethodPost, "/api/dedao/ebooks//bookshelf", "secret-token")
	if malformed.Code != http.StatusNotFound {
		t.Fatalf("malformed bookshelf status = %d, want 404", malformed.Code)
	}
}

func assertDedaoEbookResponseOmitsSecrets(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"CookieStr", "cookie_str", "access_token", "config.json", "private-cookie"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
}

type fakeDedaoEbookAcquisition struct {
	searchPage    DedaoEbookPage
	added         DedaoEbook
	detail        *services.EbookDetail
	gotQuery      string
	gotPage       int
	gotPageSize   int
	gotAddedEnid  string
	gotDetailEnid string
	searchError   error
	addError      error
	detailError   error
}

func (f *fakeDedaoEbookAcquisition) SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	f.gotQuery, f.gotPage, f.gotPageSize = query, page, pageSize
	return f.searchPage, f.searchError
}

func (f *fakeDedaoEbookAcquisition) AddEbookToBookshelf(enid string) (DedaoEbook, error) {
	f.gotAddedEnid = enid
	return f.added, f.addError
}

func (f *fakeDedaoEbookAcquisition) EbookDetail(enid string) (*services.EbookDetail, error) {
	f.gotDetailEnid = enid
	return f.detail, f.detailError
}
