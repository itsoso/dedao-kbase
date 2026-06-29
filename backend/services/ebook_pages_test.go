package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestEbookReaderPagesUsesReaderSizedCanvas(t *testing.T) {
	service := withTestLoginServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ebk_web_go/v2/get_pages" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode body returned error: %v", err)
		}
		config, ok := body["config"].(map[string]any)
		if !ok {
			t.Fatalf("config body = %#v", body["config"])
		}
		if got := int(config["width"].(float64)); got != ebookReaderPageWidth {
			t.Fatalf("ebook page width = %d, want %d", got, ebookReaderPageWidth)
		}
		if got := int(config["height"].(float64)); got != ebookReaderPageHeight {
			t.Fatalf("ebook page height = %d, want %d", got, ebookReaderPageHeight)
		}
		fmt.Fprint(w, `{"h":{"c":0,"e":null},"c":0,"data":{"is_end":true,"pages":[]}}`)
	})

	body, err := service.reqEbookReaderPages("chapter-1", "read-token", 0, 2, 0)
	if err != nil {
		t.Fatalf("reqEbookReaderPages returned error: %v", err)
	}
	defer body.Close()
}
