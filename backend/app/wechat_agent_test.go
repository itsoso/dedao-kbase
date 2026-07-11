package app

import (
	"context"
	"testing"
)

type fakeSessionHealthProvider struct {
	session WeChatMPSession
	err     error
}

func (p fakeSessionHealthProvider) Session(context.Context) (WeChatMPSession, error) {
	return p.session, p.err
}
func TestWeChatAgentReportsLoginRequiredWithoutSession(t *testing.T) {
	adapter, err := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProvider{err: ErrSourceSecretNotFound}})
	if err != nil {
		t.Fatal(err)
	}
	health := adapter.Status(context.Background())
	if health.Healthy || health.RequiresAction != "login" {
		t.Fatalf("health=%#v", health)
	}
	if adapter.Name() != "wechat_mp" {
		t.Fatalf("name=%s", adapter.Name())
	}
}
func TestWeChatAgentDeclaresFirstPartyOperations(t *testing.T) {
	adapter, _ := NewWeChatSourceAdapter(WeChatSourceAdapterConfig{Sessions: fakeSessionHealthProvider{session: WeChatMPSession{Token: "test-value"}}})
	want := map[string]bool{"discover_articles": true, "sync_articles": true, "sync_media": true}
	for _, operation := range adapter.Operations() {
		delete(want, operation)
	}
	if len(want) != 0 {
		t.Fatalf("missing operations=%v", want)
	}
}

func TestWeChatAgentCursorDecodesLegacyBegin(t *testing.T) {
	cursor, err := decodeWeChatAgentCursor(`{"begin":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.UpstreamBegin != 10 || cursor.PublicationItemIndex != 0 {
		t.Fatalf("cursor=%#v", cursor)
	}
}

func TestWeChatAgentCursorRoundTrips(t *testing.T) {
	want := weChatAgentCursor{
		UpstreamBegin:        12,
		PublicationItemIndex: 2,
		LastArticleKey:       "article-key",
		LastTimestamp:        1234,
	}
	encoded, err := encodeWeChatAgentCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWeChatAgentCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cursor=%#v want=%#v encoded=%s", got, want, encoded)
	}
}

func TestWeChatAgentCursorRejectsInvalidValue(t *testing.T) {
	for _, raw := range []string{`{"upstream_begin":`, `{"upstream_begin":-1}`, `{"upstream_begin":1,"publication_item_index":-1}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := decodeWeChatAgentCursor(raw); err == nil {
				t.Fatalf("accepted invalid cursor %q", raw)
			}
		})
	}
}
