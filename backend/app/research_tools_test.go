package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestResearchKnowledgeToolPinsPackageReleaseCitationsAndAudits(t *testing.T) {
	knowledge, pkg := agentRuntimeTestStore(t)
	research, err := OpenResearchStore(knowledge.Root(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = research.Close() })
	run := createResearchToolRun(t, research, pkg, "knowledge-tool")
	registry, err := NewResearchToolRegistry(knowledge, research)
	if err != nil {
		t.Fatal(err)
	}

	result, err := registry.Execute(context.Background(), ResearchToolSearchKnowledge, ResearchToolRequest{
		RunID: run.RunID, PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Arguments: map[string]any{"query": "grounded", "limit": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Package.PackageID != pkg.PackageID || result.Package.PackageVersion != pkg.Version ||
		result.Package.PackageHash != pkg.ContentHash {
		t.Fatalf("package scope = %#v", result.Package)
	}
	if len(result.Package.ReleaseHashes) != len(pkg.Releases) {
		t.Fatalf("release hashes = %#v", result.Package.ReleaseHashes)
	}
	if len(result.Knowledge) == 0 || len(result.Knowledge[0].CitationIDs) == 0 {
		t.Fatalf("knowledge result is not citation grounded: %#v", result.Knowledge)
	}

	item := result.Knowledge[0]
	fetched, err := registry.Execute(context.Background(), ResearchToolFetchKnowledgeEvidence, ResearchToolRequest{
		RunID: run.RunID, PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Arguments: map[string]any{
			"release_id":  item.ReleaseID,
			"claim_id":    item.ClaimID,
			"citation_id": item.CitationIDs[0],
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Citations) != 1 || fetched.Citations[0].CitationID != item.CitationIDs[0] ||
		len(fetched.PromotedEvidence) != 1 || fetched.PromotedEvidence[0].Locator.ReleaseID != item.ReleaseID {
		t.Fatalf("fetched evidence = %#v", fetched)
	}

	audits, err := research.ListResearchToolAudits(run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 2 || audits[0].ToolName != ResearchToolSearchKnowledge ||
		audits[0].ArgumentFingerprint == "" || audits[0].ResultFingerprint == "" ||
		audits[0].PolicyDecision != ResearchToolPolicyAllow || len(audits[1].PromotedEvidenceIDs) != 1 {
		t.Fatalf("tool audits = %#v", audits)
	}
	auditJSON, err := json.Marshal(audits)
	if err != nil {
		t.Fatal(err)
	}
	for _, privateValue := range []string{"grounded", item.ClaimID, item.CitationIDs[0]} {
		if strings.Contains(string(auditJSON), privateValue) {
			t.Fatalf("audit leaked raw argument %q: %s", privateValue, auditJSON)
		}
	}
}

func TestResearchKnowledgeToolFailsClosedWhenPinnedReleaseChanges(t *testing.T) {
	knowledge, pkg := agentRuntimeTestStore(t)
	research, err := OpenResearchStore(knowledge.Root(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = research.Close() })
	run := createResearchToolRun(t, research, pkg, "changed-release")
	registry, err := NewResearchToolRegistry(knowledge, research)
	if err != nil {
		t.Fatal(err)
	}

	release, err := knowledge.LoadKnowledgeRelease(pkg.Releases[0].ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	release.ContentHash = "sha256:" + strings.Repeat("f", 64)
	payload, err := json.MarshalIndent(release, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(knowledge.KnowledgeReleasePath(release.ReleaseID), append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = registry.Execute(context.Background(), ResearchToolSearchKnowledge, ResearchToolRequest{
		RunID: run.RunID, PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Arguments: map[string]any{"query": "grounded"},
	})
	if err == nil || !strings.Contains(err.Error(), "content hash changed") {
		t.Fatalf("changed release error = %v", err)
	}
	audits, auditErr := research.ListResearchToolAudits(run.RunID)
	if auditErr != nil {
		t.Fatal(auditErr)
	}
	if len(audits) != 1 || audits[0].Outcome != ResearchToolOutcomeFailed || audits[0].ResultFingerprint == "" {
		t.Fatalf("failed audit = %#v", audits)
	}
}

func TestResearchPriorRunToolPromotesVerifiedConclusionAndRequiresVerificationForUnderlyingPrivateExcerpt(t *testing.T) {
	knowledge, pkg := agentRuntimeTestStore(t)
	research, err := OpenResearchStore(knowledge.Root(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = research.Close() })
	registry, err := NewResearchToolRegistry(knowledge, research)
	if err != nil {
		t.Fatal(err)
	}

	verifiedRun := createResearchToolRun(t, research, pkg, "prior-verified")
	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceChatlog},
		Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceChatlog, SourceRole: ResearchEvidenceRoleUserHistory,
			Content: "Synthetic private history", Privacy: ResearchEvidencePrivacyPrivate, Selected: true,
			Locator: ResearchEvidenceLocator{WorkerID: "worker-1", ConversationRef: "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451", MessageRef: "sha256:5a01731f9d22d0e8243e4f3f5170b8710d35a48a49bf1090962a7a37efa94451"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	verifiedRunPointer, err := research.StoreEvidenceBundle(verifiedRun.RunID, verifiedRun.Version, bundle)
	if err != nil {
		t.Fatal(err)
	}
	verifiedRun = *verifiedRunPointer
	if _, err := research.db.Exec(`INSERT INTO research_conclusions
		(conclusion_id, run_id, conclusion_text, evidence_ids_json, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "conclusion-verified", verifiedRun.RunID, "Verified synthetic conclusion",
		`["`+bundle.Evidence[0].EvidenceID+`"]`, 0.9, verifiedRun.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := research.db.Exec(`UPDATE research_runs SET status = ? WHERE run_id = ?`, ResearchCompleted, verifiedRun.RunID); err != nil {
		t.Fatal(err)
	}

	unverifiedRun := createResearchToolRun(t, research, pkg, "prior-unverified")
	if _, err := research.db.Exec(`INSERT INTO research_conclusions
		(conclusion_id, run_id, conclusion_text, evidence_ids_json, confidence, created_at)
		VALUES (?, ?, ?, '[]', ?, ?)`, "conclusion-unverified", unverifiedRun.RunID,
		"Unverified synthetic conclusion", 0.8, unverifiedRun.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	current := createResearchToolRun(t, research, pkg, "prior-current")

	withoutVerification, err := registry.Execute(context.Background(), ResearchToolSearchPriorRuns, ResearchToolRequest{
		RunID: current.RunID, PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Arguments: map[string]any{"query": "synthetic conclusion", "limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutVerification.PriorConclusions) != 1 || withoutVerification.PriorConclusions[0].RunID != verifiedRun.RunID ||
		len(withoutVerification.PromotedEvidence) != 1 ||
		withoutVerification.PromotedEvidence[0].Locator.MessageRef != "conclusion-verified" {
		t.Fatalf("prior results without verification = %#v", withoutVerification)
	}

	withVerification, err := registry.Execute(context.Background(), ResearchToolSearchPriorRuns, ResearchToolRequest{
		RunID: current.RunID, PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Arguments: map[string]any{
			"query": "synthetic conclusion",
			"verified_locators": []any{map[string]any{
				"locator_hash": bundle.Evidence[0].LocatorHash,
				"content_hash": bundle.Evidence[0].ContentHash,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withVerification.PromotedEvidence) != 2 ||
		withVerification.PromotedEvidence[0].SourceType != ResearchEvidenceSourcePriorRun ||
		withVerification.PromotedEvidence[0].Locator.PriorRunID != verifiedRun.RunID {
		t.Fatalf("verified prior evidence = %#v", withVerification.PromotedEvidence)
	}
	foreignCurrent := createResearchToolRun(t, research, pkg, "prior-foreign-owner")
	if _, err := research.db.Exec(`INSERT INTO research_http_owners(run_id, owner_hash, created_at) VALUES (?, ?, ?), (?, ?, ?)`,
		verifiedRun.RunID, "owner-a", verifiedRun.UpdatedAt, foreignCurrent.RunID, "owner-b", foreignCurrent.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	foreignResult, err := registry.Execute(context.Background(), ResearchToolSearchPriorRuns, ResearchToolRequest{
		RunID: foreignCurrent.RunID, PackageID: pkg.PackageID, PackageVersion: pkg.Version,
		Arguments: map[string]any{"query": "synthetic conclusion", "limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(foreignResult.PriorConclusions) != 0 || len(foreignResult.PromotedEvidence) != 0 {
		t.Fatalf("cross-owner prior result=%#v", foreignResult)
	}
}

func createResearchToolRun(t *testing.T, store *ResearchStore, pkg AgentPackage, key string) ResearchRun {
	t.Helper()
	input := researchStoreTestInput(key)
	input.Request.PackageID = pkg.PackageID
	input.Request.PackageVersion = pkg.Version
	run, _, err := store.CreateRun(input)
	if err != nil {
		t.Fatal(err)
	}
	return *run
}
