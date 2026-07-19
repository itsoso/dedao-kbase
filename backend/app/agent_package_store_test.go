package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentPackageStorePublishesAtomicallyAndIdempotently(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	published, created, err := PublishAgentPackage(store, pkg, "operator:package:1", AgentReadOnlyToolIDs(), now)
	if err != nil {
		t.Fatalf("PublishAgentPackage() error = %v", err)
	}
	if !created || published.LifecycleState != AgentPackagePublished || published.PublishedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("published package = %#v, created=%v", published, created)
	}
	if _, err := os.Stat(store.AgentPackagePath(published.ContentHash)); err != nil {
		t.Fatalf("package artifact was not committed: %v", err)
	}
	if _, err := os.Stat(store.AgentPackageManifestPath()); err != nil {
		t.Fatalf("package manifest was not committed: %v", err)
	}
	temporary, err := filepath.Glob(filepath.Join(store.AgentPackageDir(), ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("atomic publication left temporary files: %v", temporary)
	}

	replayed, created, err := PublishAgentPackage(store, pkg, "operator:package:1", AgentReadOnlyToolIDs(), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("idempotent replay error = %v", err)
	}
	if created || replayed.ContentHash != published.ContentHash || replayed.PublishedAt != published.PublishedAt {
		t.Fatalf("idempotent replay changed publication: %#v, created=%v", replayed, created)
	}

	changed := validAgentPackage()
	changed.Version = "2.0.0"
	changed, err = FinalizeAgentPackage(changed)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, changed)
	if _, _, err := PublishAgentPackage(store, changed, "operator:package:1", AgentReadOnlyToolIDs(), now); !errors.Is(err, ErrAgentPackageIdempotencyConflict) {
		t.Fatalf("reused idempotency key error = %v", err)
	}
}

func TestAgentPackageStoreSupersedesVersionsWithoutMutatingArtifacts(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	knownTools := AgentReadOnlyToolIDs()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	first, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, first)
	firstPublished, _, err := PublishAgentPackage(store, first, "publish-v1", knownTools, now)
	if err != nil {
		t.Fatal(err)
	}
	first = *firstPublished
	firstArtifact, err := os.ReadFile(store.AgentPackagePath(first.ContentHash))
	if err != nil {
		t.Fatal(err)
	}

	secondInput := validAgentPackage()
	secondInput.Version = "1.1.0"
	secondInput.UIManifest.Capabilities = append(secondInput.UIManifest.Capabilities, "quiz")
	second, err := FinalizeAgentPackage(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, second)
	secondPublished, created, err := PublishAgentPackage(store, second, "publish-v2", knownTools, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	second = *secondPublished
	if !created || second.Supersedes != "agent-package-example@1.0.0" {
		t.Fatalf("second publication = %#v, created=%v", second, created)
	}
	afterSupersession, err := os.ReadFile(store.AgentPackagePath(first.ContentHash))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterSupersession) != string(firstArtifact) {
		t.Fatal("supersession mutated the immutable first package artifact")
	}

	records, err := store.ListAgentPackages("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].LifecycleState != AgentPackageSuperseded || records[1].LifecycleState != AgentPackagePublished {
		t.Fatalf("package records = %#v", records)
	}
	if records[0].URL != "/api/agent-packages/agent-package-example?version=1.0.0" ||
		records[1].URL != "/api/agent-packages/agent-package-example?version=1.1.0" {
		t.Fatalf("stable URLs = %#v", records)
	}
	latest, err := store.LoadAgentPackage("agent-package-example", "")
	if err != nil || latest.Version != "1.1.0" {
		t.Fatalf("latest package = %#v, err=%v", latest, err)
	}
	old, err := store.LoadAgentPackage("agent-package-example", "1.0.0")
	if err != nil || old.LifecycleState != AgentPackageSuperseded {
		t.Fatalf("old package = %#v, err=%v", old, err)
	}
	if err := ValidateAgentPackage(*old, store, knownTools); err != nil {
		t.Fatalf("superseded immutable package no longer validates: %v", err)
	}
}

func TestAgentPackageStoreRejectsMutableVersionReuse(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	knownTools := AgentReadOnlyToolIDs()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	first, _ := FinalizeAgentPackage(validAgentPackage())
	savePassingAgentPackageTestEvaluation(t, store, first)
	if _, _, err := PublishAgentPackage(store, first, "publish-a", knownTools, now); err != nil {
		t.Fatal(err)
	}

	changed := validAgentPackage()
	changed.UIManifest.Capabilities = append(changed.UIManifest.Capabilities, "quiz")
	changed, _ = FinalizeAgentPackage(changed)
	savePassingAgentPackageTestEvaluation(t, store, changed)
	if _, _, err := PublishAgentPackage(store, changed, "publish-b", knownTools, now.Add(time.Hour)); !errors.Is(err, ErrAgentPackageVersionConflict) {
		t.Fatalf("version reuse error = %v", err)
	}
}
