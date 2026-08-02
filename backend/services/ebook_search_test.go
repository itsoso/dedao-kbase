package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestSearchEbooksUsesSiteWideEbookEndpoint(t *testing.T) {
	service := withTestLoginServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search/v2/pc/searchebookchapter" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["content"] != "金融" || int(body["page"].(float64)) != 3 || int(body["size"].(float64)) != 7 || int(body["type"].(float64)) != 2 {
			t.Fatalf("request body = %#v", body)
		}
		fmt.Fprint(w, `{"h":{"c":0},"c":{"page":3,"size":7,"is_more":1,"total":8,"list":[{"title":"<hl>行为金融学</hl>","detail":{"id":32355,"enid":"ebook-enid","book_name":"<hl>行为金融学</hl>","book_author":"陆蓉","cover":"https://example.test/cover.jpg","current_price":"41.30","is_buy":true}}]}}`)
	})

	result, err := service.SearchEbooks("金融", 3, 7)
	if err != nil {
		t.Fatalf("SearchEbooks returned error: %v", err)
	}
	if result.Page != 3 || result.Size != 7 || result.Total != 8 || result.IsMore != 1 {
		t.Fatalf("pagination = %#v", result)
	}
	if len(result.List) != 1 || result.List[0].Detail.Enid != "ebook-enid" {
		t.Fatalf("results = %#v", result.List)
	}
}
