package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ResearchEvidenceSchemaVersion = "research_evidence.v1"

	ResearchEvidenceSourceChatlog   = "chatlog_message"
	ResearchEvidenceSourceKnowledge = "knowledge_release"
	ResearchEvidenceSourcePriorRun  = "prior_run"
	ResearchEvidenceSourceDerived   = "derived_analysis"

	ResearchEvidenceRoleDirectObservation = "direct_observation"
	ResearchEvidenceRoleDirectAdvice      = "direct_advice"
	ResearchEvidenceRoleUserHistory       = "user_history"
	ResearchEvidenceRoleArticleOpinion    = "article_opinion"
	ResearchEvidenceRoleExternalEvidence  = "external_evidence"
	ResearchEvidenceRoleDerivedAnalysis   = "derived_analysis"

	ResearchEvidencePrivacyPublic  = "public"
	ResearchEvidencePrivacyPrivate = "private"

	researchEvidenceExcerptMaxRunes  = 1200
	researchEvidenceContentMaxRunes  = 200000
	researchEvidenceItemsMax         = 500
	researchEvidenceIdentityIDsMax   = 32
	researchEvidenceLocatorFieldMax  = 512
	researchEvidenceIdentityFieldMax = 256
)

var (
	ErrResearchEvidenceSourceRole       = errors.New("research evidence source role is invalid")
	ErrResearchEvidenceDerivedForbidden = errors.New("derived evidence cannot originate from a worker result")
	ErrResearchEvidenceSourceChanged    = errors.New("research evidence source changed")
)

type ResearchEvidenceLocator struct {
	WorkerID        string `json:"worker_id,omitempty"`
	ConversationRef string `json:"conversation_ref,omitempty"`
	MessageRef      string `json:"message_ref,omitempty"`
	ReleaseID       string `json:"release_id,omitempty"`
	PriorRunID      string `json:"prior_run_id,omitempty"`
}

type ResearchEvidence struct {
	EvidenceID         string                  `json:"evidence_id"`
	SchemaVersion      string                  `json:"schema_version"`
	SourceType         string                  `json:"source_type"`
	SourceRole         string                  `json:"source_role"`
	AuthorIdentityID   string                  `json:"author_identity_id,omitempty"`
	SubjectIdentityIDs []string                `json:"subject_identity_ids,omitempty"`
	OccurredAt         string                  `json:"occurred_at,omitempty"`
	ContentExcerpt     string                  `json:"content_excerpt,omitempty"`
	Locator            ResearchEvidenceLocator `json:"locator"`
	LocatorHash        string                  `json:"-"`
	ContentHash        string                  `json:"content_hash"`
	Privacy            string                  `json:"privacy"`
	Selected           bool                    `json:"selected"`
}

type ResearchWorkerEvidenceCandidate struct {
	SourceType         string                  `json:"source_type"`
	SourceRole         string                  `json:"source_role"`
	AuthorIdentityID   string                  `json:"author_identity_id,omitempty"`
	SubjectIdentityIDs []string                `json:"subject_identity_ids,omitempty"`
	OccurredAt         string                  `json:"occurred_at,omitempty"`
	Content            string                  `json:"content"`
	Locator            ResearchEvidenceLocator `json:"locator"`
	Privacy            string                  `json:"privacy"`
	Selected           bool                    `json:"selected"`
}

type ResearchWorkerResult struct {
	SearchedSources    []string                          `json:"searched_sources"`
	Items              []ResearchWorkerEvidenceCandidate `json:"items"`
	IdentityCandidates []ResearchIdentityCandidate       `json:"identity_candidates,omitempty"`
	AnchorCandidateRef string                            `json:"anchor_candidate_ref,omitempty"`

	// Retrieval adapters may use these fields while decoding a local response.
	// The normalizer deliberately has no output path for them.
	RawResponseBody string         `json:"-"`
	ContactObject   map[string]any `json:"-"`
	Cookie          string         `json:"-"`
	Authorization   string         `json:"-"`
	LocalPath       string         `json:"-"`
}

type ResearchEvidenceBundle struct {
	Evidence        []ResearchEvidence `json:"evidence"`
	SearchedSources []string           `json:"searched_sources"`
	CitedSources    []string           `json:"cited_sources"`
}

func NormalizeResearchWorkerResult(result ResearchWorkerResult) (ResearchEvidenceBundle, error) {
	if len(result.Items) > researchEvidenceItemsMax {
		return ResearchEvidenceBundle{}, fmt.Errorf("worker result exceeds %d evidence items", researchEvidenceItemsMax)
	}
	searched, err := normalizeResearchScopeSources(result.SearchedSources)
	if err != nil {
		return ResearchEvidenceBundle{}, err
	}
	bundle := ResearchEvidenceBundle{
		Evidence: make([]ResearchEvidence, 0, len(result.Items)), SearchedSources: searched,
	}
	seenItems := make(map[string]bool, len(result.Items))
	contentByLocator := make(map[string]string, len(result.Items))
	cited := make(map[string]bool)
	for _, candidate := range result.Items {
		if !candidate.Selected {
			continue
		}
		evidence, scopeSource, err := normalizeResearchEvidenceCandidate(candidate)
		if err != nil {
			return ResearchEvidenceBundle{}, err
		}
		if previous, ok := contentByLocator[evidence.LocatorHash]; ok && previous != evidence.ContentHash {
			return ResearchEvidenceBundle{}, ErrResearchEvidenceSourceChanged
		}
		contentByLocator[evidence.LocatorHash] = evidence.ContentHash
		key := evidence.LocatorHash + "\n" + evidence.ContentHash
		if seenItems[key] {
			continue
		}
		seenItems[key] = true
		bundle.Evidence = append(bundle.Evidence, evidence)
		if !cited[scopeSource] {
			cited[scopeSource] = true
			bundle.CitedSources = append(bundle.CitedSources, scopeSource)
		}
	}
	bundle.SearchedSources = mergeResearchScopeSources(bundle.SearchedSources, bundle.CitedSources)
	return bundle, nil
}

func normalizeResearchEvidenceCandidate(candidate ResearchWorkerEvidenceCandidate) (ResearchEvidence, string, error) {
	sourceType := strings.TrimSpace(candidate.SourceType)
	sourceRole := strings.TrimSpace(candidate.SourceRole)
	privacy := strings.TrimSpace(candidate.Privacy)
	if sourceType == ResearchEvidenceSourceDerived || sourceRole == ResearchEvidenceRoleDerivedAnalysis {
		return ResearchEvidence{}, "", ErrResearchEvidenceDerivedForbidden
	}
	scopeSource, err := validateResearchEvidenceSourceRole(sourceType, sourceRole, privacy)
	if err != nil {
		return ResearchEvidence{}, "", err
	}
	content := strings.TrimSpace(candidate.Content)
	if content == "" {
		return ResearchEvidence{}, "", fmt.Errorf("selected evidence content is required")
	}
	if !utf8.ValidString(content) || len([]rune(content)) > researchEvidenceContentMaxRunes {
		return ResearchEvidence{}, "", fmt.Errorf("selected evidence content exceeds supported bounds")
	}
	if err := validateResearchEvidenceLocator(sourceType, candidate.Locator); err != nil {
		return ResearchEvidence{}, "", err
	}
	if err := validateResearchIdentityFields(candidate.AuthorIdentityID, candidate.SubjectIdentityIDs); err != nil {
		return ResearchEvidence{}, "", err
	}
	occurredAt := strings.TrimSpace(candidate.OccurredAt)
	if occurredAt != "" {
		if _, err := time.Parse(time.RFC3339, occurredAt); err != nil {
			return ResearchEvidence{}, "", fmt.Errorf("occurred_at must be RFC3339: %w", err)
		}
	}
	locatorJSON, err := json.Marshal(candidate.Locator)
	if err != nil {
		return ResearchEvidence{}, "", err
	}
	locatorHash := researchEvidenceHash(locatorJSON)
	contentHash := researchEvidenceHash([]byte(content))
	idHash := sha256.Sum256([]byte(locatorHash + "\n" + contentHash))
	return ResearchEvidence{
		EvidenceID:         "research-evidence-" + hex.EncodeToString(idHash[:16]),
		SchemaVersion:      ResearchEvidenceSchemaVersion,
		SourceType:         sourceType,
		SourceRole:         sourceRole,
		AuthorIdentityID:   strings.TrimSpace(candidate.AuthorIdentityID),
		SubjectIdentityIDs: append([]string(nil), candidate.SubjectIdentityIDs...),
		OccurredAt:         occurredAt,
		ContentExcerpt:     truncateResearchEvidenceExcerpt(content),
		Locator:            candidate.Locator,
		LocatorHash:        locatorHash,
		ContentHash:        contentHash,
		Privacy:            privacy,
		Selected:           true,
	}, scopeSource, nil
}

func validateResearchEvidenceSourceRole(sourceType, sourceRole, privacy string) (string, error) {
	roles := map[string]map[string]bool{
		ResearchEvidenceSourceChatlog: {
			ResearchEvidenceRoleDirectObservation: true,
			ResearchEvidenceRoleDirectAdvice:      true,
			ResearchEvidenceRoleUserHistory:       true,
		},
		ResearchEvidenceSourceKnowledge: {
			ResearchEvidenceRoleArticleOpinion:   true,
			ResearchEvidenceRoleExternalEvidence: true,
		},
		ResearchEvidenceSourcePriorRun: {
			ResearchEvidenceRoleUserHistory: true,
		},
	}
	if !roles[sourceType][sourceRole] {
		return "", ErrResearchEvidenceSourceRole
	}
	scopeSource, err := researchScopeSourceForEvidence(sourceType)
	if err != nil {
		return "", err
	}
	requiredPrivacy := ResearchEvidencePrivacyPrivate
	if sourceType == ResearchEvidenceSourceKnowledge {
		requiredPrivacy = ResearchEvidencePrivacyPublic
	}
	if privacy != requiredPrivacy {
		return "", fmt.Errorf("source %q requires privacy %q", sourceType, requiredPrivacy)
	}
	return scopeSource, nil
}

func researchScopeSourceForEvidence(sourceType string) (string, error) {
	switch sourceType {
	case ResearchEvidenceSourceChatlog:
		return ResearchSourceChatlog, nil
	case ResearchEvidenceSourceKnowledge:
		return ResearchSourceKnowledge, nil
	case ResearchEvidenceSourcePriorRun:
		return ResearchSourcePriorRuns, nil
	default:
		return "", fmt.Errorf("unsupported evidence source %q", sourceType)
	}
}

func validateResearchEvidenceLocator(sourceType string, locator ResearchEvidenceLocator) error {
	fields := []string{locator.WorkerID, locator.ConversationRef, locator.MessageRef, locator.ReleaseID, locator.PriorRunID}
	for _, value := range fields {
		value = strings.TrimSpace(value)
		if len([]rune(value)) > researchEvidenceLocatorFieldMax || strings.ContainsAny(value, "\r\n\x00") ||
			strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(strings.ToLower(value), "bearer ") {
			return fmt.Errorf("evidence locator must contain bounded opaque references")
		}
	}
	switch sourceType {
	case ResearchEvidenceSourceChatlog:
		if strings.TrimSpace(locator.WorkerID) == "" || strings.TrimSpace(locator.ConversationRef) == "" || strings.TrimSpace(locator.MessageRef) == "" {
			return fmt.Errorf("chatlog evidence requires worker, conversation, and message references")
		}
		if !validResearchOpaqueCandidateRef(locator.ConversationRef) || !validResearchOpaqueCandidateRef(locator.MessageRef) {
			return fmt.Errorf("chatlog evidence requires locally opaque locator references")
		}
	case ResearchEvidenceSourceKnowledge:
		if strings.TrimSpace(locator.ReleaseID) == "" {
			return fmt.Errorf("knowledge evidence requires a release reference")
		}
	case ResearchEvidenceSourcePriorRun:
		if strings.TrimSpace(locator.PriorRunID) == "" {
			return fmt.Errorf("prior-run evidence requires a run reference")
		}
	}
	return nil
}

func validateResearchIdentityFields(author string, subjects []string) error {
	if len(subjects) > researchEvidenceIdentityIDsMax {
		return fmt.Errorf("subject identity count exceeds %d", researchEvidenceIdentityIDsMax)
	}
	values := append([]string{author}, subjects...)
	for _, value := range values {
		if len([]rune(strings.TrimSpace(value))) > researchEvidenceIdentityFieldMax || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("identity reference exceeds supported bounds")
		}
	}
	return nil
}

func normalizeResearchScopeSources(sources []string) ([]string, error) {
	if len(sources) > researchRequestedSourcesMax {
		return nil, fmt.Errorf("searched_sources exceeds %d items", researchRequestedSourcesMax)
	}
	normalized := make([]string, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, raw := range sources {
		source := strings.TrimSpace(raw)
		switch source {
		case ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns:
		default:
			return nil, fmt.Errorf("unsupported research source %q", source)
		}
		if !seen[source] {
			seen[source] = true
			normalized = append(normalized, source)
		}
	}
	return normalized, nil
}

func validateNormalizedResearchEvidence(evidence ResearchEvidence) error {
	if evidence.SchemaVersion != ResearchEvidenceSchemaVersion || !evidence.Selected {
		return fmt.Errorf("only selected %s evidence can be stored", ResearchEvidenceSchemaVersion)
	}
	if _, err := validateResearchEvidenceSourceRole(evidence.SourceType, evidence.SourceRole, evidence.Privacy); err != nil {
		return err
	}
	if err := validateResearchEvidenceLocator(evidence.SourceType, evidence.Locator); err != nil {
		return err
	}
	if err := validateResearchIdentityFields(evidence.AuthorIdentityID, evidence.SubjectIdentityIDs); err != nil {
		return err
	}
	if len([]rune(evidence.ContentExcerpt)) > researchEvidenceExcerptMaxRunes || strings.TrimSpace(evidence.ContentExcerpt) == "" {
		return fmt.Errorf("evidence excerpt is outside supported bounds")
	}
	locatorJSON, err := json.Marshal(evidence.Locator)
	if err != nil {
		return err
	}
	if evidence.LocatorHash != researchEvidenceHash(locatorJSON) || !strings.HasPrefix(evidence.ContentHash, "sha256:") {
		return fmt.Errorf("evidence fingerprints are invalid")
	}
	idHash := sha256.Sum256([]byte(evidence.LocatorHash + "\n" + evidence.ContentHash))
	if evidence.EvidenceID != "research-evidence-"+hex.EncodeToString(idHash[:16]) {
		return fmt.Errorf("evidence identifier does not match its fingerprints")
	}
	return nil
}

func truncateResearchEvidenceExcerpt(content string) string {
	runes := []rune(content)
	if len(runes) <= researchEvidenceExcerptMaxRunes {
		return content
	}
	return string(runes[:researchEvidenceExcerptMaxRunes])
}

func researchEvidenceHash(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
