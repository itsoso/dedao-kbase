package app

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func evidenceTestRelease(t *testing.T, highRisk bool) (*BookKnowledgeStore, *KnowledgeRelease) {
	t.Helper()
	store := qualityTestStore(t)
	pkg, err := store.LoadPackage("42")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := BookKnowledgeContentHash(*pkg)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Book.ContentHash = hash
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	analysis, err := store.LoadAnalysisManifest("42")
	if err != nil {
		t.Fatal(err)
	}
	analysis.ContentHash = hash
	if highRisk {
		analysis.Payload.Claims[0].RiskLevel = "high"
	}
	if err := store.SaveAnalysisManifest(*analysis); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	release, err := PublishKnowledgeRelease(store, "42")
	if err != nil {
		t.Fatal(err)
	}
	return store, release
}

func TestReleaseEvidencePinnedAfterPackageChanges(t *testing.T) {
	store, release := evidenceTestRelease(t, false)
	info, err := os.Stat(store.KnowledgeReleasePath(release.ReleaseID) + ".evidence")
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unsafe evidence permissions: %v", err)
	}
	cid := release.Citations[0].CitationID
	before, err := store.ResolveReleaseEvidence(release.ReleaseID, release.ContentHash, cid)
	if err != nil || before.Status != "available" || before.Excerpt == "" {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	pkg, _ := store.LoadPackage("42")
	pkg.Chunks[0].Text = "newer text must not replace historical evidence"
	if err := store.SavePackage(*pkg); err != nil {
		t.Fatal(err)
	}
	after, err := store.ResolveReleaseEvidence(release.ReleaseID, release.ContentHash, cid)
	if err != nil || after.Excerpt != before.Excerpt {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	if _, err := store.ResolveReleaseEvidence(release.ReleaseID, "wrong", cid); err == nil {
		t.Fatal("accepted wrong hash")
	}
	if _, err := store.ResolveReleaseEvidence(release.ReleaseID, release.ContentHash, "other"); !os.IsNotExist(err) {
		t.Fatalf("wrong citation: %v", err)
	}
	if err := os.Remove(store.KnowledgeReleasePath(release.ReleaseID) + ".evidence"); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.ResolveReleaseEvidence(release.ReleaseID, release.ContentHash, cid)
	if err != nil || legacy.Status != "unavailable" || legacy.Excerpt != "" {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
}

func TestReleaseEvidencePolicyAndHTTPAuthentication(t *testing.T) {
	store, release := evidenceTestRelease(t, true)
	cid := release.Citations[0].CitationID
	item, err := store.ResolveReleaseEvidence(release.ReleaseID, release.ContentHash, cid)
	if err != nil || item.Status != "metadata_only" || item.Excerpt != "" {
		t.Fatalf("item=%+v err=%v", item, err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "test-token"})
	path := "/api/knowledge/releases/" + release.ReleaseID + "/evidence?content_hash=" + release.ContentHash + "&citation_id=" + cid
	denied := requestKBase(handler, http.MethodGet, path, "")
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("auth status=%d", denied.Code)
	}
	allowed := requestKBase(handler, http.MethodGet, path, "test-token")
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), "metadata_only") {
		t.Fatalf("response=%s", allowed.Body.String())
	}
	if allowed.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("missing private header")
	}
}
