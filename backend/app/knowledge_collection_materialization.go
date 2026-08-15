package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	KnowledgeCollectionMaterializationSchemaVersion = "knowledge_collection_materialization.v1"
	knowledgeCollectionMaterializationMaxClaims     = 5000
	knowledgeCollectionMaterializationMaxCitations  = 5000
	knowledgeCollectionMaterializationMaxQuoted     = 5000000
)

var (
	ErrKnowledgeCollectionMaterializationConflict = errors.New("knowledge collection materialization conflicts with immutable content")
	ErrKnowledgeCollectionMaterializationInvalid  = errors.New("knowledge collection release cannot be materialized")
)

type KnowledgeCollectionMaterialization struct {
	SchemaVersion             string `json:"schema_version"`
	SourceCollectionReleaseID string `json:"source_collection_release_id"`
	SourceContentHash         string `json:"source_content_hash"`
	TargetReleaseID           string `json:"target_release_id"`
	TargetContentHash         string `json:"target_content_hash"`
	MemberCount               int    `json:"member_count"`
	ClaimCount                int    `json:"claim_count"`
	CitationCount             int    `json:"citation_count"`
	CreatedAt                 string `json:"created_at"`
}

type KnowledgeCollectionMaterializationResult struct {
	Materialization KnowledgeCollectionMaterialization `json:"materialization"`
	Release         KnowledgeRelease                   `json:"release"`
}

func (s *BookKnowledgeStore) knowledgeCollectionMaterializationsDir() string {
	return filepath.Join(s.KnowledgeReleaseDir(), "collection-materializations")
}

func (s *BookKnowledgeStore) KnowledgeCollectionMaterializationPath(releaseID string) string {
	return filepath.Join(s.knowledgeCollectionMaterializationsDir(), sanitizeBookKnowledgeID(releaseID)+".json")
}

func (s *BookKnowledgeStore) MaterializeKnowledgeCollectionRelease(releaseID string) (*KnowledgeCollectionMaterializationResult, bool, error) {
	if s == nil {
		return nil, false, fmt.Errorf("book knowledge store is required")
	}
	releaseID = strings.TrimSpace(releaseID)
	if !knowledgeCollectionReleaseIDPattern.MatchString(releaseID) {
		return nil, false, fmt.Errorf("collection release_id is invalid")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rootLock, err := s.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		return nil, false, err
	}
	defer rootLock.Close()

	collection, err := s.loadKnowledgeCollectionReleaseUnlocked(releaseID)
	if err != nil {
		return nil, false, err
	}
	result, err := s.projectKnowledgeCollectionReleaseUnlocked(*collection)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrKnowledgeCollectionMaterializationInvalid, err)
	}
	materializationPath := s.KnowledgeCollectionMaterializationPath(releaseID)
	var existing KnowledgeCollectionMaterialization
	if err := readJSONFile(materializationPath, &existing); err == nil {
		if !reflect.DeepEqual(existing, result.Materialization) {
			return nil, false, ErrKnowledgeCollectionMaterializationConflict
		}
		var target KnowledgeRelease
		if err := readJSONFile(s.KnowledgeReleasePath(existing.TargetReleaseID), &target); err != nil {
			if os.IsNotExist(err) {
				return nil, false, fmt.Errorf("%w: target release is missing", ErrKnowledgeCollectionMaterializationConflict)
			}
			return nil, false, err
		}
		if !reflect.DeepEqual(target, result.Release) {
			return nil, false, ErrKnowledgeCollectionMaterializationConflict
		}
		result.Release = target
		return result, false, nil
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	manifest, err := s.loadKnowledgeReleaseManifestUnlocked()
	if err != nil {
		return nil, false, err
	}
	wantRecord := KnowledgeReleaseRecord{
		ReleaseID: result.Release.ReleaseID, BookID: result.Release.BookID,
		ContentHash: result.Release.ContentHash, Supersedes: result.Release.Supersedes,
		UsagePolicy: result.Release.UsagePolicy, CreatedAt: result.Release.CreatedAt,
	}
	for _, record := range manifest.Releases {
		if record.ReleaseID == wantRecord.ReleaseID && !reflect.DeepEqual(record, wantRecord) {
			return nil, false, fmt.Errorf("%w: target manifest record changed", ErrKnowledgeCollectionMaterializationConflict)
		}
	}

	var existingRelease KnowledgeRelease
	if err := readJSONFile(s.KnowledgeReleasePath(result.Release.ReleaseID), &existingRelease); err == nil {
		if !reflect.DeepEqual(existingRelease, result.Release) {
			return nil, false, ErrKnowledgeCollectionMaterializationConflict
		}
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	if err := s.saveKnowledgeReleaseUnlocked(result.Release); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(s.knowledgeCollectionMaterializationsDir(), 0o700); err != nil {
		return nil, false, err
	}
	if err := writeJSONFile(materializationPath, result.Materialization); err != nil {
		return nil, false, err
	}
	return result, true, nil
}

func (s *BookKnowledgeStore) loadKnowledgeCollectionReleaseUnlocked(releaseID string) (*KnowledgeCollectionRelease, error) {
	var release KnowledgeCollectionRelease
	if err := readJSONFile(s.KnowledgeCollectionReleasePath(releaseID), &release); err != nil {
		return nil, err
	}
	if release.ReleaseID != releaseID || release.SchemaVersion != KnowledgeCollectionReleaseSchemaVersion {
		return nil, fmt.Errorf("%w: release contract is invalid", ErrKnowledgeCollectionMaterializationInvalid)
	}
	hash, expectedID, err := knowledgeCollectionReleaseIdentity(release)
	if err != nil || hash != release.ContentHash || expectedID != release.ReleaseID {
		return nil, fmt.Errorf("%w: release integrity check failed", ErrKnowledgeCollectionMaterializationInvalid)
	}
	if release.UsagePolicy != BookUsageEvidenceOnly || release.Quality.Decision != BookQualityPass ||
		release.Quality.UsagePolicy != BookUsageEvidenceOnly || len(release.Members) == 0 {
		return nil, fmt.Errorf("%w: release is not eligible", ErrKnowledgeCollectionMaterializationInvalid)
	}
	if err := ValidateKnowledgeCollectionDefinition(release.Definition); err != nil {
		return nil, fmt.Errorf("%w: release definition is invalid", ErrKnowledgeCollectionMaterializationInvalid)
	}
	if len(release.Members) > KnowledgeCollectionMaxMembers {
		return nil, fmt.Errorf("%w: release exceeds member limit", ErrKnowledgeCollectionMaterializationInvalid)
	}
	return &release, nil
}

func (s *BookKnowledgeStore) projectKnowledgeCollectionReleaseUnlocked(collection KnowledgeCollectionRelease) (*KnowledgeCollectionMaterializationResult, error) {
	bookID := "collection-knowledge-" + strings.TrimPrefix(collection.ReleaseID, "collection-release-")
	claims := make([]BookAnalysisClaim, 0)
	citations := make([]BookKnowledgeCitation, 0)
	quotedRunes := 0
	for _, member := range collection.Members {
		article, err := s.loadPackageUnlocked(member.BookID)
		if err != nil {
			return nil, fmt.Errorf("load pinned collection member %q: %w", boundedEvidenceID(member.BookID), err)
		}
		if err := validateKnowledgeCollectionMaterializationMember(collection, member, *article); err != nil {
			return nil, err
		}
		citationByID := make(map[string]BookKnowledgeCitation, len(article.Citations))
		for _, citation := range article.Citations {
			if _, duplicate := citationByID[citation.CitationID]; duplicate {
				return nil, fmt.Errorf("pinned collection member %q has duplicate citation identifiers", boundedEvidenceID(member.BookID))
			}
			citationByID[citation.CitationID] = citation
		}
		chunkByID := make(map[string]BookKnowledgeChunk, len(article.Chunks))
		for _, chunk := range article.Chunks {
			if _, duplicate := chunkByID[chunk.ChunkID]; duplicate {
				return nil, fmt.Errorf("pinned collection member %q has duplicate chunk identifiers", boundedEvidenceID(member.BookID))
			}
			chunkByID[chunk.ChunkID] = chunk
		}
		allowed := sortedUniqueStrings(member.CitationIDs)
		if len(allowed) != len(member.CitationIDs) {
			return nil, fmt.Errorf("pinned collection member %q has non-canonical citation allowlist", boundedEvidenceID(member.BookID))
		}
		for _, localCitationID := range allowed {
			citation, ok := citationByID[localCitationID]
			if !ok {
				return nil, fmt.Errorf("pinned collection member %q citation allowlist cannot be resolved", boundedEvidenceID(member.BookID))
			}
			chunk, ok := chunkByID[citation.ChunkID]
			if !ok || strings.TrimSpace(chunk.Text) == "" {
				return nil, fmt.Errorf("pinned collection member %q cited chunk cannot be resolved", boundedEvidenceID(member.BookID))
			}
			if citation.CitationID == "" || citation.BookID != member.BookID ||
				citation.SourceType != collection.Definition.SourceType || citation.SourceItemKey != member.SourceItemKey ||
				citation.SourceAccount != collection.Definition.SourceAccount || citation.ChapterID != chunk.ChapterID ||
				chunk.BookID != member.BookID {
				return nil, fmt.Errorf("pinned collection member %q citation source identity changed", boundedEvidenceID(member.BookID))
			}
			namespace := knowledgeCollectionMaterializationNamespace(member)
			chapterID := namespace + "-chapter-" + opaqueKnowledgeCollectionMaterializationID(citation.ChapterID)
			chunkID := namespace + "-chunk-" + opaqueKnowledgeCollectionMaterializationID(citation.ChunkID)
			citationID := namespace + "-citation-" + opaqueKnowledgeCollectionMaterializationID(citation.CitationID)
			claimID := namespace + "-claim-" + opaqueKnowledgeCollectionMaterializationID(citation.CitationID+"\x00"+citation.ChunkID)
			statement := strings.TrimSpace(chunk.Text)
			claims = append(claims, BookAnalysisClaim{
				ID: claimID, Statement: statement, CitationIDs: []string{citationID},
				Confidence: 1, Scope: []string{collection.Definition.SourceType, member.SourceItemKey}, RiskLevel: "low",
			})
			citations = append(citations, BookKnowledgeCitation{
				CitationID: citationID, BookID: bookID, ChapterID: chapterID, ChunkID: chunkID,
				SourceHTML: citation.SourceHTML, Anchor: citation.Anchor, Note: citation.Note,
				SourceType: collection.Definition.SourceType, SourceAccount: collection.Definition.SourceAccount,
				SourceItemKey: member.SourceItemKey, PublishedAt: firstNonEmpty(member.PublishedAt, article.Book.PublishedAt),
			})
			quotedRunes += utf8.RuneCountInString(statement)
			if err := validateKnowledgeCollectionMaterializationLimits(len(claims), len(citations), quotedRunes); err != nil {
				return nil, err
			}
		}
	}
	if len(claims) == 0 || len(citations) == 0 {
		return nil, fmt.Errorf("knowledge collection materialization has no projectable evidence")
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].ID < claims[j].ID })
	sort.Slice(citations, func(i, j int) bool { return citations[i].CitationID < citations[j].CitationID })

	projectionHash, err := knowledgeCollectionProjectionHash(collection, claims, citations)
	if err != nil {
		return nil, err
	}
	book := BookKnowledgeBook{
		BookID: bookID, Title: collection.Definition.Title, Author: collection.Definition.SourceAccount,
		SourceType: collection.Definition.SourceType, SourceKey: collection.ReleaseID,
		SourceAccount: collection.Definition.SourceAccount, ContentHash: projectionHash,
		CreatedAt: collection.CreatedAt, UpdatedAt: collection.CreatedAt, Status: "published",
		Extractor: "collection-materialization.v1",
	}
	analysis := BookAnalysisPayload{
		Summary: "Grounded evidence materialized from an immutable knowledge collection release.",
		Claims:  claims, Risks: []BookAnalysisRisk{}, Actions: []BookAnalysisAction{},
	}
	analysisHash, err := bookAnalysisHash(BookAnalysisManifest{
		ContentHash: projectionHash, Model: "deterministic", PromptVersion: "collection-materialization.v1",
		Payload: &analysis, Sources: []BookKnowledgeChatSource{},
	})
	if err != nil {
		return nil, err
	}
	quality := BookQualityReport{
		Version: bookQualityVersion, BookID: bookID, ContentHash: projectionHash, AnalysisHash: analysisHash,
		Decision: BookQualityPass, UsagePolicy: BookUsageEvidenceOnly,
		Rules:       []BookQualityRule{{ID: "immutable_collection_projection", Passed: true, Hard: true}},
		EvaluatedAt: collection.CreatedAt,
	}
	targetReleaseID, err := knowledgeReleaseID(book, analysis, quality, []BookKnowledgeChatSource{}, citations)
	if err != nil {
		return nil, err
	}
	release := KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion, Version: knowledgeReleaseVersion,
		ReleaseID: targetReleaseID, BookID: bookID, ContentHash: projectionHash,
		UsagePolicy: BookUsageEvidenceOnly, Book: book, Analysis: &analysis, Quality: quality,
		Sources: []BookKnowledgeChatSource{}, Citations: citations, CreatedAt: collection.CreatedAt,
	}
	materialization := KnowledgeCollectionMaterialization{
		SchemaVersion:             KnowledgeCollectionMaterializationSchemaVersion,
		SourceCollectionReleaseID: collection.ReleaseID, SourceContentHash: collection.ContentHash,
		TargetReleaseID: release.ReleaseID, TargetContentHash: release.ContentHash,
		MemberCount: len(collection.Members), ClaimCount: len(claims), CitationCount: len(citations),
		CreatedAt: collection.CreatedAt,
	}
	return &KnowledgeCollectionMaterializationResult{Materialization: materialization, Release: release}, nil
}

func validateKnowledgeCollectionMaterializationMember(collection KnowledgeCollectionRelease, member KnowledgeCollectionMember, article BookKnowledgePackage) error {
	computedHash, err := BookKnowledgeContentHash(article)
	storedHash := strings.TrimSpace(article.Book.ContentHash)
	if err != nil || computedHash != member.ContentHash ||
		(storedHash != member.ContentHash && !isLegacySourceArticleContentHash(article.Book.SourceType, storedHash)) {
		return fmt.Errorf("pinned collection member %q content hash changed", boundedEvidenceID(member.BookID))
	}
	if article.Book.BookID != member.BookID || article.Book.SourceType != collection.Definition.SourceType ||
		article.Book.SourceKey != member.SourceItemKey || article.Book.SourceAccount != collection.Definition.SourceAccount ||
		(member.PublishedAt != "" && article.Book.PublishedAt != member.PublishedAt) {
		return fmt.Errorf("pinned collection member %q source identity changed", boundedEvidenceID(member.BookID))
	}
	if len(member.CitationIDs) == 0 {
		return fmt.Errorf("pinned collection member %q has no citation allowlist", boundedEvidenceID(member.BookID))
	}
	return nil
}

func validateKnowledgeCollectionMaterializationLimits(claims, citations, quotedRunes int) error {
	if claims > knowledgeCollectionMaterializationMaxClaims ||
		citations > knowledgeCollectionMaterializationMaxCitations || quotedRunes > knowledgeCollectionMaterializationMaxQuoted {
		return fmt.Errorf("knowledge collection materialization exceeds aggregate evidence limits")
	}
	return nil
}

func knowledgeCollectionMaterializationNamespace(member KnowledgeCollectionMember) string {
	sum := sha256.Sum256([]byte(member.BookID + "\x00" + member.ContentHash + "\x00" + member.SourceID + "\x00" + member.SourceItemKey))
	return "member-" + hex.EncodeToString(sum[:8])
}

func opaqueKnowledgeCollectionMaterializationID(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:8])
}

func knowledgeCollectionProjectionHash(collection KnowledgeCollectionRelease, claims []BookAnalysisClaim, citations []BookKnowledgeCitation) (string, error) {
	seed := struct {
		SchemaVersion string                  `json:"schema_version"`
		ReleaseID     string                  `json:"release_id"`
		ContentHash   string                  `json:"content_hash"`
		Claims        []BookAnalysisClaim     `json:"claims"`
		Citations     []BookKnowledgeCitation `json:"citations"`
	}{KnowledgeCollectionMaterializationSchemaVersion, collection.ReleaseID, collection.ContentHash, claims, citations}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
