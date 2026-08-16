package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChatlogHTTPSearchUsesV0015QueryAndNormalizesMessages(t *testing.T) {
	var captured url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/chatlog" {
			t.Fatalf("request=%s %s", r.Method, r.URL.String())
		}
		captured = r.URL.Query()
		_, _ = w.Write([]byte(`[
			{"seq":1002,"time":"2026-08-13T08:02:00+08:00","talker":"room-1","talkerName":"Room",
			 "isChatRoom":true,"sender":"person-2","senderName":"Person 2","isSelf":false,"type":49,"subType":57,
			 "content":"reply","contents":{"refer":{"seq":999,"time":"2026-08-13T08:00:00+08:00","sender":"person-1","senderName":"Person 1","type":1,"content":"quoted"}}},
			{"seq":1001,"time":"2026-08-13T08:01:00+08:00","talker":"room-1","talkerName":"Room",
			 "isChatRoom":true,"sender":"person-1","senderName":"Person 1","isSelf":false,"type":1,"subType":0,"content":"match"}
		]`))
	}))
	defer server.Close()
	reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := reader.SearchMessages(context.Background(), ChatlogQuery{
		Time: "2026-08-13", Talker: "room-1", Sender: "person-1", Keyword: "match", Limit: 20, Offset: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := map[string]string{
		"format": "json", "time": "2026-08-13", "talker": "room-1", "sender": "person-1",
		"keyword": "match", "limit": "20", "offset": "3",
	}
	for key, want := range wantQuery {
		if got := captured.Get(key); got != want {
			t.Fatalf("query %s=%q want=%q", key, got, want)
		}
	}
	if len(messages) != 2 || messages[0].Seq != 1001 || messages[1].Seq != 1002 || messages[1].MessageRef != "1002" {
		t.Fatalf("messages=%#v", messages)
	}
	if messages[1].Referred == nil || messages[1].Referred.Content != "quoted" || messages[1].Referred.Sender != "person-1" {
		t.Fatalf("referred=%#v", messages[1].Referred)
	}
}

func TestChatlogHTTPListsContactsRoomsAndSessionsWithPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("format") != "json" || query.Get("keyword") != "term" || query.Get("limit") != "2" || query.Get("offset") != "4" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/api/v1/contact":
			_, _ = w.Write([]byte(`{"items":[{"userName":"person-1","alias":"alias-1","remark":"Remark","nickName":"Nick","isFriend":true}]}`))
		case "/api/v1/chatroom":
			_, _ = w.Write([]byte(`{"items":[{"name":"room-1","owner":"owner-1","users":[{"userName":"person-1","displayName":"Display"}],"remark":"Remark","nickName":"Room"}]}`))
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"items":[{"userName":"room-1","nOrder":9,"nickName":"Room","content":"last","nTime":"2026-08-13T08:00:00+08:00"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	contacts, err := reader.ListContacts(context.Background(), "term", 2, 4)
	if err != nil || len(contacts.Items) != 1 || contacts.Items[0].UserName != "person-1" {
		t.Fatalf("contacts=%#v err=%v", contacts, err)
	}
	rooms, err := reader.ListChatRooms(context.Background(), "term", 2, 4)
	if err != nil || len(rooms.Items) != 1 || len(rooms.Items[0].Users) != 1 {
		t.Fatalf("rooms=%#v err=%v", rooms, err)
	}
	sessions, err := reader.ListSessions(context.Background(), "term", 2, 4)
	if err != nil || len(sessions.Items) != 1 || sessions.Items[0].NOrder != 9 {
		t.Fatalf("sessions=%#v err=%v", sessions, err)
	}
}

func TestChatlogHTTPSearchThenContextRemovesKeywordAndSenderFilters(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		call := len(queries)
		mu.Unlock()
		if call == 1 {
			_, _ = w.Write([]byte(`[{"seq":2002,"time":"2026-08-13T08:02:00+08:00","talker":"room-1","sender":"person-1","type":1,"content":"match"}]`))
			return
		}
		_, _ = w.Write([]byte(`[
			{"seq":2003,"time":"2026-08-13T08:03:00+08:00","talker":"room-1","sender":"person-2","type":1,"content":"after"},
			{"seq":2001,"time":"2026-08-13T08:01:00+08:00","talker":"room-1","sender":"person-2","type":1,"content":"before"},
			{"seq":2002,"time":"2026-08-13T08:02:00+08:00","talker":"room-1","sender":"person-1","type":1,"content":"match"}
		]`))
	}))
	defer server.Close()
	reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	bundles, err := reader.SearchMessagesWithContext(context.Background(), ChatlogQuery{
		Time: "2026-08-13", Talker: "room-1", Sender: "person-1", Keyword: "match", Limit: 5,
	}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[1].Get("keyword") != "" || queries[1].Get("sender") != "" || queries[1].Get("talker") != "room-1" {
		t.Fatalf("queries=%#v", queries)
	}
	if len(bundles) != 1 || len(bundles[0].Messages) != 3 || bundles[0].Messages[0].Seq != 2001 || bundles[0].Messages[2].Seq != 2003 {
		t.Fatalf("bundles=%#v", bundles)
	}
}

func TestChatlogHTTPRejectsUnsafeURLsRedirectsAndBounds(t *testing.T) {
	if _, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: "https://example.com"}); err == nil {
		t.Fatal("non-loopback URL accepted")
	}
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer redirectTarget.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/api/v1/chatlog", http.StatusFound)
	}))
	defer redirect.Close()
	reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: redirect.URL, HTTPClient: redirect.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.SearchMessages(context.Background(), ChatlogQuery{Time: "2026-08-13", Talker: "room-1", Limit: 1}); !errors.Is(err, ErrChatlogUnsafeRedirect) {
		t.Fatalf("redirect error=%v", err)
	}
	for _, query := range []ChatlogQuery{
		{Time: "2026-08-13", Talker: "room-1", Limit: chatlogHTTPMaxRows + 1},
		{Time: "2026-08-13", Talker: "room-1", Limit: 1, Offset: -1},
	} {
		if _, err := reader.SearchMessages(context.Background(), query); err == nil {
			t.Fatalf("invalid query accepted: %#v", query)
		}
	}
}

func TestChatlogHTTPEnforcesTimeoutOnCustomClient(t *testing.T) {
	reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{
		BaseURL: "http://127.0.0.1:5030", HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader.client.Timeout <= 0 || reader.client.Timeout > 30*time.Second {
		t.Fatalf("timeout=%s", reader.client.Timeout)
	}
}

func TestChatlogHTTPDefaultTimeoutSupportsBoundedLongLocalQueries(t *testing.T) {
	reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: "http://127.0.0.1:5030"})
	if err != nil {
		t.Fatal(err)
	}
	if reader.client.Timeout != 30*time.Second {
		t.Fatalf("timeout=%s want=30s", reader.client.Timeout)
	}
}

func TestChatlogHTTPBoundsBodiesAndClassifiesUnavailableOrMalformedService(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"oversized": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(chatlogHTTPMaxResponseBytes)+1)))
		},
		"malformed": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[{`)) },
		"timeout":   func(w http.ResponseWriter, _ *http.Request) { time.Sleep(100 * time.Millisecond) },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			client := server.Client()
			client.Timeout = 25 * time.Millisecond
			reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: server.URL, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.SearchMessages(context.Background(), ChatlogQuery{Time: "2026-08-13", Talker: "room-1", Limit: 1})
			if err == nil {
				t.Fatal("invalid response accepted")
			}
			if name == "timeout" && !errors.Is(err, ErrChatlogUnavailable) {
				t.Fatalf("timeout error=%v", err)
			}
		})
	}
}
