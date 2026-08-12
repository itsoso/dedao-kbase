package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEvolutionCandidateSaveIsImmutableAndIdempotent(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createGeneratingEvolutionCandidateRun(t, store, "candidate-save")
	input := EvolutionCandidateInput{
		IdempotencyKey:   "11111111-1111-4111-8111-111111111111",
		RunID:            run.RunID,
		CandidateType:    EvolutionCandidateAgentCompilation,
		BaselineIdentity: "agent:v1@release:a",
		ChangeSummary:    "Pin the candidate to the approved release set.",
		GeneratorVersion: "agent-generator.v1",
		Artifact: map[string]any{
			"mode":        "study",
			"package_id":  "agent-v2",
			"release_ids": []string{"release-a"},
		},
	}

	first, created, err := store.SaveEvolutionCandidate(input)
	if err != nil || !created {
		t.Fatalf("save candidate = %#v, %v, %v", first, created, err)
	}
	second, created, err := store.SaveEvolutionCandidate(input)
	if err != nil || created || second.CandidateID != first.CandidateID {
		t.Fatalf("replay candidate = %#v, %v, %v", second, created, err)
	}

	changed := input
	changed.Artifact = map[string]any{"mode": "study", "package_id": "agent-v3", "release_ids": []string{"release-a"}}
	if _, _, err := store.SaveEvolutionCandidate(changed); !errors.Is(err, ErrEvolutionIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}

	loaded, artifact, err := store.LoadEvolutionCandidate(first.CandidateID)
	if err != nil || loaded.ContentHash != first.ContentHash || string(artifact) == "" {
		t.Fatalf("load candidate = %#v, %q, %v", loaded, artifact, err)
	}
	run, err = store.LoadRun(run.RunID)
	if err != nil || run.CurrentCandidateID != first.CandidateID {
		t.Fatalf("run candidate = %#v, %v", run, err)
	}
}

func TestEvolutionCandidateHashBindsRuntimeFields(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createGeneratingEvolutionCandidateRun(t, store, "candidate-hash")
	base := EvolutionCandidateInput{
		IdempotencyKey:   "22222222-2222-4222-8222-222222222222",
		RunID:            run.RunID,
		CandidateType:    EvolutionCandidateAgentCompilation,
		BaselineIdentity: "agent:v1@release:a",
		ChangeSummary:    "Generate a bounded candidate.",
		GeneratorVersion: "agent-generator.v1",
		Artifact:         map[string]any{"package_id": "agent-v2", "tools": []string{"search"}},
	}
	first, _, err := store.SaveEvolutionCandidate(base)
	if err != nil {
		t.Fatal(err)
	}

	run2 := createGeneratingEvolutionCandidateRun(t, store, "candidate-hash-2")
	changed := base
	changed.IdempotencyKey = "33333333-3333-4333-8333-333333333333"
	changed.RunID = run2.RunID
	changed.GeneratorVersion = "agent-generator.v2"
	second, _, err := store.SaveEvolutionCandidate(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash == second.ContentHash {
		t.Fatal("generator version did not affect candidate content hash")
	}
}

func TestEvolutionCandidateDetectsArtifactTampering(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createGeneratingEvolutionCandidateRun(t, store, "candidate-tamper")
	input := EvolutionCandidateInput{
		IdempotencyKey:   "44444444-4444-4444-8444-444444444444",
		RunID:            run.RunID,
		CandidateType:    EvolutionCandidateKnowledgeRelease,
		BaselineIdentity: "release:old",
		ChangeSummary:    "Reverify the immutable release.",
		GeneratorVersion: "knowledge-generator.v1",
		Artifact:         map[string]any{"release_id": "release-new", "content_hash": "sha256:abc"},
	}
	candidate, _, err := store.SaveEvolutionCandidate(input)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.evolutionCandidateArtifactPath(candidate.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadEvolutionCandidate(candidate.CandidateID); !errors.Is(err, ErrEvolutionCandidateArtifactConflict) {
		t.Fatalf("tampered artifact error = %v", err)
	}
}

func TestEvolutionCandidatePrivacyFailureDoesNotPersistArtifactOrRow(t *testing.T) {
	store := newEvolutionTestStore(t)
	run := createGeneratingEvolutionCandidateRun(t, store, "candidate-privacy")
	input := EvolutionCandidateInput{
		IdempotencyKey:   "55555555-5555-4555-8555-555555555555",
		RunID:            run.RunID,
		CandidateType:    EvolutionCandidateAgentCompilation,
		BaselineIdentity: "agent:v1@release:a",
		ChangeSummary:    "Candidate must fail before persistence.",
		GeneratorVersion: "agent-generator.v1",
		Artifact:         map[string]any{"api_key": "private-candidate-value"},
	}

	if _, _, err := store.SaveEvolutionCandidate(input); err == nil {
		t.Fatal("sensitive candidate was accepted")
	}
	var candidates int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM evolution_candidates`).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 0 {
		t.Fatalf("persisted candidates = %d", candidates)
	}
	artifactRoot := filepath.Join(filepath.Dir(store.dbPath), "evolution_artifacts")
	if _, err := os.Stat(artifactRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate artifact root exists after privacy failure: %v", err)
	}
}

func createGeneratingEvolutionCandidateRun(t *testing.T, store *EvolutionControlStore, key string) *EvolutionRun {
	t.Helper()
	run, created, err := store.CreateRun(validEvolutionRunInput(key))
	if err != nil || !created {
		t.Fatalf("create run = %#v, %v, %v", run, created, err)
	}
	for _, status := range []EvolutionRunStatus{EvolutionTriaged, EvolutionGenerating} {
		run, err = store.TransitionRun(run.RunID, status, EvolutionTransitionInput{Actor: "test", Code: string(status)})
		if err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	return run
}
