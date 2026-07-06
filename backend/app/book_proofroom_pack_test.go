package app

import "testing"

func TestBuildProofroomArgumentPackFromEvidencePack(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	pack, err := store.BuildProofroomArgumentPack(10)
	if err != nil {
		t.Fatalf("BuildProofroomArgumentPack returned error: %v", err)
	}
	if pack.ConsumerContract != ProofroomArgumentPackContractV1 {
		t.Fatalf("ConsumerContract = %q, want %q", pack.ConsumerContract, ProofroomArgumentPackContractV1)
	}
	if pack.ProjectID != BookKnowledgeProjectProofroom || pack.TargetSystem != "proofroom" {
		t.Fatalf("proofroom pack identity = %#v", pack)
	}
	if pack.BasePackID == "" || pack.SourceFingerprint == "" {
		t.Fatalf("base evidence metadata is incomplete: %#v", pack)
	}
	if pack.ItemCount != 3 || len(pack.Items) != 3 {
		t.Fatalf("item count = %d/%d, want 3", pack.ItemCount, len(pack.Items))
	}
	if pack.ReviewCount == 0 || pack.ContradictionCandidateCount == 0 {
		t.Fatalf("review/contradiction counts are incomplete: %#v", pack)
	}

	study := findProofroomArgumentPackRecord(t, pack, "dedao:verify-book:verify-claim-study")
	if study.SourceRefs.SourceType != verifiedEvidenceSourceTypeDedaoBook ||
		study.SourceRefs.SourceID != "verify-book" ||
		len(study.SourceRefs.Citations) == 0 {
		t.Fatalf("study source refs are incomplete: %#v", study.SourceRefs)
	}
	if !stringSliceContains(study.ArgumentRoles, "claim") ||
		!stringSliceContains(study.ArgumentRoles, "support") {
		t.Fatalf("study roles = %#v, want claim and support", study.ArgumentRoles)
	}

	unsupported := findProofroomArgumentPackRecord(t, pack, "dedao:verify-book:verify-claim-unsupported")
	if unsupported.ReviewStatus != "needs_source_review" {
		t.Fatalf("unsupported ReviewStatus = %q, want needs_source_review", unsupported.ReviewStatus)
	}
	if !unsupported.ContradictionCandidate || !stringSliceContains(unsupported.ArgumentRoles, "question") {
		t.Fatalf("unsupported contradiction/roles = %v %#v", unsupported.ContradictionCandidate, unsupported.ArgumentRoles)
	}
	if stringSliceContains(unsupported.ArgumentRoles, "support") {
		t.Fatalf("unsupported roles must not include support: %#v", unsupported.ArgumentRoles)
	}
}

func findProofroomArgumentPackRecord(t *testing.T, pack *ProofroomArgumentPack, evidenceID string) ProofroomArgumentPackRecord {
	t.Helper()
	for _, item := range pack.Items {
		if item.EvidenceID == evidenceID {
			return item
		}
	}
	t.Fatalf("evidence_id %q not found in proofroom pack: %#v", evidenceID, pack.Items)
	return ProofroomArgumentPackRecord{}
}
