package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type curatedSink struct {
	items []SourceArticleEnvelope
	fail  bool
}

func (s *curatedSink) Enqueue(_ string, e SourceArticleEnvelope) (SourceAgentOutboxItem, error) {
	if s.fail {
		return SourceAgentOutboxItem{}, fmt.Errorf("failed")
	}
	s.items = append(s.items, e)
	return SourceAgentOutboxItem{}, nil
}
func curatedRun(kind string) SourceSyncRun {
	return SourceSyncRun{ID: "run", RequestedOperation: "sync_curated", Subscription: &SourceSubscription{SourceType: kind, SourceAccountKey: "chosen", SourceAccount: "Synthetic source"}}
}
func TestCuratedRSSBoundedAndNoAudioClaim(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss version="2.0"><channel><item><guid>stable</guid><title>Episode</title><link>https://example.org/episode</link><description><![CDATA[<p>This is a synthetic episode description containing enough text for indexing.</p><script>do not retain this</script>]]></description></item></channel></rss>`)
	}))
	defer server.Close()
	adapter, err := NewCuratedSourceAdapter(map[string]CuratedSourceConfig{"chosen": {SourceType: "xiaoyuzhou_episode", FeedURL: server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	adapter.client.Transport = server.Client().Transport
	sink := &curatedSink{}
	first, err := adapter.Execute(context.Background(), curatedRun("xiaoyuzhou_episode"), sink)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Execute(context.Background(), curatedRun("xiaoyuzhou_episode"), sink)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cursor != second.Cursor || sink.items[0].IdempotencyKey != sink.items[1].IdempotencyKey {
		t.Fatal("unstable identities")
	}
	if sink.items[0].Metadata["content_kind"] != "feed_excerpt" || strings.Contains(sink.items[0].Content, "retain") {
		t.Fatal("incorrect content coverage")
	}
	run := curatedRun("xiaoyuzhou_episode")
	run.Subscription.SourceAccountKey = "unauthorized"
	if _, err = adapter.Execute(context.Background(), run, sink); err == nil {
		t.Fatal("accepted unauthorized scope")
	}
}
func TestCuratedExportAllItemsValidatedBeforeEnqueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	bundle := CuratedSourceBundle{SchemaVersion: "curated_source.v1", SourceType: "x_post", AccountKey: "chosen", Items: []CuratedSourceItem{{ID: "one", Title: "Synthetic", URL: "https://example.org/one", Content: strings.Repeat("synthetic ", 10), ContentKind: "excerpt"}}}
	save := func() {
		raw, _ := json.Marshal(bundle)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	adapter, err := NewCuratedSourceAdapter(map[string]CuratedSourceConfig{"chosen": {SourceType: "x_post", ExportFile: path}})
	if err != nil {
		t.Fatal(err)
	}
	sink := &curatedSink{}
	if _, err = adapter.Execute(context.Background(), curatedRun("x_post"), sink); err != nil {
		t.Fatal(err)
	}
	first := sink.items[0].IdempotencyKey
	bundle.Items[0].Content += " updated"
	save()
	if _, err = adapter.Execute(context.Background(), curatedRun("x_post"), sink); err != nil {
		t.Fatal(err)
	}
	if first == sink.items[1].IdempotencyKey || sink.items[0].SourceItemID != sink.items[1].SourceItemID {
		t.Fatal("revision identity failure")
	}
	bundle.Items = append(bundle.Items, CuratedSourceItem{ID: "bad"})
	save()
	sink.items = nil
	if _, err = adapter.Execute(context.Background(), curatedRun("x_post"), sink); err == nil || len(sink.items) != 0 {
		t.Fatal("partially queued invalid export")
	}
	bundle.Items = bundle.Items[:1]
	bundle.AccountKey = "other"
	save()
	if _, err = adapter.Execute(context.Background(), curatedRun("x_post"), sink); err == nil {
		t.Fatal("accepted wrong export owner")
	}
}
func TestCuratedRSSRejectsRedirectAndLoginPage(t *testing.T) {
	calls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	adapter, _ := NewCuratedSourceAdapter(map[string]CuratedSourceConfig{"chosen": {SourceType: "rss_entry", FeedURL: server.URL}})
	adapter.client.Transport = server.Client().Transport
	if _, err := adapter.Execute(context.Background(), curatedRun("rss_entry"), &curatedSink{}); err == nil || calls != 0 {
		t.Fatal("redirect followed")
	}
	if _, err := parseCuratedRSS([]byte(`<html>login required</html>`)); err == nil {
		t.Fatal("login page accepted as RSS")
	}
}

func TestCuratedCoverageSurvivesKnowledgePackage(t *testing.T) {
	envelope := SourceArticleEnvelope{SourceType: "xiaoyuzhou_episode", SourceItemID: "item", SourceAccountID: "source", Title: "Synthetic", SourceURL: "https://example.org/episode", Content: strings.Repeat("synthetic description ", 5), Metadata: map[string]string{"content_kind": "feed_excerpt", "collection_method": "rss"}}
	pkg := buildSourceArticlePackage(envelope, "hash", "book", "", "now")
	if len(pkg.Citations) == 0 || !strings.Contains(pkg.Citations[0].Note, "content_kind=feed_excerpt") {
		t.Fatal("coverage discarded")
	}
	envelope.Metadata["content_kind"] = "transcript"
	if !sourceCoverageChanged(&pkg, envelope) {
		t.Fatal("coverage change ignored")
	}
	updated := updateSourceArticlePackageMetadata(pkg, envelope, "later")
	if !strings.Contains(updated.Citations[0].Note, "content_kind=transcript") {
		t.Fatal("coverage not updated")
	}
}

func TestCuratedIngestKeepsBodyAndPackageHashesDistinct(t *testing.T) {
	root := t.TempDir()
	books := NewBookKnowledgeStore(root)
	syncStore, err := newSourceSyncStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer syncStore.Close()
	sub := createSourceIngestSubscription(t, syncStore)
	run := createRunningSourceIngestRun(t, syncStore, sub.ID, "agent-a")
	envelope := SourceArticleEnvelope{IdempotencyKey: "curated-test", SourceType: "rss_entry", SourceAccountID: "chosen", SourceItemID: "item", Title: "Synthetic", SourceURL: "https://example.org/entry", Content: strings.Repeat("synthetic content ", 20), Metadata: map[string]string{"content_kind": "feed_excerpt", "collection_method": "rss", "hash_strategy": "canonical_package"}}
	ingestor := NewSourceIngestService(books, syncStore)
	receipt, err := ingestor.IngestArticle(run.ID, "agent-a", envelope)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := books.LoadPackage(receipt.TargetBookID)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		t.Fatal(err)
	}
	if hash != pkg.Book.ContentHash || hash == receipt.ContentHash {
		t.Fatal("body/package hash identity is wrong")
	}
	analysis, err := books.LoadAnalysisManifest(receipt.TargetBookID)
	if err != nil || analysis.ContentHash != hash {
		t.Fatal("analysis not pinned to package hash")
	}
	release := &KnowledgeRelease{ReleaseID: "synthetic", ContentHash: hash, BookID: pkg.Book.BookID, UsagePolicy: "standard", Citations: pkg.Citations}
	snapshot, err := buildReleaseEvidence(release, pkg)
	if err != nil || snapshot.Items[0].Status != "available" {
		t.Fatal("source cannot be read back")
	}
	envelope.IdempotencyKey = "curated-test-coverage"
	envelope.Metadata["content_kind"] = "transcript"
	if _, err = ingestor.IngestArticle(run.ID, "agent-a", envelope); err != nil {
		t.Fatal(err)
	}
	updated, err := books.LoadPackage(receipt.TargetBookID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Book.ContentHash == hash {
		t.Fatal("coverage change did not invalidate package identity")
	}
}

func TestCuratedRejectsOversizedDocumentAndMissingChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", curatedMaxBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewCuratedSourceAdapter(map[string]CuratedSourceConfig{"chosen": {SourceType: "x_post", ExportFile: path}})
	if err != nil {
		t.Fatal(err)
	}
	sink := &curatedSink{}
	if _, err = adapter.Execute(context.Background(), curatedRun("x_post"), sink); err == nil || !strings.Contains(err.Error(), "4 MiB") || len(sink.items) != 0 {
		t.Fatal("oversized source accepted")
	}
	if _, err := parseCuratedRSS([]byte(`<rss version="2.0"></rss>`)); err == nil {
		t.Fatal("missing channel accepted")
	}
}
