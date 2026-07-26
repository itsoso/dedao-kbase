package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBookAnalysisManifestRoundTrip(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	want := BookAnalysisManifest{
		Version:     "1",
		BookID:      "source-article-1",
		ContentHash: "hash-1",
		Status:      BookAnalysisPending,
		UpdatedAt:   "2026-07-12T12:00:00Z",
	}
	if err := store.SaveAnalysisManifest(want); err != nil {
		t.Fatalf("SaveAnalysisManifest returned error: %v", err)
	}
	got, err := store.LoadAnalysisManifest(want.BookID)
	if err != nil {
		t.Fatalf("LoadAnalysisManifest returned error: %v", err)
	}
	if got.BookID != want.BookID || got.ContentHash != want.ContentHash || got.Status != BookAnalysisPending {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}

func TestBookAnalysisManifestMissing(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	_, err := store.LoadAnalysisManifest("missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadAnalysisManifest error = %v, want os.ErrNotExist", err)
	}
}

func TestGenerateBookAnalysisManifestPersistsGroundedResult(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: sampleStructuredBookAnalysisJSON()}

	manifest, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{
		BookID: "42",
		Model:  "Qwen-3.7-Max",
	}, client)
	if err != nil {
		t.Fatalf("GenerateBookAnalysisManifestWithClient returned error: %v", err)
	}
	if manifest.Status != BookAnalysisReady || manifest.Model != "qwen3.7-max" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.Payload == nil || manifest.Payload.Summary != "这是基于证据的分析。" || len(manifest.Payload.Claims) != 1 {
		t.Fatalf("structured payload = %#v", manifest.Payload)
	}
	if !strings.Contains(manifest.Answer, "核心摘要") || !strings.Contains(manifest.Answer, "42-citation-1") {
		t.Fatalf("rendered answer = %q", manifest.Answer)
	}
	if manifest.PromptVersion != "structured-v2-citations" {
		t.Fatalf("prompt version = %q", manifest.PromptVersion)
	}
	if manifest.ContentHash != pkg.Book.ContentHash || manifest.CompletedAt == "" {
		t.Fatalf("manifest provenance = %#v", manifest)
	}
	combined := client.messages[0].Content + "\n" + client.messages[1].Content
	for _, marker := range []string{
		"结构化分析",
		"核心摘要",
		"可验证结论",
		"风险与局限",
		"阅读或验证行动",
		"citation ID",
		"Evidence [citation:42-citation-1]",
	} {
		if !strings.Contains(combined, marker) {
			t.Fatalf("analysis prompt missing %q:\n%s", marker, combined)
		}
	}
	if strings.Contains(combined, "## Chunk [42-chunk-1]") {
		t.Fatalf("analysis context exposed legacy chunk label:\n%s", combined)
	}
	if strings.Contains(combined, "[chunk:") || strings.Contains(combined, "[claim:") {
		t.Fatalf("analysis prompt retained legacy reference examples:\n%s", combined)
	}
	if len(manifest.Sources) != 1 ||
		manifest.Sources[0].Kind != "citation" ||
		manifest.Sources[0].ID != "42-citation-1" {
		t.Fatalf("analysis sources = %#v", manifest.Sources)
	}
	stored, err := store.LoadAnalysisManifest("42")
	if err != nil || stored.Payload == nil || stored.Status != BookAnalysisReady {
		t.Fatalf("stored manifest = %#v, err=%v", stored, err)
	}
}

func TestGenerateBookAnalysisManifestAppliesQwenStructuredRequestPolicy(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: sampleStructuredBookAnalysisJSON()}

	if _, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{
		BookID: "42",
		Model:  "qwen3.7-max",
	}, client); err != nil {
		t.Fatalf("GenerateBookAnalysisManifestWithClient returned error: %v", err)
	}
	field := reflect.ValueOf(client.cfg).FieldByName("EnableThinking")
	if !field.IsValid() || field.IsNil() || field.Elem().Bool() {
		t.Fatalf("structured Qwen analysis thinking config = %#v, want explicit false", client.cfg)
	}
	if client.cfg.MaxTokens < bookAnalysisMaxTokens {
		t.Fatalf("structured Qwen analysis max tokens = %d, want at least %d", client.cfg.MaxTokens, bookAnalysisMaxTokens)
	}
}

func TestGenerateBookAnalysisManifestParsesStructuredPayload(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "```json\n" + sampleStructuredBookAnalysisJSON() + "\n```"}

	manifest, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{BookID: "42"}, client)
	if err != nil {
		t.Fatalf("GenerateBookAnalysisManifestWithClient returned error: %v", err)
	}
	claim := manifest.Payload.Claims[0]
	if claim.ID != "claim-1" || claim.Statement == "" || len(claim.CitationIDs) != 1 || claim.CitationIDs[0] != "42-citation-1" {
		t.Fatalf("claim = %#v", claim)
	}
	if claim.Confidence != 0.86 || claim.RiskLevel != "medium" || len(claim.Scope) != 1 {
		t.Fatalf("claim metadata = %#v", claim)
	}
	if len(manifest.Payload.Risks) != 1 || len(manifest.Payload.Actions) != 1 {
		t.Fatalf("payload = %#v", manifest.Payload)
	}
	report, err := store.LoadBookQualityReport("42")
	if err != nil || report.Decision != BookQualityPass {
		t.Fatalf("generated quality report = %#v, err=%v", report, err)
	}
}

func TestParseBookAnalysisPayloadCoercesStringScope(t *testing.T) {
	payload, err := parseBookAnalysisPayload(`{
		"summary":"summary",
		"claims":[{
			"id":"claim-1",
			"statement":"statement",
			"citation_ids":["42-chunk-1"],
			"confidence":0.8,
			"scope":"single scope",
			"risk_level":"medium"
		}],
		"risks":[],
		"actions":[]
	}`)
	if err != nil {
		t.Fatalf("parseBookAnalysisPayload returned error: %v", err)
	}
	if got := payload.Claims[0].Scope; len(got) != 1 || got[0] != "single scope" {
		t.Fatalf("scope = %#v, want coerced single-item scope", got)
	}
}

func TestParseBookAnalysisPayloadRejectsInvalidScopeType(t *testing.T) {
	for _, scope := range []string{`{"text":"invalid"}`, `null`, `123`, `true`} {
		_, err := parseBookAnalysisPayload(`{
			"summary":"summary",
			"claims":[{
				"id":"claim-1",
				"statement":"statement",
				"citation_ids":["42-chunk-1"],
				"confidence":0.8,
				"scope":` + scope + `,
				"risk_level":"medium"
			}],
			"risks":[],
			"actions":[]
		}`)
		if err == nil || !strings.Contains(err.Error(), "structured analysis") {
			t.Fatalf("scope %s parse error = %v, want structured analysis error", scope, err)
		}
	}
}

func TestParseBookAnalysisPayloadRejectsUnknownClaimFields(t *testing.T) {
	_, err := parseBookAnalysisPayload(`{
		"summary":"summary",
		"claims":[{
			"id":"claim-1",
			"statement":"statement",
			"citation_ids":["42-chunk-1"],
			"confidence":0.8,
			"scope":"single scope",
			"risk_level":"medium",
			"unexpected":"not allowed"
		}],
		"risks":[],
		"actions":[]
	}`)
	if err == nil || !strings.Contains(err.Error(), "structured analysis") {
		t.Fatalf("parseBookAnalysisPayload error = %v, want structured analysis error", err)
	}
}

func TestGenerateBookAnalysisManifestRejectsMalformedPayload(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	previous := BookAnalysisManifest{
		Version: "1", BookID: "42", ContentHash: pkg.Book.ContentHash, Status: BookAnalysisReady,
		Answer: "previous answer", Payload: &BookAnalysisPayload{Summary: "previous summary"}, UpdatedAt: "2026-07-12T10:00:00Z",
	}
	if err := store.SaveAnalysisManifest(previous); err != nil {
		t.Fatal(err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "not-json"}

	_, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{BookID: "42"}, client)
	if err == nil || !strings.Contains(err.Error(), "structured analysis") {
		t.Fatalf("generation error = %v", err)
	}
	stored, loadErr := store.LoadAnalysisManifest("42")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.Status != BookAnalysisFailed || stored.Answer != previous.Answer || stored.Payload == nil || stored.Payload.Summary != "previous summary" {
		t.Fatalf("stored manifest = %#v", stored)
	}
}

func TestGenerateBookAnalysisManifestRejectsNonCitationReferences(t *testing.T) {
	tests := []struct {
		name      string
		claimRef  string
		riskRef   string
		actionRef string
		wantID    string
	}{
		{
			name:      "claim uses direct chunk",
			claimRef:  "42-chunk-1",
			riskRef:   "42-citation-1",
			actionRef: "42-citation-1",
			wantID:    "42-chunk-1",
		},
		{
			name:      "risk uses unknown citation",
			claimRef:  "42-citation-1",
			riskRef:   "missing-citation",
			actionRef: "42-citation-1",
			wantID:    "missing-citation",
		},
		{
			name:      "action uses chapter",
			claimRef:  "42-citation-1",
			riskRef:   "42-citation-1",
			actionRef: "42-chapter-1",
			wantID:    "42-chapter-1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
			store := NewBookKnowledgeStore(t.TempDir())
			pkg := sampleBookKnowledgePackageForExport()
			pkg.Book.ContentHash = "content-hash-42"
			if err := store.SavePackage(pkg); err != nil {
				t.Fatal(err)
			}
			previous := BookAnalysisManifest{
				Version: "1", BookID: "42", ContentHash: pkg.Book.ContentHash,
				Status: BookAnalysisReady, PromptVersion: "structured-v1",
				Answer: "previous answer",
				Payload: &BookAnalysisPayload{
					Summary: "previous summary",
					Claims: []BookAnalysisClaim{{
						ID: "previous-claim", Statement: "previous statement",
						CitationIDs: []string{"42-citation-1"},
						Confidence:  0.8, RiskLevel: "low",
					}},
				},
				UpdatedAt: "2026-07-12T10:00:00Z",
			}
			if err := store.SaveAnalysisManifest(previous); err != nil {
				t.Fatal(err)
			}
			answer := fmt.Sprintf(`{
				"summary":"summary",
				"claims":[{"id":"claim-1","statement":"statement","citation_ids":[%q],"confidence":0.8,"scope":[],"risk_level":"low"}],
				"risks":[{"id":"risk-1","description":"risk","citation_ids":[%q],"severity":"low"}],
				"actions":[{"id":"action-1","description":"action","citation_ids":[%q],"kind":"verify"}]
			}`, testCase.claimRef, testCase.riskRef, testCase.actionRef)

			_, err := GenerateBookAnalysisManifestWithClient(
				context.Background(),
				store,
				BookAnalysisGenerateRequest{BookID: "42"},
				&fakeBookKnowledgeLLMClient{answer: answer},
			)
			if err == nil ||
				!strings.Contains(err.Error(), "citation_ids") ||
				strings.Contains(err.Error(), testCase.wantID) ||
				!strings.Contains(err.Error(), "sha256-") {
				t.Fatalf("generation error = %v", err)
			}
			stored, loadErr := store.LoadAnalysisManifest("42")
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if stored.Status != BookAnalysisFailed ||
				stored.Answer != previous.Answer ||
				stored.Payload == nil ||
				stored.Payload.Summary != previous.Payload.Summary {
				t.Fatalf("stored manifest = %#v", stored)
			}
		})
	}
}

func TestGenerateBookAnalysisManifestPrioritizesCitationEvidenceWithinContextLimit(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	for index := 0; index < 12; index++ {
		pkg.Chapters = append(pkg.Chapters, BookKnowledgeChapter{
			ChapterID: fmt.Sprintf("extra-chapter-%d", index),
			BookID:    pkg.Book.BookID,
			Order:     index + 2,
			Title:     fmt.Sprintf("扩展章节 %d", index),
			Summary:   strings.Repeat("章节摘要", 30),
		})
	}
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: sampleStructuredBookAnalysisJSON()}

	if _, err := GenerateBookAnalysisManifestWithClient(
		context.Background(),
		store,
		BookAnalysisGenerateRequest{BookID: "42", MaxContextChars: 500},
		client,
	); err != nil {
		t.Fatalf("GenerateBookAnalysisManifestWithClient returned error: %v", err)
	}
	combined := client.messages[0].Content + "\n" + client.messages[1].Content
	if !strings.Contains(combined, "Evidence [citation:42-citation-1]") {
		t.Fatalf("bounded analysis context omitted citation evidence:\n%s", combined)
	}
}

func TestValidateGeneratedBookAnalysisCitationIDsRedactsUntrustedReference(t *testing.T) {
	pkg := sampleBookKnowledgePackageForExport()
	for _, privateReference := range []string{
		"local/private-account/downloaded-content.html",
		"short-private-token",
	} {
		err := validateGeneratedBookAnalysisCitationIDs(pkg, []BookKnowledgeChatSource{{
			Kind: "citation", ID: "42-citation-1",
		}}, BookAnalysisPayload{
			Claims: []BookAnalysisClaim{{
				ID: "claim-1", Statement: "statement",
				CitationIDs: []string{privateReference},
				Confidence:  0.8, RiskLevel: "low",
			}},
		})
		if err == nil {
			t.Fatal("validateGeneratedBookAnalysisCitationIDs returned nil")
		}
		if strings.Contains(err.Error(), privateReference) || !strings.Contains(err.Error(), "sha256-") {
			t.Fatalf("validation error exposed untrusted reference: %v", err)
		}
	}
}

func TestValidateGeneratedBookAnalysisCitationIDsRejectsCitationOutsideContext(t *testing.T) {
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Citations = append(pkg.Citations, BookKnowledgeCitation{
		CitationID: "42-citation-unseen",
		BookID:     pkg.Book.BookID,
		ChapterID:  "42-chapter-1",
		ChunkID:    "42-chunk-1",
	})
	err := validateGeneratedBookAnalysisCitationIDs(pkg, []BookKnowledgeChatSource{{
		Kind: "citation", ID: "42-citation-1",
	}}, BookAnalysisPayload{
		Claims: []BookAnalysisClaim{{
			ID: "claim-1", Statement: "statement",
			CitationIDs: []string{"42-citation-unseen"},
			Confidence:  0.8, RiskLevel: "low",
		}},
	})
	if err == nil ||
		strings.Contains(err.Error(), "42-citation-unseen") ||
		!strings.Contains(err.Error(), "sha256-") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestValidateGeneratedBookAnalysisCitationIDsRequiresGroundedRisksAndActions(t *testing.T) {
	pkg := sampleBookKnowledgePackageForExport()
	sources := []BookKnowledgeChatSource{{Kind: "citation", ID: "42-citation-1"}}
	tests := []struct {
		name    string
		payload BookAnalysisPayload
		want    string
	}{
		{
			name: "claim missing citation",
			payload: BookAnalysisPayload{Claims: []BookAnalysisClaim{{
				ID: "claim-1", Statement: "statement", Confidence: 0.8, RiskLevel: "low",
			}}},
			want: "claims[0].citation_ids",
		},
		{
			name: "risk missing citation",
			payload: BookAnalysisPayload{Risks: []BookAnalysisRisk{{
				ID: "risk-1", Description: "risk", Severity: "low",
			}}},
			want: "risks[0].citation_ids",
		},
		{
			name: "risk invalid severity",
			payload: BookAnalysisPayload{Risks: []BookAnalysisRisk{{
				ID: "risk-1", Description: "risk",
				CitationIDs: []string{"42-citation-1"}, Severity: "critical",
			}}},
			want: "risks[0].severity",
		},
		{
			name: "risk missing id",
			payload: BookAnalysisPayload{Risks: []BookAnalysisRisk{{
				Description: "risk",
				CitationIDs: []string{"42-citation-1"}, Severity: "low",
			}}},
			want: "risks[0].id",
		},
		{
			name: "risk missing description",
			payload: BookAnalysisPayload{Risks: []BookAnalysisRisk{{
				ID:          "risk-1",
				CitationIDs: []string{"42-citation-1"}, Severity: "low",
			}}},
			want: "risks[0].description",
		},
		{
			name: "action missing citation",
			payload: BookAnalysisPayload{Actions: []BookAnalysisAction{{
				ID: "action-1", Description: "action", Kind: "verify",
			}}},
			want: "actions[0].citation_ids",
		},
		{
			name: "action invalid kind",
			payload: BookAnalysisPayload{Actions: []BookAnalysisAction{{
				ID: "action-1", Description: "action",
				CitationIDs: []string{"42-citation-1"}, Kind: "execute",
			}}},
			want: "actions[0].kind",
		},
		{
			name: "action missing id",
			payload: BookAnalysisPayload{Actions: []BookAnalysisAction{{
				Description: "action",
				CitationIDs: []string{"42-citation-1"}, Kind: "verify",
			}}},
			want: "actions[0].id",
		},
		{
			name: "action missing description",
			payload: BookAnalysisPayload{Actions: []BookAnalysisAction{{
				ID:          "action-1",
				CitationIDs: []string{"42-citation-1"}, Kind: "verify",
			}}},
			want: "actions[0].description",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateGeneratedBookAnalysisCitationIDs(pkg, sources, testCase.payload)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestBuildBookAnalysisContextOmitsLegacyChunkBody(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Citations = nil
	pkg.Chunks[0].Text = "private legacy body sentinel"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}

	contextText, _, sources, err := buildBookAnalysisContext(store, &pkg, "analysis", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(contextText, "private legacy body sentinel") ||
		!strings.Contains(contextText, "citation_status: unavailable") {
		t.Fatalf("legacy analysis context = %q", contextText)
	}
	if len(sources) != 1 || sources[0].Kind != "legacy_chunk" {
		t.Fatalf("legacy analysis sources = %#v", sources)
	}
}

func sampleStructuredBookAnalysisJSON() string {
	return `{
  "summary":"这是基于证据的分析。",
  "claims":[{"id":"claim-1","statement":"趋势过滤是该方法的前置条件。","citation_ids":["42-citation-1"],"confidence":0.86,"scope":["示例策略"],"risk_level":"medium"}],
  "risks":[{"id":"risk-1","description":"需要外部数据验证。","citation_ids":["42-citation-1"],"severity":"medium"}],
  "actions":[{"id":"action-1","description":"核对原始样本。","citation_ids":["42-citation-1"],"kind":"verify"}]
}`
}

func TestGenerateBookAnalysisManifestPreservesPreviousAnswerOnFailure(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "content-hash-42"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version: "1", BookID: "42", ContentHash: pkg.Book.ContentHash,
		Status: BookAnalysisReady, Model: "old-model", Answer: "previous answer", UpdatedAt: "2026-07-12T10:00:00Z",
	}); err != nil {
		t.Fatalf("SaveAnalysisManifest returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{err: errors.New("model unavailable")}

	_, err := GenerateBookAnalysisManifestWithClient(context.Background(), store, BookAnalysisGenerateRequest{BookID: "42"}, client)
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("generation error = %v", err)
	}
	stored, loadErr := store.LoadAnalysisManifest("42")
	if loadErr != nil {
		t.Fatalf("LoadAnalysisManifest returned error: %v", loadErr)
	}
	if stored.Status != BookAnalysisFailed || stored.Answer != "previous answer" || !strings.Contains(stored.Error, "model unavailable") {
		t.Fatalf("failed manifest = %#v", stored)
	}
}
