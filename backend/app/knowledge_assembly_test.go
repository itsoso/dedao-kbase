package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeKnowledgeAssemblyClaimAndPolarity(t *testing.T) {
	if got := normalizeKnowledgeAssemblyClaim("  Evidence，Supports  TRIAL! "); got != "evidence supports trial" {
		t.Fatalf("normalized claim = %q", got)
	}
	positiveBase, positive := splitKnowledgeAssemblyClaimPolarity("治疗能降低风险")
	negativeBase, negative := splitKnowledgeAssemblyClaimPolarity("治疗不能降低风险")
	if positive != KnowledgeAssemblyPolarityPositive ||
		negative != KnowledgeAssemblyPolarityNegative ||
		positiveBase != negativeBase {
		t.Fatalf(
			"polarity = positive(%q,%q) negative(%q,%q)",
			positiveBase, positive, negativeBase, negative,
		)
	}
	distinctBase, distinctPolarity := splitKnowledgeAssemblyClaimPolarity("不同治疗方案适合不同人群")
	if distinctPolarity != KnowledgeAssemblyPolarityPositive ||
		distinctBase != normalizeKnowledgeAssemblyClaim("不同治疗方案适合不同人群") {
		t.Fatalf("non-negating 不 was treated as negative: base=%q polarity=%q", distinctBase, distinctPolarity)
	}
}

func TestBuildKnowledgeReleaseAssemblyUsesLatestReleasePerBookAndIsDeterministic(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a-old", "book-a", "2026-07-25T10:00:00Z",
		"旧结论", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a-new", "book-a", "2026-07-26T10:00:00Z",
		"干预能改善结局", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"干预能改善结局", "Publisher B", "dedao_course_article",
	))

	first, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if first.AssemblyID == "" || first.AssemblyID != second.AssemblyID ||
		!reflect.DeepEqual(first, second) {
		t.Fatalf("assembly is not deterministic: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(first.ReleaseIDs, []string{"release-a-new", "release-b"}) {
		t.Fatalf("release snapshot = %#v", first.ReleaseIDs)
	}
	if first.Summary.ReleaseCount != 2 || first.Summary.ClaimCount != 2 ||
		first.Summary.ClusterCount != 1 || first.Summary.CorroboratedClusters != 1 {
		t.Fatalf("summary = %#v", first.Summary)
	}
	if len(first.Clusters) != 1 ||
		first.Clusters[0].Status != KnowledgeAssemblyStatusCorroborated ||
		first.Clusters[0].IndependentPublicationCount != 2 {
		t.Fatalf("clusters = %#v", first.Clusters)
	}
	for _, ref := range first.Clusters[0].Claims {
		if ref.ReleaseID == "release-a-old" {
			t.Fatalf("superseded release was assembled: %#v", first.Clusters[0])
		}
	}
}

func TestLatestKnowledgeAssemblyReleaseRecordsComparesAbsoluteTimes(t *testing.T) {
	records, err := latestKnowledgeAssemblyReleaseRecords([]KnowledgeReleaseRecord{
		{
			ReleaseID: "release-earlier",
			BookID:    "book-a",
			CreatedAt: "2026-07-26T12:00:00+08:00",
		},
		{
			ReleaseID: "release-later",
			BookID:    "book-a",
			CreatedAt: "2026-07-26T05:00:00Z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ReleaseID != "release-later" {
		t.Fatalf("latest records = %#v", records)
	}
}

func TestBuildKnowledgeReleaseAssemblyFlagsExplicitPolarityConflict(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"治疗能降低风险", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"治疗不能降低风险", "Publisher B", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 ||
		assembly.Clusters[0].Status != KnowledgeAssemblyStatusPotentialConflict ||
		len(assembly.Clusters[0].PotentialConflicts) != 1 ||
		assembly.Summary.PotentialConflictClusters != 1 {
		t.Fatalf("assembly = %#v", assembly)
	}
	conflict := assembly.Clusters[0].PotentialConflicts[0]
	if conflict.PositiveClaimID == "" || conflict.NegativeClaimID == "" ||
		!conflict.ReviewRequired {
		t.Fatalf("conflict = %#v", conflict)
	}
}

func TestBuildKnowledgeReleaseAssemblyCountsPublisherAcrossTransportsOnce(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"同一结论", "Same Publisher", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"同一结论", " same publisher ", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 ||
		assembly.Clusters[0].PublicationCount != 1 ||
		assembly.Clusters[0].IndependentPublicationCount != 1 ||
		assembly.Clusters[0].Status != KnowledgeAssemblyStatusSinglePublication {
		t.Fatalf("cluster = %#v", assembly.Clusters)
	}
}

func TestKnowledgeAssemblyPublicationIdentityIsOpaqueAndTransportIndependent(t *testing.T) {
	first := canonicalKnowledgeAssemblyPublicationIdentity(BookKnowledgeBook{
		BookID: "book-a", SourceType: "wechat_mp_article", SourceAccount: "Medical Desk",
	})
	second := canonicalKnowledgeAssemblyPublicationIdentity(BookKnowledgeBook{
		BookID: "book-b", SourceType: "dedao_course_article", SourceAccount: " medical desk ",
	})
	host := canonicalKnowledgeAssemblyPublicationIdentity(BookKnowledgeBook{
		BookID: "book-host", SourceHTML: "https://private.internal.example/article",
	})
	if first.Key != second.Key ||
		strings.Contains(first.Key, "medical") ||
		strings.Contains(host.Key, "private.internal.example") ||
		!first.IndependentSourceEligible ||
		!host.IndependentSourceEligible {
		t.Fatalf("assembly publication identities = %#v %#v %#v", first, second, host)
	}
}

func TestBuildKnowledgeReleaseAssemblyDoesNotMergeBroadParaphrases(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"治疗降低风险", "Publisher A", "wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"风险因治疗而下降", "Publisher B", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if assembly.Summary.ClusterCount != 2 || len(assembly.Clusters) != 2 {
		t.Fatalf("broad paraphrases were merged: %#v", assembly)
	}
}

func TestBuildKnowledgeReleaseAssemblyRejectsInvalidSelectedRelease(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := knowledgeAssemblyTestRelease(
		"release-invalid",
		"book-invalid",
		"2026-07-26T10:00:00Z",
		"不完整结论",
		"Publisher",
		"wechat_mp_article",
	)
	release.ContentHash = ""
	saveKnowledgeAssemblyRelease(t, store, release)

	if _, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100}); err == nil ||
		!strings.Contains(err.Error(), "content_hash") {
		t.Fatalf("invalid selected release error = %v", err)
	}
}

func TestBuildKnowledgeReleaseAssemblyRejectsDanglingClaimCitation(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := knowledgeAssemblyTestRelease(
		"release-dangling",
		"book-dangling",
		"2026-07-26T10:00:00Z",
		"悬空证据结论",
		"Publisher",
		"wechat_mp_article",
	)
	release.Analysis.Claims[0].CitationIDs = []string{"missing-citation"}
	saveKnowledgeAssemblyRelease(t, store, release)

	if _, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{Limit: 100}); err == nil ||
		!strings.Contains(err.Error(), "missing-citation") {
		t.Fatalf("dangling citation error = %v", err)
	}
}

func TestBuildKnowledgeReleaseAssemblyReadsLegacyReleaseEvidence(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := knowledgeAssemblyTestRelease(
		"release-legacy",
		"book-legacy",
		"2026-07-26T10:00:00Z",
		"旧版证据仍可组装",
		"Publisher",
		"wechat_mp_article",
	)
	release.SchemaVersion = ""
	release.Analysis.Claims[0].CitationIDs = []string{release.Citations[0].ChunkID}
	saveKnowledgeAssemblyRelease(t, store, release)

	assembly, err := BuildKnowledgeReleaseAssembly(
		store,
		KnowledgeReleaseAssemblyQuery{Limit: 100},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 ||
		len(assembly.Clusters[0].Claims) != 1 ||
		!reflect.DeepEqual(
			assembly.Clusters[0].Claims[0].CitationIDs,
			[]string{release.Citations[0].CitationID},
		) {
		t.Fatalf("legacy release assembly = %#v", assembly)
	}
}

func TestBuildKnowledgeReleaseAssemblyRejectsOversizedCluster(t *testing.T) {
	tests := []struct {
		name  string
		build func(*testing.T, *BookKnowledgeStore)
		want  string
	}{
		{
			name: "claims",
			build: func(t *testing.T, store *BookKnowledgeStore) {
				for index := 0; index < 129; index++ {
					saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
						fmt.Sprintf("release-claim-%03d", index),
						fmt.Sprintf("book-claim-%03d", index),
						fmt.Sprintf("2026-07-26T10:%02d:%02dZ", index/60, index%60),
						"同一受限结论",
						fmt.Sprintf("Publisher %03d", index),
						"wechat_mp_article",
					))
				}
			},
			want: "claims exceeds 128",
		},
		{
			name: "statement",
			build: func(t *testing.T, store *BookKnowledgeStore) {
				saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
					"release-statement",
					"book-statement",
					"2026-07-26T10:00:00Z",
					strings.Repeat("界", 4097),
					"Publisher",
					"wechat_mp_article",
				))
			},
			want: "statement exceeds 4096",
		},
		{
			name: "citation ids",
			build: func(t *testing.T, store *BookKnowledgeStore) {
				release := knowledgeAssemblyTestRelease(
					"release-citations",
					"book-citations",
					"2026-07-26T10:00:00Z",
					"引用数量受限",
					"Publisher",
					"wechat_mp_article",
				)
				release.Analysis.Claims[0].CitationIDs = nil
				release.Citations = nil
				for index := 0; index < 129; index++ {
					citationID := fmt.Sprintf("citation-%03d", index)
					release.Analysis.Claims[0].CitationIDs = append(
						release.Analysis.Claims[0].CitationIDs,
						citationID,
					)
					release.Citations = append(release.Citations, BookKnowledgeCitation{
						CitationID: citationID,
						BookID:     release.BookID,
						ChunkID:    fmt.Sprintf("chunk-%03d", index),
					})
				}
				saveKnowledgeAssemblyRelease(t, store, release)
			},
			want: "citation_ids exceeds 128",
		},
		{
			name: "citation ids before deduplication",
			build: func(t *testing.T, store *BookKnowledgeStore) {
				release := knowledgeAssemblyTestRelease(
					"release-duplicate-citations",
					"book-duplicate-citations",
					"2026-07-26T10:00:00Z",
					"重复引用数量仍然受限",
					"Publisher",
					"wechat_mp_article",
				)
				citationID := release.Analysis.Claims[0].CitationIDs[0]
				release.Analysis.Claims[0].CitationIDs = make([]string, 129)
				for index := range release.Analysis.Claims[0].CitationIDs {
					release.Analysis.Claims[0].CitationIDs[index] = citationID
				}
				saveKnowledgeAssemblyRelease(t, store, release)
			},
			want: "citation_ids exceeds 128",
		},
		{
			name: "potential conflicts",
			build: func(t *testing.T, store *BookKnowledgeStore) {
				for index := 0; index < 34; index++ {
					statement := "治疗能降低风险"
					if index >= 17 {
						statement = "治疗不能降低风险"
					}
					saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
						fmt.Sprintf("release-conflict-%03d", index),
						fmt.Sprintf("book-conflict-%03d", index),
						fmt.Sprintf("2026-07-26T10:00:%02dZ", index),
						statement,
						fmt.Sprintf("Publisher %03d", index),
						"wechat_mp_article",
					))
				}
			},
			want: "potential_conflicts exceeds 256",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewBookKnowledgeStore(t.TempDir())
			testCase.build(t, store)
			if _, err := BuildKnowledgeReleaseAssembly(
				store,
				KnowledgeReleaseAssemblyQuery{Limit: 500},
			); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("oversized assembly error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestValidateKnowledgeReleaseAssemblyRejectsOversizedCluster(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-a",
		"book-a",
		"2026-07-26T10:00:00Z",
		"受限结论",
		"Publisher",
		"wechat_mp_article",
	))
	base, err := BuildKnowledgeReleaseAssembly(
		store,
		KnowledgeReleaseAssemblyQuery{Limit: 500},
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*KnowledgeReleaseAssembly)
		want   string
	}{
		{
			name: "claims",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				claim := assembly.Clusters[0].Claims[0]
				assembly.Clusters[0].Claims = nil
				for index := 0; index < 129; index++ {
					next := claim
					next.ClaimID = fmt.Sprintf("claim-%03d", index)
					assembly.Clusters[0].Claims = append(assembly.Clusters[0].Claims, next)
				}
				assembly.Summary.ClaimCount = 129
			},
			want: "claims exceeds 128",
		},
		{
			name: "statement",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Claims[0].Statement = strings.Repeat("界", 4097)
			},
			want: "statement exceeds 4096",
		},
		{
			name: "citation ids",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Claims[0].CitationIDs = nil
				for index := 0; index < 129; index++ {
					assembly.Clusters[0].Claims[0].CitationIDs = append(
						assembly.Clusters[0].Claims[0].CitationIDs,
						fmt.Sprintf("citation-%03d", index),
					)
				}
			},
			want: "citation_ids exceeds 128",
		},
		{
			name: "potential conflicts",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Status = KnowledgeAssemblyStatusPotentialConflict
				assembly.Clusters[0].PotentialConflicts = make(
					[]KnowledgeReleaseAssemblyPotentialConflict,
					257,
				)
			},
			want: "potential_conflicts exceeds 256",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assembly := cloneKnowledgeReleaseAssembly(t, *base)
			testCase.mutate(&assembly)
			if err := ValidateKnowledgeReleaseAssembly(assembly); err == nil ||
				!strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("oversized contract error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestValidateKnowledgeReleaseAssemblyRelationships(t *testing.T) {
	base := knowledgeAssemblyRelationshipFixture(t)
	tests := []struct {
		name   string
		mutate func(*KnowledgeReleaseAssembly)
		want   string
	}{
		{
			name: "unknown claim release",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Claims[0].ReleaseID = "release-unknown"
			},
			want: "unknown release_id",
		},
		{
			name: "cluster id",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].ClusterID = "cluster-forged"
			},
			want: "cluster_id is inconsistent",
		},
		{
			name: "normalized assertion",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].NormalizedAssertion = "伪造断言"
				assembly.Clusters[0].ClusterID = knowledgeAssemblyHashID("cluster", "伪造断言")
			},
			want: "normalized_assertion is inconsistent",
		},
		{
			name: "duplicate claim",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Claims = append(
					assembly.Clusters[0].Claims,
					assembly.Clusters[0].Claims[0],
				)
				assembly.Summary.ClaimCount++
			},
			want: "duplicate claim",
		},
		{
			name: "duplicate citation id",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				claim := &assembly.Clusters[0].Claims[0]
				claim.CitationIDs = append(claim.CitationIDs, claim.CitationIDs[0])
			},
			want: "duplicate citation_id",
		},
		{
			name: "publication count",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].PublicationCount++
			},
			want: "publication_count is inconsistent",
		},
		{
			name: "publication eligibility",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Claims[0].IndependentPublicationEligible = false
			},
			want: "independent publication eligibility is inconsistent",
		},
		{
			name: "publication identity",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Claims[0].PublicationIdentity = "account:private-publisher"
			},
			want: "publication_identity is invalid",
		},
		{
			name: "status",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].Status = KnowledgeAssemblyStatusCorroborated
			},
			want: "status is inconsistent",
		},
		{
			name: "conflict edge",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Clusters[0].PotentialConflicts[0].ConflictID = "conflict-forged"
			},
			want: "potential_conflicts is inconsistent",
		},
		{
			name: "summary category totals",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Summary.CorroboratedClusters++
			},
			want: "summary category counts are inconsistent",
		},
		{
			name: "has more",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.HasMore = true
			},
			want: "has_more is inconsistent",
		},
		{
			name: "matched exceeds cluster count",
			mutate: func(assembly *KnowledgeReleaseAssembly) {
				assembly.Summary.MatchedClusterCount = 2
				assembly.HasMore = true
			},
			want: "matched_cluster_count exceeds cluster_count",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assembly := cloneKnowledgeReleaseAssembly(t, base)
			testCase.mutate(&assembly)
			if err := ValidateKnowledgeReleaseAssembly(assembly); err == nil ||
				!strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("relationship error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestValidateKnowledgeReleaseAssemblyDoesNotMutateInput(t *testing.T) {
	assembly := knowledgeAssemblyRelationshipFixture(t)
	before := cloneKnowledgeReleaseAssembly(t, assembly)
	if err := ValidateKnowledgeReleaseAssembly(assembly); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(assembly, before) {
		t.Fatalf("validator mutated input: before=%#v after=%#v", before, assembly)
	}
}

func TestBuildKnowledgeReleaseAssemblyIsBoundedQueryableAndPrivacySafe(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	first := knowledgeAssemblyTestRelease(
		"release-a", "book-a", "2026-07-26T10:00:00Z",
		"可检索结论", "private publisher value", "wechat_mp_article",
	)
	first.Book.SourceHTML = "local/private/source.html"
	first.Analysis.Summary = "private summary sentinel"
	first.Citations[0].SourceHTML = "local/private/citation.html"
	first.Citations[0].Note = "private citation note"
	saveKnowledgeAssemblyRelease(t, store, first)
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-b", "book-b", "2026-07-26T11:00:00Z",
		"另一个结论", "Publisher B", "dedao_course_article",
	))

	assembly, err := BuildKnowledgeReleaseAssembly(store, KnowledgeReleaseAssemblyQuery{
		Limit: 1,
		Query: "可检索",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Clusters) != 1 || assembly.ReturnedClusters != 1 ||
		assembly.HasMore || !strings.Contains(assembly.Clusters[0].Claims[0].Statement, "可检索") {
		t.Fatalf("bounded query result = %#v", assembly)
	}
	payload, err := json.Marshal(assembly)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(payload)
	for _, forbidden := range []string{
		"private publisher value",
		"private summary sentinel",
		"local/private",
		"private citation note",
		`"source_account":`,
		`"prompt"`,
		`"answer"`,
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("assembly leaked %q: %s", forbidden, raw)
		}
	}
}

func knowledgeAssemblyTestRelease(
	releaseID, bookID, createdAt, statement, publisher, sourceType string,
) KnowledgeRelease {
	citationID := releaseID + "-citation"
	return KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       knowledgeReleaseVersion,
		ReleaseID:     releaseID,
		BookID:        bookID,
		ContentHash:   "sha256:" + releaseID,
		UsagePolicy:   BookUsageEvidenceOnly,
		Book: BookKnowledgeBook{
			BookID:        bookID,
			Title:         "Book " + bookID,
			SourceType:    sourceType,
			SourceAccount: publisher,
		},
		Analysis: &BookAnalysisPayload{
			Summary: "summary",
			Claims: []BookAnalysisClaim{{
				ID:          releaseID + "-claim",
				Statement:   statement,
				CitationIDs: []string{citationID},
				Confidence:  0.8,
				RiskLevel:   "low",
			}},
		},
		Quality: BookQualityReport{Decision: BookQualityPass},
		Citations: []BookKnowledgeCitation{{
			CitationID: citationID,
			BookID:     bookID,
			ChapterID:  bookID + "-chapter",
			ChunkID:    bookID + "-chunk",
		}},
		CreatedAt: createdAt,
	}
}

func saveKnowledgeAssemblyRelease(t *testing.T, store *BookKnowledgeStore, release KnowledgeRelease) {
	t.Helper()
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatal(err)
	}
}

func cloneKnowledgeReleaseAssembly(
	t *testing.T,
	assembly KnowledgeReleaseAssembly,
) KnowledgeReleaseAssembly {
	t.Helper()
	payload, err := json.Marshal(assembly)
	if err != nil {
		t.Fatal(err)
	}
	var cloned KnowledgeReleaseAssembly
	if err := json.Unmarshal(payload, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func knowledgeAssemblyRelationshipFixture(t *testing.T) KnowledgeReleaseAssembly {
	t.Helper()
	store := NewBookKnowledgeStore(t.TempDir())
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-positive",
		"book-positive",
		"2026-07-26T10:00:00Z",
		"治疗能降低风险",
		"Publisher Positive",
		"wechat_mp_article",
	))
	saveKnowledgeAssemblyRelease(t, store, knowledgeAssemblyTestRelease(
		"release-negative",
		"book-negative",
		"2026-07-26T11:00:00Z",
		"治疗不能降低风险",
		"Publisher Negative",
		"dedao_course_article",
	))
	assembly, err := BuildKnowledgeReleaseAssembly(
		store,
		KnowledgeReleaseAssemblyQuery{Limit: 500},
	)
	if err != nil {
		t.Fatal(err)
	}
	return *assembly
}
