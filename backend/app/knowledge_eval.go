package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	KnowledgeEvalSchemaVersion       = "knowledge_eval_suite.v1"
	KnowledgeEvalReportSchemaVersion = "knowledge_eval_report.v1"
)

type KnowledgeEvalSuite struct {
	SchemaVersion string              `json:"schema_version"`
	Cases         []KnowledgeEvalCase `json:"cases"`
}

type KnowledgeEvalCase struct {
	ID               string   `json:"id"`
	Query            string   `json:"query"`
	ExpectedChunkIDs []string `json:"expected_chunk_ids,omitempty"`
	Answer           string   `json:"answer,omitempty"`
	CitationIDs      []string `json:"citation_ids,omitempty"`
	Consumer         string   `json:"consumer,omitempty"`
	Domain           string   `json:"domain,omitempty"`
}

type KnowledgeEvalReport struct {
	SchemaVersion          string                    `json:"schema_version"`
	TotalCases             int                       `json:"total_cases"`
	RetrievalHitRate       float64                   `json:"retrieval_hit_rate"`
	CitationCoverage       float64                   `json:"citation_coverage"`
	UnsupportedAnswerCount int                       `json:"unsupported_answer_count"`
	ZeroHitGapCount        int                       `json:"zero_hit_gap_count"`
	CaseResults            []KnowledgeEvalCaseResult `json:"case_results"`
	Gaps                   []KnowledgeGapInput       `json:"gaps,omitempty"`
}

type KnowledgeEvalCaseResult struct {
	ID                 string   `json:"id"`
	RetrievalHit       bool     `json:"retrieval_hit"`
	CitationCovered    bool     `json:"citation_covered"`
	UnsupportedAnswer  bool     `json:"unsupported_answer"`
	RetrievedChunkIDs  []string `json:"retrieved_chunk_ids,omitempty"`
	MissingCitationIDs []string `json:"missing_citation_ids,omitempty"`
}

func LoadKnowledgeEvalSuite(path string) (KnowledgeEvalSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return KnowledgeEvalSuite{}, err
	}
	var suite KnowledgeEvalSuite
	if err := json.Unmarshal(data, &suite); err != nil {
		return KnowledgeEvalSuite{}, err
	}
	if suite.SchemaVersion != KnowledgeEvalSchemaVersion {
		return KnowledgeEvalSuite{}, fmt.Errorf("unsupported knowledge eval suite schema %q", suite.SchemaVersion)
	}
	return suite, nil
}

func EvaluateKnowledgeSuite(index *KnowledgeSearchIndex, suite KnowledgeEvalSuite) (KnowledgeEvalReport, error) {
	if index == nil {
		return KnowledgeEvalReport{}, fmt.Errorf("knowledge search index is required")
	}
	if suite.SchemaVersion != KnowledgeEvalSchemaVersion {
		return KnowledgeEvalReport{}, fmt.Errorf("unsupported knowledge eval suite schema %q", suite.SchemaVersion)
	}
	report := KnowledgeEvalReport{
		SchemaVersion: KnowledgeEvalReportSchemaVersion,
		TotalCases:    len(suite.Cases),
		CaseResults:   make([]KnowledgeEvalCaseResult, 0, len(suite.Cases)),
	}
	retrievalHits := 0
	citationCovered := 0
	for _, evalCase := range suite.Cases {
		caseResult, gaps, err := evaluateKnowledgeCase(index, evalCase)
		if err != nil {
			return KnowledgeEvalReport{}, err
		}
		if caseResult.RetrievalHit {
			retrievalHits++
		}
		if caseResult.CitationCovered {
			citationCovered++
		}
		if caseResult.UnsupportedAnswer {
			report.UnsupportedAnswerCount++
		}
		report.Gaps = append(report.Gaps, gaps...)
		report.CaseResults = append(report.CaseResults, caseResult)
	}
	if report.TotalCases > 0 {
		report.RetrievalHitRate = float64(retrievalHits) / float64(report.TotalCases)
		report.CitationCoverage = float64(citationCovered) / float64(report.TotalCases)
	}
	report.ZeroHitGapCount = len(report.Gaps)
	return report, nil
}

func evaluateKnowledgeCase(index *KnowledgeSearchIndex, evalCase KnowledgeEvalCase) (KnowledgeEvalCaseResult, []KnowledgeGapInput, error) {
	evalCase.ID = strings.TrimSpace(evalCase.ID)
	results, err := index.Search(KnowledgeSearchIndexQuery{Query: evalCase.Query, Limit: 20})
	if err != nil {
		return KnowledgeEvalCaseResult{}, nil, err
	}
	retrieved := make([]string, 0, len(results))
	retrievedSet := map[string]bool{}
	for _, result := range results {
		if strings.TrimSpace(result.ChunkID) == "" {
			continue
		}
		retrieved = append(retrieved, result.ChunkID)
		retrievedSet[result.ChunkID] = true
	}
	expectedSet := stringSliceSet(evalCase.ExpectedChunkIDs)
	retrievalHit := len(expectedSet) == 0 && len(retrieved) > 0
	for chunkID := range expectedSet {
		if retrievedSet[chunkID] {
			retrievalHit = true
			break
		}
	}
	citationSet := stringSliceSet(evalCase.CitationIDs)
	missingCitations := make([]string, 0)
	for chunkID := range expectedSet {
		if !citationSet[chunkID] || !retrievedSet[chunkID] {
			missingCitations = append(missingCitations, chunkID)
		}
	}
	sort.Strings(missingCitations)
	unsupported := strings.Contains(strings.ToLower(evalCase.Answer), "unsupported")
	caseResult := KnowledgeEvalCaseResult{
		ID:                 evalCase.ID,
		RetrievalHit:       retrievalHit,
		CitationCovered:    len(missingCitations) == 0 && len(expectedSet) > 0,
		UnsupportedAnswer:  unsupported,
		RetrievedChunkIDs:  retrieved,
		MissingCitationIDs: missingCitations,
	}
	var gaps []KnowledgeGapInput
	if !retrievalHit {
		gaps = append(gaps, KnowledgeGapInput{
			Consumer:    strings.TrimSpace(evalCase.Consumer),
			Domain:      strings.TrimSpace(evalCase.Domain),
			Fingerprint: knowledgeEvalGapFingerprint(evalCase.Query, evalCase.Consumer, evalCase.Domain),
			Kind:        "zero_hit",
		})
	}
	return caseResult, gaps, nil
}

func knowledgeEvalGapFingerprint(query, consumer, domain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(query) + "\x00" + strings.TrimSpace(consumer) + "\x00" + strings.TrimSpace(domain)))
	return "gap-" + hex.EncodeToString(sum[:12])
}

func stringSliceSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = true
		}
	}
	return result
}
