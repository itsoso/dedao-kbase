package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yann0917/dedao-gui/backend/services"
)

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
}

func (f *fakeDedaoEbookAcquisition) SearchEbooks(query string, page, pageSize int) (DedaoEbookPage, error) {
	f.gotQuery, f.gotPage, f.gotPageSize = query, page, pageSize
	return f.searchPage, nil
}

func (f *fakeDedaoEbookAcquisition) AddEbookToBookshelf(enid string) (DedaoEbook, error) {
	f.gotAddedEnid = enid
	return f.added, nil
}

func (f *fakeDedaoEbookAcquisition) EbookDetail(enid string) (*services.EbookDetail, error) {
	f.gotDetailEnid = enid
	return f.detail, nil
}
