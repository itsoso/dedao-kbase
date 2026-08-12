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
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	KnowledgeCollectionDefinitionSchemaVersion = "knowledge_collection_definition.v1"
	KnowledgeCollectionCandidateSchemaVersion  = "knowledge_collection_candidate.v1"
	KnowledgeCollectionQualitySchemaVersion    = "knowledge_collection_quality.v1"
	KnowledgeCollectionReleaseSchemaVersion    = "knowledge_collection_release.v1"

	KnowledgeCollectionCandidateReady   = "ready"
	KnowledgeCollectionCandidatePartial = "partial"
	KnowledgeCollectionCandidateBlocked = "blocked"

	KnowledgeCollectionMaxMembers    = 500
	knowledgeCollectionMaxExclusions = 500
	knowledgeCollectionManifestName  = "manifest.json"
	knowledgeCollectionCandidateName = "candidate.json"
	knowledgeCollectionQualityName   = "quality.json"
)

var (
	ErrKnowledgeCollectionNotFound       = errors.New("knowledge collection not found")
	ErrKnowledgeCollectionSourceConflict = errors.New("knowledge collection source identity conflict")
	knowledgeCollectionIDPattern         = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	knowledgeCollectionReleaseIDPattern  = regexp.MustCompile(`^collection-release-[a-f0-9]{24}$`)
)

type KnowledgeCollectionDefinition struct {
	SchemaVersion    string `json:"schema_version"`
	CollectionID     string `json:"collection_id"`
	Title            string `json:"title"`
	SourceType       string `json:"source_type"`
	SourceAccountKey string `json:"source_account_key"`
	SourceAccount    string `json:"source_account"`
	Enabled          bool   `json:"enabled"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type KnowledgeCollectionMember struct {
	BookID        string   `json:"book_id"`
	ContentHash   string   `json:"content_hash"`
	SourceID      string   `json:"source_id"`
	SourceItemKey string   `json:"source_item_key"`
	PublishedAt   string   `json:"published_at,omitempty"`
	CitationIDs   []string `json:"citation_ids"`
}

type KnowledgeCollectionExclusion struct {
	BookID        string `json:"book_id,omitempty"`
	SourceItemKey string `json:"source_item_key,omitempty"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

type KnowledgeCollectionCandidate struct {
	SchemaVersion string                         `json:"schema_version"`
	CollectionID  string                         `json:"collection_id"`
	CandidateHash string                         `json:"candidate_hash"`
	Status        string                         `json:"status"`
	MemberCount   int                            `json:"member_count"`
	Members       []KnowledgeCollectionMember    `json:"members"`
	Exclusions    []KnowledgeCollectionExclusion `json:"exclusions,omitempty"`
	BuiltAt       string                         `json:"built_at"`
}

type KnowledgeCollectionQualityRule struct {
	ID      string `json:"id"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
	Hard    bool   `json:"hard,omitempty"`
}

type KnowledgeCollectionQualityReport struct {
	SchemaVersion string                           `json:"schema_version"`
	CollectionID  string                           `json:"collection_id"`
	CandidateHash string                           `json:"candidate_hash"`
	Decision      string                           `json:"decision"`
	UsagePolicy   string                           `json:"usage_policy"`
	Rules         []KnowledgeCollectionQualityRule `json:"rules"`
	EvaluatedAt   string                           `json:"evaluated_at"`
}

type KnowledgeCollectionRelease struct {
	SchemaVersion string                           `json:"schema_version"`
	ReleaseID     string                           `json:"release_id"`
	CollectionID  string                           `json:"collection_id"`
	ContentHash   string                           `json:"content_hash"`
	Supersedes    string                           `json:"supersedes,omitempty"`
	Definition    KnowledgeCollectionDefinition    `json:"definition"`
	CandidateHash string                           `json:"candidate_hash"`
	Members       []KnowledgeCollectionMember      `json:"members"`
	Quality       KnowledgeCollectionQualityReport `json:"quality"`
	UsagePolicy   string                           `json:"usage_policy"`
	CreatedAt     string                           `json:"created_at"`
}

type KnowledgeCollectionReleaseRecord struct {
	ReleaseID     string `json:"release_id"`
	CollectionID  string `json:"collection_id"`
	ContentHash   string `json:"content_hash"`
	Supersedes    string `json:"supersedes,omitempty"`
	CandidateHash string `json:"candidate_hash"`
	MemberCount   int    `json:"member_count"`
	CreatedAt     string `json:"created_at"`
}

type knowledgeCollectionReleaseManifest struct {
	SchemaVersion string                             `json:"schema_version"`
	Releases      []KnowledgeCollectionReleaseRecord `json:"releases"`
	UpdatedAt     string                             `json:"updated_at,omitempty"`
}

type knowledgeCollectionManifest struct {
	SchemaVersion string                          `json:"schema_version"`
	Collections   []KnowledgeCollectionDefinition `json:"collections"`
	UpdatedAt     string                          `json:"updated_at,omitempty"`
}

func ValidateKnowledgeCollectionDefinition(definition KnowledgeCollectionDefinition) error {
	if definition.SchemaVersion != KnowledgeCollectionDefinitionSchemaVersion {
		return fmt.Errorf("schema_version must be %q", KnowledgeCollectionDefinitionSchemaVersion)
	}
	if definition.CollectionID == "" || definition.CollectionID != strings.TrimSpace(definition.CollectionID) ||
		!knowledgeCollectionIDPattern.MatchString(definition.CollectionID) || utf8.RuneCountInString(definition.CollectionID) > 128 {
		return fmt.Errorf("collection_id must use canonical form and contain only letters, digits, dot, underscore, or hyphen")
	}
	for name, value := range map[string]string{
		"title": definition.Title, "source_type": definition.SourceType,
		"source_account_key": definition.SourceAccountKey, "source_account": definition.SourceAccount,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
		if value != strings.TrimSpace(value) {
			return fmt.Errorf("%s must use canonical form", name)
		}
	}
	if utf8.RuneCountInString(definition.Title) > 256 || utf8.RuneCountInString(definition.SourceType) > 64 ||
		utf8.RuneCountInString(definition.SourceAccountKey) > 512 || utf8.RuneCountInString(definition.SourceAccount) > 512 {
		return fmt.Errorf("knowledge collection definition field exceeds length limit")
	}
	return nil
}

func FinalizeKnowledgeCollectionCandidate(candidate KnowledgeCollectionCandidate) (*KnowledgeCollectionCandidate, error) {
	if candidate.SchemaVersion != KnowledgeCollectionCandidateSchemaVersion {
		return nil, fmt.Errorf("schema_version must be %q", KnowledgeCollectionCandidateSchemaVersion)
	}
	if candidate.CollectionID == "" || candidate.CollectionID != strings.TrimSpace(candidate.CollectionID) ||
		!knowledgeCollectionIDPattern.MatchString(candidate.CollectionID) {
		return nil, fmt.Errorf("collection_id is invalid")
	}
	switch candidate.Status {
	case KnowledgeCollectionCandidateReady, KnowledgeCollectionCandidatePartial, KnowledgeCollectionCandidateBlocked:
	default:
		return nil, fmt.Errorf("candidate status is invalid")
	}
	if len(candidate.Members) > KnowledgeCollectionMaxMembers {
		return nil, fmt.Errorf("members must not exceed %d items", KnowledgeCollectionMaxMembers)
	}
	if len(candidate.Exclusions) > knowledgeCollectionMaxExclusions {
		return nil, fmt.Errorf("exclusions must not exceed %d items", knowledgeCollectionMaxExclusions)
	}
	members := append([]KnowledgeCollectionMember(nil), candidate.Members...)
	for index := range members {
		member := &members[index]
		if err := requireContractFields(map[string]string{
			"book_id": member.BookID, "content_hash": member.ContentHash,
			"source_id": member.SourceID, "source_item_key": member.SourceItemKey,
		}); err != nil {
			return nil, fmt.Errorf("members[%d]: %w", index, err)
		}
		member.CitationIDs = sortedUniqueStrings(member.CitationIDs)
		if len(member.CitationIDs) == 0 {
			return nil, fmt.Errorf("members[%d].citation_ids is required", index)
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].BookID != members[j].BookID {
			return members[i].BookID < members[j].BookID
		}
		return members[i].SourceItemKey < members[j].SourceItemKey
	})
	seenBooks := make(map[string]bool, len(members))
	seenItems := make(map[string]bool, len(members))
	for _, member := range members {
		if seenBooks[member.BookID] {
			return nil, fmt.Errorf("duplicate member book_id %q", boundedEvidenceID(member.BookID))
		}
		if seenItems[member.SourceItemKey] {
			return nil, fmt.Errorf("duplicate member source_item_key %q", boundedEvidenceID(member.SourceItemKey))
		}
		seenBooks[member.BookID] = true
		seenItems[member.SourceItemKey] = true
	}
	exclusions := append([]KnowledgeCollectionExclusion(nil), candidate.Exclusions...)
	for index, exclusion := range exclusions {
		if strings.TrimSpace(exclusion.Code) == "" || strings.TrimSpace(exclusion.Message) == "" {
			return nil, fmt.Errorf("exclusions[%d].code and message are required", index)
		}
	}
	sort.Slice(exclusions, func(i, j int) bool {
		if exclusions[i].SourceItemKey != exclusions[j].SourceItemKey {
			return exclusions[i].SourceItemKey < exclusions[j].SourceItemKey
		}
		if exclusions[i].BookID != exclusions[j].BookID {
			return exclusions[i].BookID < exclusions[j].BookID
		}
		return exclusions[i].Code < exclusions[j].Code
	})
	candidate.Members = members
	candidate.Exclusions = exclusions
	candidate.MemberCount = len(members)
	candidate.CandidateHash = ""
	payload := candidate
	payload.BuiltAt = ""
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(encoded)
	candidate.CandidateHash = "sha256:" + hex.EncodeToString(sum[:])
	return &candidate, nil
}

func (s *BookKnowledgeStore) knowledgeCollectionsRoot() string {
	return filepath.Join(s.root, "collections")
}

func (s *BookKnowledgeStore) knowledgeCollectionManifestPath() string {
	return filepath.Join(s.knowledgeCollectionsRoot(), knowledgeCollectionManifestName)
}

func (s *BookKnowledgeStore) knowledgeCollectionDir(collectionID string) string {
	return filepath.Join(s.knowledgeCollectionsRoot(), sanitizeBookKnowledgeID(collectionID))
}

func (s *BookKnowledgeStore) knowledgeCollectionCandidatePath(collectionID string) string {
	return filepath.Join(s.knowledgeCollectionDir(collectionID), knowledgeCollectionCandidateName)
}

func (s *BookKnowledgeStore) knowledgeCollectionQualityPath(collectionID string) string {
	return filepath.Join(s.knowledgeCollectionDir(collectionID), knowledgeCollectionQualityName)
}

func (s *BookKnowledgeStore) knowledgeCollectionReleasesDir() string {
	return filepath.Join(s.knowledgeCollectionsRoot(), "releases")
}

func (s *BookKnowledgeStore) knowledgeCollectionReleaseManifestPath() string {
	return filepath.Join(s.knowledgeCollectionReleasesDir(), knowledgeCollectionManifestName)
}

func (s *BookKnowledgeStore) KnowledgeCollectionReleasePath(releaseID string) string {
	return filepath.Join(s.knowledgeCollectionReleasesDir(), sanitizeBookKnowledgeID(releaseID)+".json")
}

func (s *BookKnowledgeStore) SaveKnowledgeCollection(definition KnowledgeCollectionDefinition) (*KnowledgeCollectionDefinition, error) {
	if s == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	definition.SchemaVersion = strings.TrimSpace(definition.SchemaVersion)
	definition.CollectionID = strings.TrimSpace(definition.CollectionID)
	definition.Title = strings.TrimSpace(definition.Title)
	definition.SourceType = strings.TrimSpace(definition.SourceType)
	definition.SourceAccountKey = strings.TrimSpace(definition.SourceAccountKey)
	definition.SourceAccount = strings.TrimSpace(definition.SourceAccount)
	if err := ValidateKnowledgeCollectionDefinition(definition); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rootLock, err := s.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	if err := os.MkdirAll(s.knowledgeCollectionsRoot(), 0o700); err != nil {
		return nil, err
	}
	manifest, err := s.loadKnowledgeCollectionManifestUnlocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	found := false
	for index, existing := range manifest.Collections {
		if existing.CollectionID != definition.CollectionID && existing.SourceType == definition.SourceType &&
			existing.SourceAccountKey == definition.SourceAccountKey {
			return nil, ErrKnowledgeCollectionSourceConflict
		}
		if existing.CollectionID == definition.CollectionID {
			if existing.SourceType != definition.SourceType || existing.SourceAccountKey != definition.SourceAccountKey {
				return nil, ErrKnowledgeCollectionSourceConflict
			}
			definition.CreatedAt = existing.CreatedAt
			definition.UpdatedAt = now
			manifest.Collections[index] = definition
			found = true
		}
	}
	if !found {
		definition.CreatedAt = now
		definition.UpdatedAt = now
		manifest.Collections = append(manifest.Collections, definition)
	}
	sort.Slice(manifest.Collections, func(i, j int) bool {
		return manifest.Collections[i].CollectionID < manifest.Collections[j].CollectionID
	})
	manifest.SchemaVersion = KnowledgeCollectionDefinitionSchemaVersion
	manifest.UpdatedAt = now
	if err := writeJSONFile(s.knowledgeCollectionManifestPath(), manifest); err != nil {
		return nil, err
	}
	result := definition
	return &result, nil
}

func (s *BookKnowledgeStore) LoadKnowledgeCollection(collectionID string) (*KnowledgeCollectionDefinition, error) {
	collectionID = strings.TrimSpace(collectionID)
	if s == nil || collectionID == "" {
		return nil, ErrKnowledgeCollectionNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	manifest, err := s.loadKnowledgeCollectionManifestUnlocked()
	if err != nil {
		return nil, err
	}
	for _, definition := range manifest.Collections {
		if definition.CollectionID == collectionID {
			result := definition
			return &result, nil
		}
	}
	return nil, ErrKnowledgeCollectionNotFound
}

func (s *BookKnowledgeStore) ListKnowledgeCollections() ([]KnowledgeCollectionDefinition, error) {
	if s == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	manifest, err := s.loadKnowledgeCollectionManifestUnlocked()
	if err != nil {
		return nil, err
	}
	return append([]KnowledgeCollectionDefinition(nil), manifest.Collections...), nil
}

func (s *BookKnowledgeStore) loadKnowledgeCollectionManifestUnlocked() (knowledgeCollectionManifest, error) {
	manifest := knowledgeCollectionManifest{
		SchemaVersion: KnowledgeCollectionDefinitionSchemaVersion,
		Collections:   []KnowledgeCollectionDefinition{},
	}
	if err := readJSONFile(s.knowledgeCollectionManifestPath(), &manifest); err != nil {
		if os.IsNotExist(err) {
			return manifest, nil
		}
		return manifest, err
	}
	if manifest.Collections == nil {
		manifest.Collections = []KnowledgeCollectionDefinition{}
	}
	return manifest, nil
}

func (s *BookKnowledgeStore) BuildKnowledgeCollectionCandidate(collectionID string) (*KnowledgeCollectionCandidate, error) {
	definition, err := s.LoadKnowledgeCollection(collectionID)
	if err != nil {
		return nil, err
	}
	if !definition.Enabled {
		return nil, fmt.Errorf("knowledge collection %q is disabled", boundedEvidenceID(collectionID))
	}
	catalog, err := NewKnowledgeCatalogStore(s.Root(), time.Now)
	if err != nil {
		return nil, err
	}
	defer catalog.Close()
	records, err := catalog.ListCurrentContentVersionsBySourceAccount(
		definition.SourceType, definition.SourceAccountKey, KnowledgeCollectionMaxMembers+1,
	)
	if err != nil {
		return nil, err
	}

	candidate := KnowledgeCollectionCandidate{
		SchemaVersion: KnowledgeCollectionCandidateSchemaVersion,
		CollectionID:  definition.CollectionID,
		Status:        KnowledgeCollectionCandidateReady,
		Members:       []KnowledgeCollectionMember{},
		Exclusions:    []KnowledgeCollectionExclusion{},
		BuiltAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if len(records) > KnowledgeCollectionMaxMembers {
		candidate.Status = KnowledgeCollectionCandidateBlocked
		candidate.Exclusions = append(candidate.Exclusions, KnowledgeCollectionExclusion{
			Code: "member_limit_exceeded", Message: fmt.Sprintf("source account exceeds the %d member limit", KnowledgeCollectionMaxMembers),
		})
		records = nil
	}
	for _, record := range records {
		member, exclusion := s.knowledgeCollectionMemberFromRecord(*definition, record)
		if exclusion != nil {
			candidate.Exclusions = append(candidate.Exclusions, *exclusion)
			continue
		}
		candidate.Members = append(candidate.Members, *member)
	}
	if candidate.Status != KnowledgeCollectionCandidateBlocked {
		switch {
		case len(candidate.Members) == 0:
			candidate.Status = KnowledgeCollectionCandidateBlocked
		case len(candidate.Exclusions) > 0:
			candidate.Status = KnowledgeCollectionCandidatePartial
		default:
			candidate.Status = KnowledgeCollectionCandidateReady
		}
	}
	finalized, err := FinalizeKnowledgeCollectionCandidate(candidate)
	if err != nil {
		return nil, err
	}
	quality := evaluateKnowledgeCollectionCandidate(*definition, *finalized)
	if err := s.saveKnowledgeCollectionBuild(*finalized, quality); err != nil {
		return nil, err
	}
	return finalized, nil
}

func (s *BookKnowledgeStore) knowledgeCollectionMemberFromRecord(definition KnowledgeCollectionDefinition, record KnowledgeCatalogRecord) (*KnowledgeCollectionMember, *KnowledgeCollectionExclusion) {
	exclusion := func(code, message string) (*KnowledgeCollectionMember, *KnowledgeCollectionExclusion) {
		return nil, &KnowledgeCollectionExclusion{
			BookID: record.Version.TargetBookID, SourceItemKey: record.Source.SourceItemKey,
			Code: code, Message: message,
		}
	}
	if record.Source.SourceType != definition.SourceType || record.Source.SourceAccountKey != definition.SourceAccountKey {
		return exclusion("source_scope_mismatch", "catalog record is outside the collection source identity")
	}
	pkg, err := s.LoadPackage(record.Version.TargetBookID)
	if err != nil {
		return exclusion("package_unavailable", "knowledge package cannot be loaded")
	}
	return knowledgeCollectionMemberFromPackage(definition, record, *pkg)
}

func knowledgeCollectionMemberFromPackage(definition KnowledgeCollectionDefinition, record KnowledgeCatalogRecord, pkg BookKnowledgePackage) (*KnowledgeCollectionMember, *KnowledgeCollectionExclusion) {
	exclusion := func(code, message string) (*KnowledgeCollectionMember, *KnowledgeCollectionExclusion) {
		return nil, &KnowledgeCollectionExclusion{
			BookID: record.Version.TargetBookID, SourceItemKey: record.Source.SourceItemKey,
			Code: code, Message: message,
		}
	}
	if pkg.Book.BookID != record.Version.TargetBookID || pkg.Book.SourceType != definition.SourceType || pkg.Book.SourceKey != record.Source.SourceItemKey {
		return exclusion("package_scope_mismatch", "knowledge package source metadata does not match the catalog record")
	}
	computedHash, err := BookKnowledgeContentHash(pkg)
	if err != nil || pkg.Book.ContentHash == "" || pkg.Book.ContentHash != record.Version.ContentHash || computedHash != pkg.Book.ContentHash {
		return exclusion("content_hash_mismatch", "knowledge package content does not match the current catalog version")
	}
	chapterIDs := make(map[string]bool, len(pkg.Chapters))
	for _, chapter := range pkg.Chapters {
		if chapter.ChapterID == "" || chapter.BookID != pkg.Book.BookID {
			return exclusion("invalid_chapter", "knowledge package contains an invalid chapter")
		}
		chapterIDs[chapter.ChapterID] = true
	}
	chunkIDs := make(map[string]string, len(pkg.Chunks))
	for _, chunk := range pkg.Chunks {
		if chunk.ChunkID == "" || chunk.BookID != pkg.Book.BookID || !chapterIDs[chunk.ChapterID] {
			return exclusion("invalid_chunk", "knowledge package contains an invalid chunk")
		}
		chunkIDs[chunk.ChunkID] = chunk.ChapterID
	}
	citationIDs := make([]string, 0, len(pkg.Citations))
	seenCitations := make(map[string]bool, len(pkg.Citations))
	for _, citation := range pkg.Citations {
		if citation.CitationID == "" || seenCitations[citation.CitationID] || citation.BookID != pkg.Book.BookID ||
			citation.SourceType != definition.SourceType || citation.SourceItemKey != record.Source.SourceItemKey ||
			citation.ChunkID == "" || chunkIDs[citation.ChunkID] == "" || citation.ChapterID != chunkIDs[citation.ChunkID] {
			return exclusion("invalid_citation", "knowledge package citations do not resolve to the scoped article content")
		}
		seenCitations[citation.CitationID] = true
		citationIDs = append(citationIDs, citation.CitationID)
	}
	if len(citationIDs) == 0 {
		return exclusion("missing_citations", "knowledge package has no usable citations")
	}
	return &KnowledgeCollectionMember{
		BookID: pkg.Book.BookID, ContentHash: pkg.Book.ContentHash, SourceID: record.Source.SourceID,
		SourceItemKey: record.Source.SourceItemKey, PublishedAt: pkg.Book.PublishedAt,
		CitationIDs: sortedUniqueStrings(citationIDs),
	}, nil
}

func evaluateKnowledgeCollectionCandidate(definition KnowledgeCollectionDefinition, candidate KnowledgeCollectionCandidate) KnowledgeCollectionQualityReport {
	decision := BookQualityPass
	if candidate.Status == KnowledgeCollectionCandidatePartial {
		decision = BookQualityQuarantine
	} else if candidate.Status == KnowledgeCollectionCandidateBlocked {
		decision = BookQualityReject
	}
	noExclusions := len(candidate.Exclusions) == 0
	return KnowledgeCollectionQualityReport{
		SchemaVersion: KnowledgeCollectionQualitySchemaVersion,
		CollectionID:  definition.CollectionID, CandidateHash: candidate.CandidateHash,
		Decision: decision, UsagePolicy: BookUsageEvidenceOnly,
		Rules: []KnowledgeCollectionQualityRule{
			{ID: "source_identity", Passed: definition.SourceType != "" && definition.SourceAccountKey != "", Message: "collection uses an exact catalog source identity", Hard: true},
			{ID: "members_present", Passed: candidate.MemberCount > 0, Message: "collection contains at least one usable member", Hard: true},
			{ID: "member_scope", Passed: noExclusions, Message: "all selected members match the collection source identity", Hard: true},
			{ID: "content_integrity", Passed: noExclusions, Message: "all member content hashes match the current catalog versions", Hard: true},
			{ID: "citation_integrity", Passed: noExclusions, Message: "all member citations resolve to stored chunks", Hard: true},
			{ID: "summary_consistency", Passed: candidate.MemberCount == len(candidate.Members), Message: "candidate summary matches its immutable member list", Hard: true},
		},
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (s *BookKnowledgeStore) saveKnowledgeCollectionBuild(candidate KnowledgeCollectionCandidate, quality KnowledgeCollectionQualityReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rootLock, err := s.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		return err
	}
	defer rootLock.Close()
	if err := os.MkdirAll(s.knowledgeCollectionDir(candidate.CollectionID), 0o700); err != nil {
		return err
	}
	if err := writeJSONFile(s.knowledgeCollectionCandidatePath(candidate.CollectionID), candidate); err != nil {
		return err
	}
	return writeJSONFile(s.knowledgeCollectionQualityPath(candidate.CollectionID), quality)
}

func (s *BookKnowledgeStore) LoadKnowledgeCollectionCandidate(collectionID string) (*KnowledgeCollectionCandidate, error) {
	if s == nil {
		return nil, ErrKnowledgeCollectionNotFound
	}
	var candidate KnowledgeCollectionCandidate
	if err := s.loadKnowledgeCollectionArtifact(collectionID, s.knowledgeCollectionCandidatePath(collectionID), &candidate); err != nil {
		return nil, err
	}
	return &candidate, nil
}

func (s *BookKnowledgeStore) LoadKnowledgeCollectionQuality(collectionID string) (*KnowledgeCollectionQualityReport, error) {
	if s == nil {
		return nil, ErrKnowledgeCollectionNotFound
	}
	var quality KnowledgeCollectionQualityReport
	if err := s.loadKnowledgeCollectionArtifact(collectionID, s.knowledgeCollectionQualityPath(collectionID), &quality); err != nil {
		return nil, err
	}
	return &quality, nil
}

func (s *BookKnowledgeStore) loadKnowledgeCollectionArtifact(collectionID, path string, target any) error {
	collectionID = strings.TrimSpace(collectionID)
	if s == nil || collectionID == "" || !knowledgeCollectionIDPattern.MatchString(collectionID) {
		return ErrKnowledgeCollectionNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return err
	}
	defer rootLock.Close()
	if err := readJSONFile(path, target); err != nil {
		if os.IsNotExist(err) {
			return ErrKnowledgeCollectionNotFound
		}
		return err
	}
	return nil
}

func (s *BookKnowledgeStore) PublishKnowledgeCollection(collectionID string) (*KnowledgeCollectionRelease, error) {
	if s == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	definition, err := s.LoadKnowledgeCollection(collectionID)
	if err != nil {
		return nil, err
	}
	candidate, err := s.LoadKnowledgeCollectionCandidate(collectionID)
	if err != nil {
		return nil, err
	}
	quality, err := s.LoadKnowledgeCollectionQuality(collectionID)
	if err != nil {
		return nil, err
	}
	if candidate.SchemaVersion != KnowledgeCollectionCandidateSchemaVersion || quality.SchemaVersion != KnowledgeCollectionQualitySchemaVersion {
		return nil, fmt.Errorf("knowledge collection build artifact schema is invalid")
	}
	recomputed, err := FinalizeKnowledgeCollectionCandidate(*candidate)
	if err != nil || recomputed.CandidateHash != candidate.CandidateHash {
		return nil, fmt.Errorf("knowledge collection candidate hash is invalid")
	}
	if quality.CandidateHash != candidate.CandidateHash {
		return nil, fmt.Errorf("knowledge collection quality report is stale")
	}
	if quality.Decision != BookQualityPass {
		return nil, fmt.Errorf("knowledge collection release requires quality decision %q, got %q", BookQualityPass, quality.Decision)
	}
	if quality.UsagePolicy != BookUsageEvidenceOnly || candidate.Status != KnowledgeCollectionCandidateReady {
		return nil, fmt.Errorf("knowledge collection release requires a ready evidence-only candidate")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rootLock, err := s.acquireBookKnowledgeRootLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	if err := s.validateKnowledgeCollectionCandidateFreshUnlocked(*definition, *candidate); err != nil {
		return nil, err
	}

	manifest, err := s.loadKnowledgeCollectionReleaseManifestUnlocked()
	if err != nil {
		return nil, err
	}
	release := KnowledgeCollectionRelease{
		SchemaVersion: KnowledgeCollectionReleaseSchemaVersion,
		CollectionID:  definition.CollectionID,
		Definition:    *definition,
		CandidateHash: candidate.CandidateHash,
		Members:       append([]KnowledgeCollectionMember(nil), candidate.Members...),
		Quality:       *quality,
		UsagePolicy:   BookUsageEvidenceOnly,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	for index := len(manifest.Releases) - 1; index >= 0; index-- {
		if manifest.Releases[index].CollectionID == release.CollectionID {
			release.Supersedes = manifest.Releases[index].ReleaseID
			break
		}
	}
	releaseHash, releaseID, err := knowledgeCollectionReleaseIdentity(release)
	if err != nil {
		return nil, err
	}
	release.ContentHash = releaseHash
	release.ReleaseID = releaseID
	path := s.KnowledgeCollectionReleasePath(release.ReleaseID)
	var existing KnowledgeCollectionRelease
	if err := readJSONFile(path, &existing); err == nil {
		if existing.ContentHash != release.ContentHash || existing.CandidateHash != release.CandidateHash || existing.CollectionID != release.CollectionID {
			return nil, fmt.Errorf("immutable knowledge collection release conflicts with existing content")
		}
		if err := s.upsertKnowledgeCollectionReleaseManifestUnlocked(&manifest, existing); err != nil {
			return nil, err
		}
		return &existing, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(s.knowledgeCollectionReleasesDir(), 0o700); err != nil {
		return nil, err
	}
	if err := writeJSONFile(path, release); err != nil {
		return nil, err
	}
	if err := s.upsertKnowledgeCollectionReleaseManifestUnlocked(&manifest, release); err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *BookKnowledgeStore) upsertKnowledgeCollectionReleaseManifestUnlocked(manifest *knowledgeCollectionReleaseManifest, release KnowledgeCollectionRelease) error {
	record := KnowledgeCollectionReleaseRecord{
		ReleaseID: release.ReleaseID, CollectionID: release.CollectionID, ContentHash: release.ContentHash,
		Supersedes: release.Supersedes, CandidateHash: release.CandidateHash, MemberCount: len(release.Members), CreatedAt: release.CreatedAt,
	}
	manifest.SchemaVersion = KnowledgeCollectionReleaseSchemaVersion
	found := false
	for index := range manifest.Releases {
		if manifest.Releases[index].ReleaseID == record.ReleaseID {
			if !reflect.DeepEqual(manifest.Releases[index], record) {
				return fmt.Errorf("immutable knowledge collection release manifest conflicts with existing content")
			}
			found = true
			break
		}
	}
	if !found {
		manifest.Releases = append(manifest.Releases, record)
	}
	sort.Slice(manifest.Releases, func(i, j int) bool {
		if manifest.Releases[i].CreatedAt != manifest.Releases[j].CreatedAt {
			return manifest.Releases[i].CreatedAt < manifest.Releases[j].CreatedAt
		}
		return manifest.Releases[i].ReleaseID < manifest.Releases[j].ReleaseID
	})
	if release.CreatedAt > manifest.UpdatedAt {
		manifest.UpdatedAt = release.CreatedAt
	}
	return writeJSONFile(s.knowledgeCollectionReleaseManifestPath(), *manifest)
}

func knowledgeCollectionReleaseIdentity(release KnowledgeCollectionRelease) (string, string, error) {
	definition := release.Definition
	definition.CreatedAt = ""
	definition.UpdatedAt = ""
	quality := release.Quality
	quality.EvaluatedAt = ""
	payload := struct {
		SchemaVersion string                           `json:"schema_version"`
		CollectionID  string                           `json:"collection_id"`
		Definition    KnowledgeCollectionDefinition    `json:"definition"`
		CandidateHash string                           `json:"candidate_hash"`
		Members       []KnowledgeCollectionMember      `json:"members"`
		Quality       KnowledgeCollectionQualityReport `json:"quality"`
		UsagePolicy   string                           `json:"usage_policy"`
	}{release.SchemaVersion, release.CollectionID, definition, release.CandidateHash, release.Members, quality, release.UsagePolicy}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	return "sha256:" + hash, "collection-release-" + hash[:24], nil
}

func (s *BookKnowledgeStore) validateKnowledgeCollectionCandidateFreshUnlocked(definition KnowledgeCollectionDefinition, candidate KnowledgeCollectionCandidate) error {
	catalog, err := NewKnowledgeCatalogStore(s.Root(), time.Now)
	if err != nil {
		return err
	}
	defer catalog.Close()
	records, err := catalog.ListCurrentContentVersionsBySourceAccount(definition.SourceType, definition.SourceAccountKey, KnowledgeCollectionMaxMembers+1)
	if err != nil {
		return err
	}
	if len(records) != len(candidate.Members) {
		return fmt.Errorf("knowledge collection candidate membership is stale")
	}
	memberBySource := make(map[string]KnowledgeCollectionMember, len(candidate.Members))
	for _, member := range candidate.Members {
		memberBySource[member.SourceID] = member
	}
	for _, record := range records {
		pinned, ok := memberBySource[record.Source.SourceID]
		if !ok {
			return fmt.Errorf("knowledge collection candidate membership is stale")
		}
		pkg, err := s.loadPackageUnlocked(record.Version.TargetBookID)
		if err != nil {
			return fmt.Errorf("knowledge collection member package is unavailable: %w", err)
		}
		current, exclusion := knowledgeCollectionMemberFromPackage(definition, record, *pkg)
		if exclusion != nil || !reflect.DeepEqual(pinned, *current) {
			return fmt.Errorf("knowledge collection member %q is stale", boundedEvidenceID(pinned.BookID))
		}
	}
	return nil
}

func (s *BookKnowledgeStore) LoadKnowledgeCollectionRelease(releaseID string) (*KnowledgeCollectionRelease, error) {
	releaseID = strings.TrimSpace(releaseID)
	if s == nil || !knowledgeCollectionReleaseIDPattern.MatchString(releaseID) {
		return nil, os.ErrNotExist
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	var release KnowledgeCollectionRelease
	if err := readJSONFile(s.KnowledgeCollectionReleasePath(releaseID), &release); err != nil {
		return nil, err
	}
	if release.ReleaseID != releaseID || release.SchemaVersion != KnowledgeCollectionReleaseSchemaVersion {
		return nil, fmt.Errorf("knowledge collection release contract is invalid")
	}
	hash, expectedID, err := knowledgeCollectionReleaseIdentity(release)
	if err != nil || hash != release.ContentHash || expectedID != release.ReleaseID {
		return nil, fmt.Errorf("knowledge collection release integrity check failed")
	}
	return &release, nil
}

func (s *BookKnowledgeStore) ListKnowledgeCollectionReleases(collectionID string) ([]KnowledgeCollectionReleaseRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rootLock, err := s.acquireBookKnowledgeRootReadLock(context.Background())
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	manifest, err := s.loadKnowledgeCollectionReleaseManifestUnlocked()
	if err != nil {
		return nil, err
	}
	collectionID = strings.TrimSpace(collectionID)
	result := make([]KnowledgeCollectionReleaseRecord, 0)
	for _, record := range manifest.Releases {
		if collectionID == "" || record.CollectionID == collectionID {
			result = append(result, record)
		}
	}
	return result, nil
}

func (s *BookKnowledgeStore) loadKnowledgeCollectionReleaseManifestUnlocked() (knowledgeCollectionReleaseManifest, error) {
	manifest := knowledgeCollectionReleaseManifest{
		SchemaVersion: KnowledgeCollectionReleaseSchemaVersion,
		Releases:      []KnowledgeCollectionReleaseRecord{},
	}
	if err := readJSONFile(s.knowledgeCollectionReleaseManifestPath(), &manifest); err != nil {
		if os.IsNotExist(err) {
			return manifest, nil
		}
		return manifest, err
	}
	if manifest.Releases == nil {
		manifest.Releases = []KnowledgeCollectionReleaseRecord{}
	}
	return manifest, nil
}
