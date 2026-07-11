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
