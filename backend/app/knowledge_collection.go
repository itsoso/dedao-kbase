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
)

var (
	ErrKnowledgeCollectionNotFound       = errors.New("knowledge collection not found")
	ErrKnowledgeCollectionSourceConflict = errors.New("knowledge collection source identity conflict")
	knowledgeCollectionIDPattern         = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
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
	Definition    KnowledgeCollectionDefinition    `json:"definition"`
	CandidateHash string                           `json:"candidate_hash"`
	Members       []KnowledgeCollectionMember      `json:"members"`
	Quality       KnowledgeCollectionQualityReport `json:"quality"`
	UsagePolicy   string                           `json:"usage_policy"`
	CreatedAt     string                           `json:"created_at"`
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
