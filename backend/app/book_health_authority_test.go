package app

import (
	"reflect"
	"testing"
)

func TestBuildHealthAuthorityPackDowngradesHighRiskClaims(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthAuthorityPackFixture(t, store)

	pack, err := store.BuildHealthAuthorityPack(0)
	if err != nil {
		t.Fatalf("BuildHealthAuthorityPack returned error: %v", err)
	}
	if pack.ConsumerContract != HealthAuthorityPackContractV1 {
		t.Fatalf("ConsumerContract = %q, want %q", pack.ConsumerContract, HealthAuthorityPackContractV1)
	}
	if pack.BasePackID == "" || pack.SourceFingerprint == "" {
		t.Fatalf("base evidence metadata is incomplete: %#v", pack)
	}

	record := findHealthAuthorityPackRecord(t, pack, "dedao:verify-book:verify-claim-medication")
	if record.RiskTier == bookKnowledgeRiskAutoUsable {
		t.Fatalf("RiskTier = %q, want downgraded health-sensitive claim", record.RiskTier)
	}
	if record.Decision == bookKnowledgeDecisionAllow {
		t.Fatalf("Decision = %q, want non-actionable decision", record.Decision)
	}
	if record.CandidateType == "action_support_candidate" {
		t.Fatalf("CandidateType = %q, Dedao-only health claims must not support action", record.CandidateType)
	}
}

func TestBuildHealthAuthorityPackKeepsStableSourceRefs(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthAuthorityPackFixture(t, store)

	first, err := store.BuildHealthAuthorityPack(0)
	if err != nil {
		t.Fatalf("first BuildHealthAuthorityPack returned error: %v", err)
	}
	second, err := store.BuildHealthAuthorityPack(0)
	if err != nil {
		t.Fatalf("second BuildHealthAuthorityPack returned error: %v", err)
	}

	firstRecord := findHealthAuthorityPackRecord(t, first, "dedao:verify-book:verify-claim-medication")
	secondRecord := findHealthAuthorityPackRecord(t, second, "dedao:verify-book:verify-claim-medication")
	if first.SourceFingerprint == "" || first.SourceFingerprint != second.SourceFingerprint {
		t.Fatalf("SourceFingerprint changed between builds: %q != %q", first.SourceFingerprint, second.SourceFingerprint)
	}
	if firstRecord.BookID != "verify-book" {
		t.Fatalf("BookID = %q, want verify-book", firstRecord.BookID)
	}
	if firstRecord.EvidenceID != "dedao:verify-book:verify-claim-medication" {
		t.Fatalf("EvidenceID = %q, want stable base evidence id", firstRecord.EvidenceID)
	}
	if firstRecord.ChapterID != "verify-chapter-1" {
		t.Fatalf("ChapterID = %q, want verify-chapter-1", firstRecord.ChapterID)
	}
	if firstRecord.SourceHash == "" {
		t.Fatal("SourceHash is empty")
	}
	if firstRecord.SourceHash != secondRecord.SourceHash {
		t.Fatalf("SourceHash changed between builds: %q != %q", firstRecord.SourceHash, secondRecord.SourceHash)
	}
	if !reflect.DeepEqual(firstRecord.Citations, []string{"verify-citation-2"}) {
		t.Fatalf("Citations = %#v, want verify-citation-2", firstRecord.Citations)
	}
	if !reflect.DeepEqual(firstRecord.Citations, secondRecord.Citations) {
		t.Fatalf("Citations changed between builds: %#v != %#v", firstRecord.Citations, secondRecord.Citations)
	}
}

func TestBuildHealthAuthorityPackAddsReviewMetadata(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthAuthorityPackFixture(t, store)

	pack, err := store.BuildHealthAuthorityPack(0)
	if err != nil {
		t.Fatalf("BuildHealthAuthorityPack returned error: %v", err)
	}

	study := findHealthAuthorityPackRecord(t, pack, "dedao:verify-book:verify-claim-study")
	if study.SourceRefs.SourceHash != study.SourceHash {
		t.Fatalf("SourceRefs.SourceHash = %q, want flat SourceHash %q", study.SourceRefs.SourceHash, study.SourceHash)
	}
	if study.SourceRefs.BookID != study.BookID || study.SourceRefs.ChapterID != study.ChapterID || study.SourceRefs.ClaimID != study.ClaimID {
		t.Fatalf("SourceRefs do not mirror stable identity fields: %#v", study.SourceRefs)
	}
	if study.SourceRefs.SourceType != verifiedEvidenceSourceTypeDedaoBook || study.SourceRefs.SourceID != study.BookID {
		t.Fatalf("SourceRefs do not preserve base source identity: %#v", study.SourceRefs)
	}
	if !reflect.DeepEqual(study.SourceRefs.Citations, study.Citations) {
		t.Fatalf("SourceRefs.Citations = %#v, want %#v", study.SourceRefs.Citations, study.Citations)
	}
	if study.ReviewStatus != "needs_review" {
		t.Fatalf("ReviewStatus = %q, want needs_review", study.ReviewStatus)
	}
	if study.RiskReason != "dedao_educational_source" {
		t.Fatalf("RiskReason = %q, want dedao_educational_source", study.RiskReason)
	}
	if !stringSliceContains(study.EntityCandidates, "学习复盘") {
		t.Fatalf("EntityCandidates = %#v, want 学习复盘", study.EntityCandidates)
	}

	medication := findHealthAuthorityPackRecord(t, pack, "dedao:verify-book:verify-claim-medication")
	if medication.ReviewStatus != "blocked" {
		t.Fatalf("ReviewStatus = %q, want blocked", medication.ReviewStatus)
	}
	if medication.RiskReason != "medical_action_boundary" {
		t.Fatalf("RiskReason = %q, want medical_action_boundary", medication.RiskReason)
	}
	if !stringSliceContains(medication.EntityCandidates, "用药安全") {
		t.Fatalf("EntityCandidates = %#v, want 用药安全", medication.EntityCandidates)
	}
}

func TestBuildHealthAuthorityPackBlocksMedicationActionClaims(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthAuthorityPackFixture(t, store)

	pack, err := store.BuildHealthAuthorityPack(0)
	if err != nil {
		t.Fatalf("BuildHealthAuthorityPack returned error: %v", err)
	}

	record := findHealthAuthorityPackRecord(t, pack, "dedao:verify-book:verify-claim-medication")
	for _, use := range []string{"diagnosis", "treatment", "dosage", "medication_change", "emergency_guidance"} {
		if !stringSliceContains(record.BlockedUses, use) {
			t.Fatalf("BlockedUses = %#v, want %q", record.BlockedUses, use)
		}
	}
	for _, item := range pack.Items {
		if item.CandidateType == "action_support_candidate" {
			t.Fatalf("ClaimID %q returned action_support_candidate", item.ClaimID)
		}
	}
}

func saveHealthAuthorityPackFixture(t *testing.T, store *BookKnowledgeStore) {
	t.Helper()
	if err := store.SavePackage(sampleHealthAuthorityPackPackage()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
}

func findHealthAuthorityPackRecord(t *testing.T, pack *HealthAuthorityPack, claimID string) HealthAuthorityPackRecord {
	t.Helper()
	for _, item := range pack.Items {
		if item.ClaimID == claimID {
			return item
		}
	}
	t.Fatalf("ClaimID %q not found in authority pack: %#v", claimID, pack.Items)
	return HealthAuthorityPackRecord{}
}

func sampleHealthAuthorityPackPackage() BookKnowledgePackage {
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:     "verify-book",
			Title:      "验证能力测试书",
			SourceHTML: "/tmp/verify-book.html",
			Status:     "draft",
		},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "verify-chapter-1", BookID: "verify-book", Order: 1, Title: "验证章节", Summary: "验证章节摘要"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "verify-chunk-1", BookID: "verify-book", ChapterID: "verify-chapter-1", Order: 1, Text: "稳定复盘可以帮助学习者识别错误模式，并把行为观察转成后续讨论的问题。"},
			{ChunkID: "verify-chunk-2", BookID: "verify-book", ChapterID: "verify-chapter-1", Order: 2, Text: "具体用药剂量必须由医生结合个体情况判断，不能根据单一书摘自行调整药物。"},
		},
		Claims: []BookKnowledgeClaim{
			{
				ClaimID:       "verify-claim-study",
				BookID:        "verify-book",
				ChapterID:     "verify-chapter-1",
				Title:         "复盘提高学习质量",
				Summary:       "稳定复盘可以帮助学习者识别错误模式。",
				EvidenceLevel: "B",
				Confidence:    0.92,
				ReviewStatus:  "draft",
				Citations:     []string{"verify-citation-1"},
			},
			{
				ClaimID:       "verify-claim-medication",
				BookID:        "verify-book",
				ChapterID:     "verify-chapter-1",
				Title:         "用药剂量需要个体判断",
				Summary:       "具体用药剂量必须由医生结合个体情况判断。",
				EvidenceLevel: "B",
				Confidence:    0.88,
				ReviewStatus:  "draft",
				Citations:     []string{"verify-citation-2"},
			},
		},
		Citations: []BookKnowledgeCitation{
			{CitationID: "verify-citation-1", BookID: "verify-book", ChapterID: "verify-chapter-1", ChunkID: "verify-chunk-1", SourceHTML: "/tmp/verify-book.html"},
			{CitationID: "verify-citation-2", BookID: "verify-book", ChapterID: "verify-chapter-1", ChunkID: "verify-chunk-2", SourceHTML: "/tmp/verify-book.html"},
		},
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
