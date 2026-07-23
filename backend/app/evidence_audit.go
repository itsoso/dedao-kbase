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
	FailureReason  string                           `json:"failure_reason,omitempty"`
}

type EvidenceAuditInput struct {
	SchemaVersion  string                         `json:"schema_version"`
	InputHash      string                         `json:"input_hash,omitempty"`
	Package        EvidenceAuditPackageRef        `json:"package"`
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
	ReleaseID   string   `json:"release_id"`
	ContentHash string   `json:"content_hash"`
	SourceType  string   `json:"source_type"`
	CitationIDs []string `json:"citation_ids,omitempty"`
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
	ReleaseID   string `json:"release_id"`
	ContentHash string `json:"content_hash"`
	SourceType  string `json:"source_type"`
	ClaimID     string `json:"claim_id,omitempty"`
	ChunkID     string `json:"chunk_id,omitempty"`
	CitationID  string `json:"citation_id,omitempty"`
	Conflict    bool   `json:"conflict,omitempty"`
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
	switch audit.Status {
	case EvidenceAuditQueued:
		if evidenceAuditHasPartialReport(audit) {
			return fmt.Errorf("queued status cannot contain a partial report")
		}
		if audit.StartedAt != "" || audit.CompletedAt != "" || audit.FailedAt != "" ||
			audit.TraceID != "" || audit.FailureReason != "" {
			return fmt.Errorf("queued status cannot contain terminal metadata")
		}
	case EvidenceAuditRunning:
		if evidenceAuditHasPartialReport(audit) {
			return fmt.Errorf("running status cannot contain a partial report")
		}
		if audit.StartedAt == "" || audit.TraceID == "" {
			return fmt.Errorf("running status requires started_at and trace_id")
		}
		if audit.CompletedAt != "" || audit.FailedAt != "" || audit.FailureReason != "" {
			return fmt.Errorf("running status cannot contain terminal metadata")
		}
	case EvidenceAuditFailed:
		if evidenceAuditHasPartialReport(audit) {
			return fmt.Errorf("failed status cannot contain a partial report")
		}
		if audit.FailedAt == "" || strings.TrimSpace(audit.FailureReason) == "" {
			return fmt.Errorf("failed status requires failed_at and failure_reason")
		}
		if audit.CompletedAt != "" {
			return fmt.Errorf("failed status cannot contain completed_at")
		}
	case EvidenceAuditCompleted:
		if audit.StartedAt == "" || audit.CompletedAt == "" || audit.TraceID == "" {
			return fmt.Errorf("completed status requires started_at, completed_at, and trace_id")
		}
		if audit.FailedAt != "" || audit.FailureReason != "" {
			return fmt.Errorf("completed status cannot contain failure metadata")
		}
		if audit.OutputHash == "" {
			return fmt.Errorf("completed status requires output_hash")
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
	return nil
}

func ComputeEvidenceAuditConfidence(evidence []EvidenceAuditEvidenceRef, conflicts int) float64 {
	if len(evidence) == 0 {
		return 0
	}
	resolvable := 0
	sourceTypes := map[string]struct{}{}
	for _, ref := range evidence {
		if evidenceAuditRefResolvable(ref) {
			resolvable++
			sourceTypes[strings.TrimSpace(ref.SourceType)] = struct{}{}
		}
	}
	completeness := float64(resolvable) / float64(len(evidence))
	independence := math.Min(float64(len(sourceTypes))/2, 1)
	conflictRatio := math.Min(math.Max(float64(conflicts), 0)/float64(len(evidence)), 1)
	score := completeness * independence * (1 - 0.5*conflictRatio)
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
	if len(input.Releases) == 0 {
		return fmt.Errorf("releases must pin at least one immutable release")
	}
	seenReleases := map[string]struct{}{}
	for index, release := range input.Releases {
		if err := requireContractFields(map[string]string{
			fmt.Sprintf("releases[%d].release_id", index):   release.ReleaseID,
			fmt.Sprintf("releases[%d].content_hash", index): release.ContentHash,
			fmt.Sprintf("releases[%d].source_type", index):  release.SourceType,
		}); err != nil {
			return err
		}
		releaseID := strings.TrimSpace(release.ReleaseID)
		if _, duplicate := seenReleases[releaseID]; duplicate {
			return fmt.Errorf("releases contains duplicate release_id %q", releaseID)
		}
		seenReleases[releaseID] = struct{}{}
	}
	if len(input.SelectedClaims) == 0 || len(input.SelectedClaims) > agentEvidenceMaxClaims {
		return fmt.Errorf("selected_claims must contain between 1 and %d claims", agentEvidenceMaxClaims)
	}
	return nil
}

func validateCompletedEvidenceAudit(audit EvidenceAudit) error {
	if len(audit.ClaimAudits) == 0 || len(audit.ClaimAudits) > agentEvidenceMaxClaims {
		return fmt.Errorf("claim_audits must contain between 1 and %d claims", agentEvidenceMaxClaims)
	}
	pinnedReleases := make(map[string]EvidenceAuditReleaseRef, len(audit.Releases))
	for _, release := range audit.Releases {
		pinnedReleases[release.ReleaseID] = release
	}
	actualCounts := map[string]int{}
	for index, claim := range audit.ClaimAudits {
		if err := requireContractFields(map[string]string{
			fmt.Sprintf("claim_audits[%d].source_claim", index):         claim.SourceClaim,
			fmt.Sprintf("claim_audits[%d].normalized_statement", index): claim.NormalizedStatement,
			fmt.Sprintf("claim_audits[%d].verdict", index):              claim.Verdict,
		}); err != nil {
			return err
		}
		switch claim.Verdict {
		case EvidenceAuditVerdictSupported, EvidenceAuditVerdictContradicted, EvidenceAuditVerdictMixed:
			if len(claim.Evidence) == 0 {
				return fmt.Errorf("claim_audits[%d] verdict %q requires evidence", index, claim.Verdict)
			}
		case EvidenceAuditVerdictInsufficient:
		default:
			return fmt.Errorf("claim_audits[%d] has unsupported verdict %q", index, claim.Verdict)
		}
		if len(claim.Evidence) > agentEvidenceMaxEvidencePerClaim {
			return fmt.Errorf("claim_audits[%d] exceeds evidence limit %d", index, agentEvidenceMaxEvidencePerClaim)
		}
		conflicts := 0
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
			if strings.TrimSpace(ref.SourceType) != release.SourceType {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] source_type does not match pinned release",
					index,
					evidenceIndex,
				)
			}
			if !evidenceAuditReleaseAllowsCitation(release, ref.CitationID) {
				return fmt.Errorf(
					"claim_audits[%d].evidence_refs[%d] must reference a pinned citation from release %q",
					index,
					evidenceIndex,
					release.ReleaseID,
				)
			}
			if ref.Conflict {
				conflicts++
			}
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
	if !equalEvidenceAuditVerdictCounts(actualCounts, audit.Summary.VerdictCounts) {
		return fmt.Errorf("summary.verdict_counts does not match claim audits")
	}
	if strings.TrimSpace(audit.Proofroom.SchemaVersion) == "" ||
		strings.TrimSpace(audit.Proofroom.Title) == "" ||
		len(audit.Proofroom.ReviewItems) == 0 {
		return fmt.Errorf("proofroom_projection must be complete")
	}
	return nil
}

func evidenceAuditRefResolvable(ref EvidenceAuditEvidenceRef) bool {
	if strings.TrimSpace(ref.ReleaseID) == "" ||
		strings.TrimSpace(ref.ContentHash) == "" ||
		strings.TrimSpace(ref.SourceType) == "" {
		return false
	}
	return strings.TrimSpace(ref.CitationID) != ""
}

func evidenceAuditReleaseAllowsCitation(release EvidenceAuditReleaseRef, citationID string) bool {
	citationID = strings.TrimSpace(citationID)
	for _, allowed := range release.CitationIDs {
		if strings.TrimSpace(allowed) == citationID {
			return true
		}
	}
	return false
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
		input.Releases[index].CitationIDs = append([]string(nil), input.Releases[index].CitationIDs...)
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
		input.Releases[index].SourceType = strings.TrimSpace(input.Releases[index].SourceType)
		input.Releases[index].CitationIDs = normalizeEvidenceAuditStrings(input.Releases[index].CitationIDs, true)
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
