package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	EvidenceAuditSchemaVersion = "evidence-audit.v1"

	EvidenceAuditQueued    = "queued"
	EvidenceAuditRunning   = "running"
	EvidenceAuditCompleted = "completed"
	EvidenceAuditFailed    = "failed"

	EvidenceAuditVerdictSupported    = "supported"
	EvidenceAuditVerdictContradicted = "contradicted"
	EvidenceAuditVerdictMixed        = "mixed"
	EvidenceAuditVerdictInsufficient = "insufficient"

	EvidenceAuditReleasePrimary    = "primary"
	EvidenceAuditReleaseSupporting = "supporting"

	evidenceAuditMaxReleases            = 32
	evidenceAuditMaxCitationsPerRelease = 512
	evidenceAuditMaxTotalCitations      = 2048
	evidenceAuditMaxTextBytes           = 4096
	evidenceAuditMaxIdentifierBytes     = 256
	evidenceAuditMaxListItems           = 32
)

type EvidenceAudit struct {
	SchemaVersion  string                           `json:"schema_version"`
	AuditID        string                           `json:"audit_id"`
	Status         string                           `json:"status"`
	CreatedAt      string                           `json:"created_at"`
	UpdatedAt      string                           `json:"updated_at"`
	StartedAt      string                           `json:"started_at,omitempty"`
	CompletedAt    string                           `json:"completed_at,omitempty"`
	FailedAt       string                           `json:"failed_at,omitempty"`
	IdempotencyKey string                           `json:"idempotency_key"`
	InputHash      string                           `json:"input_hash"`
	Package        EvidenceAuditPackageRef          `json:"package"`
	EvidencePolicy EvidenceAuditPolicySnapshot      `json:"evidence_policy"`
	Model          EvidenceAuditModelIdentity       `json:"model"`
	Retrieval      EvidenceAuditRetrievalIdentity   `json:"retrieval"`
	Releases       []EvidenceAuditReleaseRef        `json:"releases"`
	Subject        string                           `json:"subject"`
	Scope          string                           `json:"scope"`
	SelectedClaims []string                         `json:"selected_claims"`
	ClaimAudits    []EvidenceAuditClaim             `json:"claim_audits,omitempty"`
	Summary        EvidenceAuditSummary             `json:"summary,omitempty"`
	Proofroom      EvidenceAuditProofroomProjection `json:"proofroom_projection,omitempty"`
	OutputHash     string                           `json:"output_hash,omitempty"`
	TraceID        string                           `json:"trace_id,omitempty"`
	FailureCode    string                           `json:"failure_code,omitempty"`
	FailureSummary string                           `json:"failure_summary,omitempty"`
}

type EvidenceAuditInput struct {
	SchemaVersion  string                         `json:"schema_version"`
	InputHash      string                         `json:"input_hash,omitempty"`
	Package        EvidenceAuditPackageRef        `json:"package"`
	EvidencePolicy EvidenceAuditPolicySnapshot    `json:"evidence_policy"`
	Model          EvidenceAuditModelIdentity     `json:"model"`
	Retrieval      EvidenceAuditRetrievalIdentity `json:"retrieval"`
	Releases       []EvidenceAuditReleaseRef      `json:"releases"`
	Subject        string                         `json:"subject"`
	Scope          string                         `json:"scope"`
	SelectedClaims []string                       `json:"selected_claims"`
}

type EvidenceAuditPackageRef struct {
	PackageID   string `json:"package_id"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
}

type EvidenceAuditPolicySnapshot struct {
	MinimumIndependentSources int `json:"minimum_independent_sources"`
	MaxClaims                 int `json:"max_claims"`
	MaxEvidencePerClaim       int `json:"max_evidence_per_claim"`
}

type EvidenceAuditModelIdentity struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Route    string `json:"route"`
}

type EvidenceAuditRetrievalIdentity struct {
	Strategy         string `json:"strategy"`
	IndexVersion     string `json:"index_version"`
	RerankerVersion  string `json:"reranker_version,omitempty"`
	EmbeddingVersion string `json:"embedding_version,omitempty"`
}

type EvidenceAuditReleaseRef struct {
	ReleaseID           string                     `json:"release_id"`
	ContentHash         string                     `json:"content_hash"`
	Role                string                     `json:"role"`
	SourceType          string                     `json:"source_type"`
	PublicationIdentity string                     `json:"publication_identity"`
	Citations           []EvidenceAuditCitationRef `json:"citations"`
}

type EvidenceAuditCitationRef struct {
	CitationID string `json:"citation_id"`
	ClaimID    string `json:"claim_id"`
	ChunkID    string `json:"chunk_id"`
}

type EvidenceAuditClaim struct {
	SourceClaim         string                     `json:"source_claim"`
	NormalizedStatement string                     `json:"normalized_statement"`
	Verdict             string                     `json:"verdict"`
	Evidence            []EvidenceAuditEvidenceRef `json:"evidence_refs,omitempty"`
	ComputedConfidence  float64                    `json:"computed_confidence"`
	Limitations         []string                   `json:"limitations"`
	KnowledgeGaps       []string                   `json:"knowledge_gaps"`
	ReviewActions       []string                   `json:"review_actions"`
}

type EvidenceAuditEvidenceRef struct {
	ReleaseID           string `json:"release_id"`
	ContentHash         string `json:"content_hash"`
	Role                string `json:"role"`
	SourceType          string `json:"source_type"`
	PublicationIdentity string `json:"publication_identity"`
	ClaimID             string `json:"claim_id"`
	ChunkID             string `json:"chunk_id"`
	CitationID          string `json:"citation_id"`
	Conflict            bool   `json:"conflict,omitempty"`
}

type EvidenceAuditSummary struct {
	Conclusion    string         `json:"conclusion"`
	VerdictCounts map[string]int `json:"verdict_counts"`
	Limitations   []string       `json:"limitations"`
}

type EvidenceAuditProofroomProjection struct {
	SchemaVersion string   `json:"schema_version"`
	Title         string   `json:"title"`
	ReviewItems   []string `json:"review_items"`
}

func EvidenceAuditInputHash(input EvidenceAuditInput) (string, error) {
	normalized, err := normalizeEvidenceAuditInput(input)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func EvidenceAuditOutputHash(audit EvidenceAudit) (string, error) {
	normalized := audit
	normalized.OutputHash = ""
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func FinalizeEvidenceAuditReport(audit EvidenceAudit) (EvidenceAudit, error) {
	audit.OutputHash = ""
	hash, err := EvidenceAuditOutputHash(audit)
	if err != nil {
		return EvidenceAudit{}, err
	}
	audit.OutputHash = hash
	if err := ValidateEvidenceAudit(audit); err != nil {
		return EvidenceAudit{}, err
	}
	return audit, nil
}

func ValidateEvidenceAudit(audit EvidenceAudit) error {
	if audit.SchemaVersion != EvidenceAuditSchemaVersion {
		return fmt.Errorf("schema_version must be %q", EvidenceAuditSchemaVersion)
	}
	if err := requireContractFields(map[string]string{
		"audit_id":                audit.AuditID,
		"created_at":              audit.CreatedAt,
		"updated_at":              audit.UpdatedAt,
		"idempotency_key":         audit.IdempotencyKey,
		"input_hash":              audit.InputHash,
		"package.package_id":      audit.Package.PackageID,
		"package.version":         audit.Package.Version,
		"package.content_hash":    audit.Package.ContentHash,
		"model.provider":          audit.Model.Provider,
		"model.model":             audit.Model.Model,
		"model.route":             audit.Model.Route,
		"retrieval.strategy":      audit.Retrieval.Strategy,
		"retrieval.index_version": audit.Retrieval.IndexVersion,
		"subject":                 audit.Subject,
		"scope":                   audit.Scope,
	}); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"audit_id":        audit.AuditID,
		"status":          audit.Status,
		"idempotency_key": audit.IdempotencyKey,
		"input_hash":      audit.InputHash,
		"trace_id":        audit.TraceID,
		"failure_code":    audit.FailureCode,
	} {
		if err := validateEvidenceAuditString(name, value, evidenceAuditMaxIdentifierBytes); err != nil {
			return err
		}
	}
	if err := validateEvidenceAuditString(
		"failure_summary",
		audit.FailureSummary,
		evidenceAuditMaxTextBytes,
	); err != nil {
		return err
	}
	if err := validateEvidenceAuditTimestamp("created_at", audit.CreatedAt); err != nil {
		return err
	}
	if err := validateEvidenceAuditTimestamp("updated_at", audit.UpdatedAt); err != nil {
		return err
	}
	if err := validateEvidenceAuditInput(auditInputFromAudit(audit)); err != nil {
		return err
	}
	wantInputHash, err := EvidenceAuditInputHash(auditInputFromAudit(audit))
	if err != nil {
		return err
	}
	if audit.InputHash != wantInputHash {
		return fmt.Errorf("input_hash does not match deterministic audit input")
	}
	if !validEvidenceAuditSHA256(audit.InputHash) {
		return fmt.Errorf("input_hash must be a sha256 digest")
	}
	switch audit.Status {
	case EvidenceAuditQueued:
		if evidenceAuditHasPartialReport(audit) {
			return fmt.Errorf("queued status cannot contain a partial report")
		}
		if audit.StartedAt != "" || audit.CompletedAt != "" || audit.FailedAt != "" ||
			audit.TraceID != "" || audit.FailureCode != "" || audit.FailureSummary != "" {
			return fmt.Errorf("queued status cannot contain terminal metadata")
		}
	case EvidenceAuditRunning:
		if evidenceAuditHasPartialReport(audit) {
			return fmt.Errorf("running status cannot contain a partial report")
		}
		if audit.StartedAt == "" || audit.TraceID == "" {
			return fmt.Errorf("running status requires started_at and trace_id")
		}
		if audit.CompletedAt != "" || audit.FailedAt != "" ||
			audit.FailureCode != "" || audit.FailureSummary != "" {
			return fmt.Errorf("running status cannot contain terminal metadata")
		}
	case EvidenceAuditFailed:
		if evidenceAuditHasPartialReport(audit) {
			return fmt.Errorf("failed status cannot contain a partial report")
		}
		if audit.FailedAt == "" || strings.TrimSpace(audit.FailureCode) == "" ||
			strings.TrimSpace(audit.FailureSummary) == "" {
			return fmt.Errorf("failed status requires failed_at, failure_code, and failure_summary")
		}
		if audit.CompletedAt != "" {
			return fmt.Errorf("failed status cannot contain completed_at")
		}
	case EvidenceAuditCompleted:
		if audit.StartedAt == "" || audit.CompletedAt == "" || audit.TraceID == "" {
			return fmt.Errorf("completed status requires started_at, completed_at, and trace_id")
		}
		if audit.FailedAt != "" || audit.FailureCode != "" || audit.FailureSummary != "" {
			return fmt.Errorf("completed status cannot contain failure metadata")
		}
		if audit.OutputHash == "" {
			return fmt.Errorf("completed status requires output_hash")
		}
		if !validEvidenceAuditSHA256(audit.OutputHash) {
			return fmt.Errorf("output_hash must be a sha256 digest")
		}
		if err := validateCompletedEvidenceAudit(audit); err != nil {
			return err
		}
		wantOutputHash, err := EvidenceAuditOutputHash(audit)
		if err != nil {
			return err
		}
		if audit.OutputHash != wantOutputHash {
			return fmt.Errorf("output_hash does not match complete deterministic report")
		}
	default:
		return fmt.Errorf("unsupported status %q", audit.Status)
	}
	for name, value := range map[string]string{
		"started_at": audit.StartedAt, "completed_at": audit.CompletedAt, "failed_at": audit.FailedAt,
	} {
		if value != "" {
			if err := validateEvidenceAuditTimestamp(name, value); err != nil {
				return err
			}
		}
	}
	return validateEvidenceAuditTimeline(audit)
}

func ComputeEvidenceAuditConfidence(evidence []EvidenceAuditEvidenceRef, conflicts int) float64 {
	if len(evidence) == 0 {
		return 0
	}
	resolvable := 0
	publications := map[string]struct{}{}
	sourceTypes := map[string]struct{}{}
	for _, ref := range evidence {
		if evidenceAuditRefResolvable(ref) {
			resolvable++
			if strings.TrimSpace(ref.Role) == EvidenceAuditReleaseSupporting {
				publications[strings.TrimSpace(ref.PublicationIdentity)] = struct{}{}
				sourceTypes[strings.TrimSpace(ref.SourceType)] = struct{}{}
			}
		}
	}
	completeness := float64(resolvable) / float64(len(evidence))
	independence := math.Min(float64(len(publications))/2, 1)
	diversity := math.Min(float64(len(sourceTypes))/2, 1)
	conflictRatio := math.Min(math.Max(float64(conflicts), 0)/float64(len(evidence)), 1)
	score := completeness * (0.6*independence + 0.4*diversity) * (1 - 0.5*conflictRatio)
	score = math.Max(0, math.Min(score, 1))
	return math.Round(score*100) / 100
}

func validateEvidenceAuditInput(input EvidenceAuditInput) error {
	if input.SchemaVersion != EvidenceAuditSchemaVersion {
		return fmt.Errorf("schema_version must be %q", EvidenceAuditSchemaVersion)
	}
	if err := requireContractFields(map[string]string{
		"package.package_id":      input.Package.PackageID,
		"package.version":         input.Package.Version,
		"package.content_hash":    input.Package.ContentHash,
		"model.provider":          input.Model.Provider,
		"model.model":             input.Model.Model,
		"model.route":             input.Model.Route,
		"retrieval.strategy":      input.Retrieval.Strategy,
		"retrieval.index_version": input.Retrieval.IndexVersion,
		"subject":                 input.Subject,
		"scope":                   input.Scope,
	}); err != nil {
		return err
	}
	if !validEvidenceAuditSHA256(input.Package.ContentHash) {
		return fmt.Errorf("package.content_hash must be a sha256 digest")
	}
	if input.EvidencePolicy.MinimumIndependentSources < 1 {
		return fmt.Errorf("evidence_policy.minimum_independent_sources must be positive")
	}
	if input.EvidencePolicy.MaxClaims < 1 || input.EvidencePolicy.MaxClaims > agentEvidenceMaxClaims {
		return fmt.Errorf("evidence_policy.max_claims must be between 1 and %d", agentEvidenceMaxClaims)
	}
	if input.EvidencePolicy.MaxEvidencePerClaim < 1 ||
		input.EvidencePolicy.MaxEvidencePerClaim > agentEvidenceMaxEvidencePerClaim {
		return fmt.Errorf(
			"evidence_policy.max_evidence_per_claim must be between 1 and %d",
			agentEvidenceMaxEvidencePerClaim,
		)
	}
	for name, value := range map[string]string{
		"package.package_id":          input.Package.PackageID,
		"package.version":             input.Package.Version,
		"model.provider":              input.Model.Provider,
		"model.model":                 input.Model.Model,
		"model.route":                 input.Model.Route,
		"retrieval.strategy":          input.Retrieval.Strategy,
		"retrieval.index_version":     input.Retrieval.IndexVersion,
		"retrieval.reranker_version":  input.Retrieval.RerankerVersion,
		"retrieval.embedding_version": input.Retrieval.EmbeddingVersion,
	} {
		if err := validateEvidenceAuditString(name, value, evidenceAuditMaxIdentifierBytes); err != nil {
			return err
		}
	}
	if err := validateEvidenceAuditString("subject", input.Subject, evidenceAuditMaxTextBytes); err != nil {
		return err
	}
	if err := validateEvidenceAuditString("scope", input.Scope, evidenceAuditMaxTextBytes); err != nil {
		return err
	}
	if len(input.Releases) == 0 || len(input.Releases) > evidenceAuditMaxReleases {
		return fmt.Errorf("releases must contain between 1 and %d immutable releases", evidenceAuditMaxReleases)
	}
	seenReleases := map[string]struct{}{}
	primaryCount := 0
	supportingCount := 0
	supportingPublications := map[string]struct{}{}
	totalCitations := 0
	for index, release := range input.Releases {
		if err := requireContractFields(map[string]string{
			fmt.Sprintf("releases[%d].release_id", index):           release.ReleaseID,
			fmt.Sprintf("releases[%d].content_hash", index):         release.ContentHash,
			fmt.Sprintf("releases[%d].role", index):                 release.Role,
			fmt.Sprintf("releases[%d].source_type", index):          release.SourceType,
			fmt.Sprintf("releases[%d].publication_identity", index): release.PublicationIdentity,
		}); err != nil {
			return err
		}
		for name, value := range map[string]string{
			fmt.Sprintf("releases[%d].release_id", index):  release.ReleaseID,
			fmt.Sprintf("releases[%d].role", index):        release.Role,
			fmt.Sprintf("releases[%d].source_type", index): release.SourceType,
		} {
			if err := validateEvidenceAuditString(name, value, evidenceAuditMaxIdentifierBytes); err != nil {
				return err
			}
		}
		if !validEvidenceAuditSHA256(release.ContentHash) {
			return fmt.Errorf("releases[%d].content_hash must be a sha256 digest", index)
		}
		switch release.Role {
		case EvidenceAuditReleasePrimary:
			primaryCount++
		case EvidenceAuditReleaseSupporting:
			supportingCount++
			supportingPublications[release.PublicationIdentity] = struct{}{}
		default:
			return fmt.Errorf("releases[%d].role must be primary or supporting", index)
		}
		if !validEvidenceAuditSHA256(release.PublicationIdentity) {
			return fmt.Errorf("releases[%d].publication_identity must be an immutable sha256 identity", index)
		}
		if len(release.Citations) == 0 || len(release.Citations) > evidenceAuditMaxCitationsPerRelease {
			return fmt.Errorf(
				"releases[%d].citations must contain between 1 and %d bindings",
				index,
				evidenceAuditMaxCitationsPerRelease,
			)
		}
		totalCitations += len(release.Citations)
		if totalCitations > evidenceAuditMaxTotalCitations {
			return fmt.Errorf("releases citations exceed total limit %d", evidenceAuditMaxTotalCitations)
		}
		seenCitations := map[string]struct{}{}
		for citationIndex, citation := range release.Citations {
			if err := requireContractFields(map[string]string{
				fmt.Sprintf("releases[%d].citations[%d].citation_id", index, citationIndex): citation.CitationID,
				fmt.Sprintf("releases[%d].citations[%d].claim_id", index, citationIndex):    citation.ClaimID,
				fmt.Sprintf("releases[%d].citations[%d].chunk_id", index, citationIndex):    citation.ChunkID,
			}); err != nil {
				return err
			}
			for name, value := range map[string]string{
				fmt.Sprintf("releases[%d].citations[%d].citation_id", index, citationIndex): citation.CitationID,
				fmt.Sprintf("releases[%d].citations[%d].claim_id", index, citationIndex):    citation.ClaimID,
				fmt.Sprintf("releases[%d].citations[%d].chunk_id", index, citationIndex):    citation.ChunkID,
			} {
				if err := validateEvidenceAuditString(name, value, evidenceAuditMaxIdentifierBytes); err != nil {
					return err
				}
			}
			citationID := strings.TrimSpace(citation.CitationID)
			if _, duplicate := seenCitations[citationID]; duplicate {
				return fmt.Errorf("releases[%d].citations contains duplicate citation_id %q", index, citationID)
			}
			seenCitations[citationID] = struct{}{}
		}
		releaseID := strings.TrimSpace(release.ReleaseID)
		if _, duplicate := seenReleases[releaseID]; duplicate {
			return fmt.Errorf("releases contains duplicate release_id %q", releaseID)
		}
		seenReleases[releaseID] = struct{}{}
	}
	if primaryCount != 1 || supportingCount == 0 {
		return fmt.Errorf("releases must contain exactly one primary and at least one supporting release")
	}
	if len(supportingPublications) < input.EvidencePolicy.MinimumIndependentSources {
		return fmt.Errorf(
			"releases provide %d independent supporting publications; evidence policy requires %d",
			len(supportingPublications),
			input.EvidencePolicy.MinimumIndependentSources,
		)
	}
	if len(input.SelectedClaims) == 0 || len(input.SelectedClaims) > input.EvidencePolicy.MaxClaims {
		return fmt.Errorf("selected_claims must contain between 1 and %d claims", input.EvidencePolicy.MaxClaims)
	}
	seenClaims := map[string]struct{}{}
	for _, claim := range input.SelectedClaims {
		if err := validateEvidenceAuditString("selected_claims", claim, evidenceAuditMaxTextBytes); err != nil {
			return err
		}
		if _, duplicate := seenClaims[claim]; duplicate {
			return fmt.Errorf("selected_claims contains duplicate claim %q", claim)
		}
		seenClaims[claim] = struct{}{}
	}
	return nil
}

func validateCompletedEvidenceAudit(audit EvidenceAudit) error {
	if len(audit.ClaimAudits) != len(audit.SelectedClaims) {
		return fmt.Errorf("claim_audits must match selected_claims exactly")
	}
	pinnedReleases := make(map[string]EvidenceAuditReleaseRef, len(audit.Releases))
	for _, release := range audit.Releases {
		pinnedReleases[release.ReleaseID] = release
	}
	actualCounts := map[string]int{}
	selectedClaims := make(map[string]struct{}, len(audit.SelectedClaims))
	for _, selected := range audit.SelectedClaims {
		selectedClaims[strings.TrimSpace(selected)] = struct{}{}
	}
	seenClaims := make(map[string]struct{}, len(audit.ClaimAudits))
	for index, claim := range audit.ClaimAudits {
		if err := requireContractFields(map[string]string{
			fmt.Sprintf("claim_audits[%d].source_claim", index):         claim.SourceClaim,
			fmt.Sprintf("claim_audits[%d].normalized_statement", index): claim.NormalizedStatement,
			fmt.Sprintf("claim_audits[%d].verdict", index):              claim.Verdict,
		}); err != nil {
			return err
		}
		if err := validateEvidenceAuditString(
			fmt.Sprintf("claim_audits[%d].source_claim", index),
			claim.SourceClaim,
			evidenceAuditMaxTextBytes,
		); err != nil {
			return err
		}
		if err := validateEvidenceAuditString(
			fmt.Sprintf("claim_audits[%d].normalized_statement", index),
			claim.NormalizedStatement,
			evidenceAuditMaxTextBytes,
		); err != nil {
			return err
		}
		for name, values := range map[string][]string{
			fmt.Sprintf("claim_audits[%d].limitations", index):    claim.Limitations,
			fmt.Sprintf("claim_audits[%d].knowledge_gaps", index): claim.KnowledgeGaps,
			fmt.Sprintf("claim_audits[%d].review_actions", index): claim.ReviewActions,
		} {
			if err := validateEvidenceAuditStringList(name, values); err != nil {
				return err
			}
		}
		sourceClaim := strings.TrimSpace(claim.SourceClaim)
		if _, selected := selectedClaims[sourceClaim]; !selected {
			return fmt.Errorf("claim_audits[%d].source_claim must belong to selected_claims", index)
		}
		if _, duplicate := seenClaims[sourceClaim]; duplicate {
			return fmt.Errorf("claim_audits must match selected_claims without duplicates")
		}
		seenClaims[sourceClaim] = struct{}{}
		switch claim.Verdict {
		case EvidenceAuditVerdictSupported, EvidenceAuditVerdictContradicted, EvidenceAuditVerdictMixed:
			if len(claim.Evidence) == 0 {
				return fmt.Errorf("claim_audits[%d] verdict %q requires evidence", index, claim.Verdict)
			}
		case EvidenceAuditVerdictInsufficient:
		default:
			return fmt.Errorf("claim_audits[%d] has unsupported verdict %q", index, claim.Verdict)
		}
		if len(claim.Evidence) > audit.EvidencePolicy.MaxEvidencePerClaim {
			return fmt.Errorf(
				"claim_audits[%d] exceeds evidence limit %d",
				index,
				audit.EvidencePolicy.MaxEvidencePerClaim,
			)
		}
		conflicts := 0
		seenEvidence := make(map[string]struct{}, len(claim.Evidence))
		supportingPublications := make(map[string]struct{})
		for evidenceIndex, ref := range claim.Evidence {
			if !evidenceAuditRefResolvable(ref) {
				return fmt.Errorf("claim_audits[%d].evidence_refs[%d] requires a pinned citation", index, evidenceIndex)
			}
			release, ok := pinnedReleases[strings.TrimSpace(ref.ReleaseID)]
			if !ok {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] must reference a pinned release",
					index,
					evidenceIndex,
				)
			}
			if strings.TrimSpace(ref.ContentHash) != release.ContentHash {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] content_hash does not match pinned release",
					index,
					evidenceIndex,
				)
			}
			if strings.TrimSpace(ref.Role) != release.Role {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] role does not match pinned release",
					index,
					evidenceIndex,
				)
			}
			if strings.TrimSpace(ref.SourceType) != release.SourceType {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] source_type does not match pinned release",
					index,
					evidenceIndex,
				)
			}
			if strings.TrimSpace(ref.PublicationIdentity) != release.PublicationIdentity {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] publication_identity does not match pinned release",
					index,
					evidenceIndex,
				)
			}
			if !evidenceAuditReleaseAllowsCitation(release, ref) {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] citation binding must match pinned citation from release %q",
					index,
					evidenceIndex,
					release.ReleaseID,
				)
			}
			identity := evidenceAuditEvidenceIdentity(ref)
			if _, duplicate := seenEvidence[identity]; duplicate {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs contains duplicate evidence identity %q",
					index,
					identity,
				)
			}
			seenEvidence[identity] = struct{}{}
			if ref.Role == EvidenceAuditReleaseSupporting {
				supportingPublications[ref.PublicationIdentity] = struct{}{}
			}
			if ref.Conflict {
				conflicts++
			}
		}
		if claim.Verdict != EvidenceAuditVerdictInsufficient &&
			len(supportingPublications) < audit.EvidencePolicy.MinimumIndependentSources {
			return fmt.Errorf(
				"claim_audits[%d] requires at least %d independent supporting publications; use insufficient verdict",
				index,
				audit.EvidencePolicy.MinimumIndependentSources,
			)
		}
		computed := ComputeEvidenceAuditConfidence(claim.Evidence, conflicts)
		if math.Abs(claim.ComputedConfidence-computed) > 0.000001 {
			return fmt.Errorf("claim_audits[%d].computed_confidence must be derived by the audit confidence function", index)
		}
		actualCounts[claim.Verdict]++
	}
	if strings.TrimSpace(audit.Summary.Conclusion) == "" {
		return fmt.Errorf("summary.conclusion is required")
	}
	if err := validateEvidenceAuditString("summary.conclusion", audit.Summary.Conclusion, evidenceAuditMaxTextBytes); err != nil {
		return err
	}
	if err := validateEvidenceAuditStringList("summary.limitations", audit.Summary.Limitations); err != nil {
		return err
	}
	if !equalEvidenceAuditVerdictCounts(actualCounts, audit.Summary.VerdictCounts) {
		return fmt.Errorf("summary.verdict_counts does not match claim audits")
	}
	if strings.TrimSpace(audit.Proofroom.SchemaVersion) == "" ||
		strings.TrimSpace(audit.Proofroom.Title) == "" ||
		len(audit.Proofroom.ReviewItems) == 0 {
		return fmt.Errorf("proofroom_projection must be complete")
	}
	if err := validateEvidenceAuditString(
		"proofroom_projection.schema_version",
		audit.Proofroom.SchemaVersion,
		evidenceAuditMaxIdentifierBytes,
	); err != nil {
		return err
	}
	if err := validateEvidenceAuditString(
		"proofroom_projection.title",
		audit.Proofroom.Title,
		evidenceAuditMaxTextBytes,
	); err != nil {
		return err
	}
	if err := validateEvidenceAuditStringList(
		"proofroom_projection.review_items",
		audit.Proofroom.ReviewItems,
	); err != nil {
		return err
	}
	return nil
}

func evidenceAuditEvidenceIdentity(ref EvidenceAuditEvidenceRef) string {
	return strings.Join([]string{
		strings.TrimSpace(ref.ReleaseID),
		strings.TrimSpace(ref.PublicationIdentity),
		strings.TrimSpace(ref.CitationID),
		strings.TrimSpace(ref.ClaimID),
		strings.TrimSpace(ref.ChunkID),
	}, "\x00")
}

func evidenceAuditRefResolvable(ref EvidenceAuditEvidenceRef) bool {
	if strings.TrimSpace(ref.ReleaseID) == "" ||
		strings.TrimSpace(ref.ContentHash) == "" ||
		strings.TrimSpace(ref.Role) == "" ||
		strings.TrimSpace(ref.SourceType) == "" ||
		strings.TrimSpace(ref.PublicationIdentity) == "" ||
		strings.TrimSpace(ref.CitationID) == "" ||
		strings.TrimSpace(ref.ClaimID) == "" ||
		strings.TrimSpace(ref.ChunkID) == "" {
		return false
	}
	return validEvidenceAuditSHA256(ref.PublicationIdentity)
}

func evidenceAuditReleaseAllowsCitation(release EvidenceAuditReleaseRef, ref EvidenceAuditEvidenceRef) bool {
	for _, allowed := range release.Citations {
		if strings.TrimSpace(allowed.CitationID) == strings.TrimSpace(ref.CitationID) &&
			strings.TrimSpace(allowed.ClaimID) == strings.TrimSpace(ref.ClaimID) &&
			strings.TrimSpace(allowed.ChunkID) == strings.TrimSpace(ref.ChunkID) {
			return true
		}
	}
	return false
}

func validEvidenceAuditSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validateEvidenceAuditString(name, value string, maxBytes int) error {
	if len(value) > maxBytes {
		return fmt.Errorf("%s exceeds byte limit %d", name, maxBytes)
	}
	return nil
}

func validateEvidenceAuditStringList(name string, values []string) error {
	if len(values) > evidenceAuditMaxListItems {
		return fmt.Errorf("%s exceeds item limit %d", name, evidenceAuditMaxListItems)
	}
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] is required", name, index)
		}
		if err := validateEvidenceAuditString(
			fmt.Sprintf("%s[%d]", name, index),
			value,
			evidenceAuditMaxTextBytes,
		); err != nil {
			return err
		}
	}
	return nil
}

func evidenceAuditHasPartialReport(audit EvidenceAudit) bool {
	return len(audit.ClaimAudits) > 0 ||
		strings.TrimSpace(audit.Summary.Conclusion) != "" ||
		len(audit.Summary.VerdictCounts) > 0 ||
		len(audit.Summary.Limitations) > 0 ||
		strings.TrimSpace(audit.Proofroom.SchemaVersion) != "" ||
		strings.TrimSpace(audit.Proofroom.Title) != "" ||
		len(audit.Proofroom.ReviewItems) > 0 ||
		strings.TrimSpace(audit.OutputHash) != ""
}

func normalizeEvidenceAuditInput(input EvidenceAuditInput) (EvidenceAuditInput, error) {
	input.Releases = append([]EvidenceAuditReleaseRef(nil), input.Releases...)
	for index := range input.Releases {
		input.Releases[index].Citations = append([]EvidenceAuditCitationRef(nil), input.Releases[index].Citations...)
	}
	input.InputHash = ""
	input.SchemaVersion = strings.TrimSpace(input.SchemaVersion)
	input.Package.PackageID = strings.TrimSpace(input.Package.PackageID)
	input.Package.Version = strings.TrimSpace(input.Package.Version)
	input.Package.ContentHash = strings.TrimSpace(input.Package.ContentHash)
	input.Model.Provider = strings.TrimSpace(input.Model.Provider)
	input.Model.Model = strings.TrimSpace(input.Model.Model)
	input.Model.Route = strings.TrimSpace(input.Model.Route)
	input.Retrieval.Strategy = strings.TrimSpace(input.Retrieval.Strategy)
	input.Retrieval.IndexVersion = strings.TrimSpace(input.Retrieval.IndexVersion)
	input.Retrieval.RerankerVersion = strings.TrimSpace(input.Retrieval.RerankerVersion)
	input.Retrieval.EmbeddingVersion = strings.TrimSpace(input.Retrieval.EmbeddingVersion)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Scope = strings.TrimSpace(input.Scope)
	for index := range input.Releases {
		input.Releases[index].ReleaseID = strings.TrimSpace(input.Releases[index].ReleaseID)
		input.Releases[index].ContentHash = strings.TrimSpace(input.Releases[index].ContentHash)
		input.Releases[index].Role = strings.TrimSpace(input.Releases[index].Role)
		input.Releases[index].SourceType = strings.TrimSpace(input.Releases[index].SourceType)
		input.Releases[index].PublicationIdentity = strings.TrimSpace(input.Releases[index].PublicationIdentity)
		for citationIndex := range input.Releases[index].Citations {
			citation := &input.Releases[index].Citations[citationIndex]
			citation.CitationID = strings.TrimSpace(citation.CitationID)
			citation.ClaimID = strings.TrimSpace(citation.ClaimID)
			citation.ChunkID = strings.TrimSpace(citation.ChunkID)
		}
		sort.Slice(input.Releases[index].Citations, func(i, j int) bool {
			left := input.Releases[index].Citations[i]
			right := input.Releases[index].Citations[j]
			if left.CitationID != right.CitationID {
				return left.CitationID < right.CitationID
			}
			if left.ClaimID != right.ClaimID {
				return left.ClaimID < right.ClaimID
			}
			return left.ChunkID < right.ChunkID
		})
	}
	sort.Slice(input.Releases, func(i, j int) bool {
		return input.Releases[i].ReleaseID < input.Releases[j].ReleaseID
	})
	input.SelectedClaims = normalizeEvidenceAuditStrings(input.SelectedClaims, false)
	if err := validateEvidenceAuditInput(input); err != nil {
		return EvidenceAuditInput{}, err
	}
	return input, nil
}

func normalizeEvidenceAuditStrings(values []string, sortValues bool) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if sortValues {
		sort.Strings(normalized)
	}
	return normalized
}

func auditInputFromAudit(audit EvidenceAudit) EvidenceAuditInput {
	return EvidenceAuditInput{
		SchemaVersion:  audit.SchemaVersion,
		InputHash:      audit.InputHash,
		Package:        audit.Package,
		EvidencePolicy: audit.EvidencePolicy,
		Model:          audit.Model,
		Retrieval:      audit.Retrieval,
		Releases:       audit.Releases,
		Subject:        audit.Subject,
		Scope:          audit.Scope,
		SelectedClaims: audit.SelectedClaims,
	}
}

func validateEvidenceAuditTimestamp(name, value string) error {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be RFC3339: %w", name, err)
	}
	return nil
}

func validateEvidenceAuditTimeline(audit EvidenceAudit) error {
	created, _ := time.Parse(time.RFC3339Nano, audit.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, audit.UpdatedAt)
	if updated.Before(created) {
		return fmt.Errorf("invalid timestamp order: updated_at precedes created_at")
	}
	var started time.Time
	if audit.StartedAt != "" {
		started, _ = time.Parse(time.RFC3339Nano, audit.StartedAt)
		if started.Before(created) || updated.Before(started) {
			return fmt.Errorf("invalid timestamp order: started_at must be between created_at and updated_at")
		}
	}
	if audit.CompletedAt != "" {
		completed, _ := time.Parse(time.RFC3339Nano, audit.CompletedAt)
		if started.IsZero() || completed.Before(started) || updated.Before(completed) {
			return fmt.Errorf("invalid timestamp order: completed_at must be between started_at and updated_at")
		}
	}
	if audit.FailedAt != "" {
		failed, _ := time.Parse(time.RFC3339Nano, audit.FailedAt)
		lowerBound := created
		if !started.IsZero() {
			lowerBound = started
		}
		if failed.Before(lowerBound) || updated.Before(failed) {
			return fmt.Errorf("invalid timestamp order: failed_at must follow lifecycle start and not exceed updated_at")
		}
	}
	return nil
}

func equalEvidenceAuditVerdictCounts(want, got map[string]int) bool {
	if len(want) != len(got) {
		return false
	}
	for verdict, count := range want {
		if got[verdict] != count {
			return false
		}
	}
	return true
}
