package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildVerifiedEvidencePackFromProjectCollection(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	first, err := store.BuildVerifiedEvidencePack(BookKnowledgeProjectHealth, 10)
	if err != nil {
		t.Fatalf("BuildVerifiedEvidencePack returned error: %v", err)
	}
	second, err := store.BuildVerifiedEvidencePack(BookKnowledgeProjectHealth, 10)
	if err != nil {
		t.Fatalf("second BuildVerifiedEvidencePack returned error: %v", err)
	}

	if first.ConsumerContract != VerifiedEvidencePackContractV1 {
		t.Fatalf("ConsumerContract = %q, want %q", first.ConsumerContract, VerifiedEvidencePackContractV1)
	}
	if first.PackID == "" || first.PackID != second.PackID {
		t.Fatalf("PackID must be stable for identical source; first=%q second=%q", first.PackID, second.PackID)
	}
	if first.SourceFingerprint == "" {
		t.Fatal("SourceFingerprint is required")
	}
	if first.QualitySummary.Total != 3 ||
		first.QualitySummary.Accepted == 0 ||
		first.QualitySummary.Assistive == 0 ||
		first.QualitySummary.MissingSourceRefs == 0 {
		t.Fatalf("quality summary missing expected counts: %#v", first.QualitySummary)
	}
	if len(first.Records) != 3 {
		t.Fatalf("record count = %d, want 3", len(first.Records))
	}

	record := first.Records[0]
	if !strings.HasPrefix(record.EvidenceID, "dedao:") {
		t.Fatalf("EvidenceID = %q, want dedao prefix", record.EvidenceID)
	}
	if record.SourceRefs.SourceType != "dedao_book_claim" ||
		record.SourceRefs.SourceID != "verify-book" ||
		record.SourceRefs.ClaimID == "" ||
		record.SourceRefs.SourceHash == "" ||
		len(record.SourceRefs.Citations) == 0 {
		t.Fatalf("source refs are incomplete: %#v", record.SourceRefs)
	}
	if record.Audit.ReviewStatus == "" || len(record.Audit.RecommendedActions) == 0 {
		t.Fatalf("audit metadata is incomplete: %#v", record.Audit)
	}

	payload, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, forbidden := range []string{
		"/tmp/",
		"secret-token",
		"Authorization",
		"Cookie",
		"verify-book.html",
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("verified evidence pack leaked %q: %s", forbidden, string(payload))
		}
	}
}

func TestBuildVerifiedEvidencePackDiff(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	previous, err := store.BuildVerifiedEvidencePack(BookKnowledgeProjectHealth, 10)
	if err != nil {
		t.Fatalf("BuildVerifiedEvidencePack returned error: %v", err)
	}

	if _, err := store.BuildVerifiedEvidencePackDiff(BookKnowledgeProjectHealth, "missing-pack", 10); err == nil ||
		!strings.Contains(err.Error(), "previous_pack_not_found") {
		t.Fatalf("missing previous pack error = %v, want previous_pack_not_found", err)
	}

	if err := store.SavePackage(mutatedBookKnowledgePackageForEvidenceDiff()); err != nil {
		t.Fatalf("SavePackage mutated returned error: %v", err)
	}
	diff, err := store.BuildVerifiedEvidencePackDiff(BookKnowledgeProjectHealth, previous.PackID, 10)
	if err != nil {
		t.Fatalf("BuildVerifiedEvidencePackDiff returned error: %v", err)
	}
	if diff.ConsumerContract != VerifiedEvidencePackContractV1 ||
		diff.ProjectID != BookKnowledgeProjectHealth ||
		diff.PreviousPackID != previous.PackID ||
		diff.CurrentPackID == "" {
		t.Fatalf("diff identity fields are incomplete: %#v", diff)
	}
	if diff.SourceUnchanged {
		t.Fatal("SourceUnchanged = true, want false for changed source")
	}
	if diff.Counts.Added != 1 || diff.Counts.Removed != 1 || diff.Counts.Changed != 1 || diff.Counts.Unchanged != 1 {
		t.Fatalf("diff counts = %#v, want added=1 removed=1 changed=1 unchanged=1", diff.Counts)
	}

	changed := findEvidencePackDiffRecord(t, diff.Changed, "dedao:verify-book:verify-claim-study")
	if !stringSliceContains(changed.ChangedFields, "normalized_claim") ||
		!stringSliceContains(changed.ChangedFields, "source_hash") {
		t.Fatalf("changed fields = %#v, want normalized_claim and source_hash", changed.ChangedFields)
	}
	findEvidencePackDiffRecord(t, diff.Added, "dedao:verify-book:verify-claim-new")
	findEvidencePackDiffRecord(t, diff.Removed, "dedao:verify-book:verify-claim-unsupported")
	findEvidencePackDiffRecord(t, diff.Unchanged, "dedao:verify-book:verify-claim-medication")

	currentWithBlocked := *previous
	currentWithBlocked.PackID = "vep_blocked_fixture"
	currentWithBlocked.SourceFingerprint = "blocked_fixture_source"
	currentWithBlocked.Records = append([]VerifiedEvidencePackRecord(nil), previous.Records...)
	currentWithBlocked.Records[0].RiskTier = bookKnowledgeRiskBlocked
	currentWithBlocked.Records[0].QualityStatus = "rejected"
	blockedDiff := buildVerifiedEvidencePackDiff(&currentWithBlocked, previous)
	if blockedDiff.Counts.Blocked != 1 {
		t.Fatalf("blocked count = %d, want 1", blockedDiff.Counts.Blocked)
	}
}

func TestBuildVerifiedEvidencePullManifest(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForVerification()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	manifest, err := store.BuildVerifiedEvidencePullManifest(BookKnowledgeProjectHealth, 10)
	if err != nil {
		t.Fatalf("BuildVerifiedEvidencePullManifest returned error: %v", err)
	}

	if manifest.ConsumerContract != VerifiedEvidencePullManifestContractV1 {
		t.Fatalf("ConsumerContract = %q, want %q", manifest.ConsumerContract, VerifiedEvidencePullManifestContractV1)
	}
	if manifest.ProjectID != BookKnowledgeProjectHealth || manifest.TargetSystem == "" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.CurrentPack.PackID == "" || manifest.CurrentPack.SourceFingerprint == "" {
		t.Fatalf("manifest missing current pack identity: %#v", manifest.CurrentPack)
	}
	if manifest.CurrentPack.RecordCount != 3 || manifest.CurrentPack.QualitySummary.Total != 3 {
		t.Fatalf("manifest pack summary = %#v, want 3 records", manifest.CurrentPack)
	}
	if manifest.Endpoints.EvidencePackURL != "/api/projects/health/evidence-pack?limit=10" ||
		manifest.Endpoints.EvidencePackJSONLURL != "/api/projects/health/evidence-pack/export?format=jsonl&limit=10" ||
		manifest.Endpoints.DiffURLTemplate != "/api/projects/health/evidence-pack/diff?previous_pack_id={pack_id}&limit=10" ||
		manifest.Endpoints.DomainPackURL != "/api/projects/health/authority-pack?limit=10" {
		t.Fatalf("manifest endpoints = %#v", manifest.Endpoints)
	}
	if !manifest.ConsumerGate.MustCheckSourceFingerprint || !manifest.ConsumerGate.MustRejectBlocked {
		t.Fatalf("manifest consumer gate missing required checks: %#v", manifest.ConsumerGate)
	}
	if len(manifest.ConsumerGate.BlockedUses) == 0 || len(manifest.NextActions) == 0 {
		t.Fatalf("manifest missing safety metadata: gate=%#v actions=%#v", manifest.ConsumerGate, manifest.NextActions)
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest returned error: %v", err)
	}
	for _, forbidden := range []string{"/tmp/", "secret-token", "verify-book.html"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("manifest leaked %q: %s", forbidden, string(payload))
		}
	}
}

func findEvidencePackDiffRecord(t *testing.T, records []VerifiedEvidencePackDiffRecord, evidenceID string) VerifiedEvidencePackDiffRecord {
	t.Helper()
	for _, record := range records {
		if record.EvidenceID == evidenceID {
			return record
		}
	}
	t.Fatalf("evidence_id %q not found in diff records: %#v", evidenceID, records)
	return VerifiedEvidencePackDiffRecord{}
}

func mutatedBookKnowledgePackageForEvidenceDiff() BookKnowledgePackage {
	pkg := sampleBookKnowledgePackageForVerification()
	pkg.Chunks = append(pkg.Chunks, BookKnowledgeChunk{
		ChunkID:   "verify-chunk-3",
		BookID:    "verify-book",
		ChapterID: "verify-chapter-1",
		Order:     3,
		Text:      "新增证据说明复盘问题可以形成下一步学习计划。",
	})
	pkg.Claims = []BookKnowledgeClaim{
		{
			ClaimID:       "verify-claim-study",
			BookID:        "verify-book",
			ChapterID:     "verify-chapter-1",
			Title:         "复盘提高学习质量",
			Summary:       "稳定复盘可以帮助学习者识别错误模式，并形成下一步学习计划。",
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
		{
			ClaimID:       "verify-claim-new",
			BookID:        "verify-book",
			ChapterID:     "verify-chapter-1",
			Title:         "复盘问题形成计划",
			Summary:       "复盘问题可以形成下一步学习计划。",
			EvidenceLevel: "B",
			Confidence:    0.86,
			ReviewStatus:  "draft",
			Citations:     []string{"verify-citation-3"},
		},
	}
	pkg.Citations = append(pkg.Citations, BookKnowledgeCitation{
		CitationID: "verify-citation-3",
		BookID:     "verify-book",
		ChapterID:  "verify-chapter-1",
		ChunkID:    "verify-chunk-3",
		SourceHTML: "/tmp/verify-book.html",
	})
	return pkg
}
