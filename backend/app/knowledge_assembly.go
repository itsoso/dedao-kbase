package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	KnowledgeReleaseAssemblySchemaVersion = "knowledge_release_assembly.v1"
	KnowledgeAssemblyAlgorithmVersion     = "deterministic-claim-assembly.v1"

	KnowledgeAssemblyPolarityPositive = "positive"
	KnowledgeAssemblyPolarityNegative = "negative"

	KnowledgeAssemblyStatusPotentialConflict    = "potential_conflict"
	KnowledgeAssemblyStatusCorroborated         = "corroborated"
	KnowledgeAssemblyStatusSinglePublication    = "single_publication"
	KnowledgeAssemblyStatusInsufficientIdentity = "insufficient_identity"

	knowledgeAssemblyDefaultLimit  = 100
	knowledgeAssemblyMaxLimit      = 500
	knowledgeAssemblyMaxQueryRunes = 256
)

type KnowledgeReleaseAssemblyQuery struct {
	Limit int
	Query string
}

type KnowledgeReleaseAssembly struct {
	SchemaVersion    string                            `json:"schema_version"`
	AlgorithmVersion string                            `json:"algorithm_version"`
	AssemblyID       string                            `json:"assembly_id"`
	ReleaseIDs       []string                          `json:"release_ids"`
	Summary          KnowledgeReleaseAssemblySummary   `json:"summary"`
	Clusters         []KnowledgeReleaseAssemblyCluster `json:"clusters"`
	ReturnedClusters int                               `json:"returned_clusters"`
	HasMore          bool                              `json:"has_more"`
}

type KnowledgeReleaseAssemblySummary struct {
	ReleaseCount              int `json:"release_count"`
	ClaimCount                int `json:"claim_count"`
	ClusterCount              int `json:"cluster_count"`
	MatchedClusterCount       int `json:"matched_cluster_count"`
	CorroboratedClusters      int `json:"corroborated_clusters"`
	PotentialConflictClusters int `json:"potential_conflict_clusters"`
	SinglePublicationClusters int `json:"single_publication_clusters"`
	InsufficientIdentity      int `json:"insufficient_identity_clusters"`
}

type KnowledgeReleaseAssemblyCluster struct {
	ClusterID                   string                                      `json:"cluster_id"`
	NormalizedAssertion         string                                      `json:"normalized_assertion"`
	Status                      string                                      `json:"status"`
	PublicationCount            int                                         `json:"publication_count"`
	IndependentPublicationCount int                                         `json:"independent_publication_count"`
	Claims                      []KnowledgeReleaseAssemblyClaimRef          `json:"claims"`
	PotentialConflicts          []KnowledgeReleaseAssemblyPotentialConflict `json:"potential_conflicts,omitempty"`
}

type KnowledgeReleaseAssemblyClaimRef struct {
	ReleaseID                      string   `json:"release_id"`
	BookID                         string   `json:"book_id"`
	ClaimID                        string   `json:"claim_id"`
	Statement                      string   `json:"statement"`
	Polarity                       string   `json:"polarity"`
	CitationIDs                    []string `json:"citation_ids"`
	PublicationIdentity            string   `json:"publication_identity"`
	PublicationIdentityBasis       string   `json:"publication_identity_basis"`
	IndependentPublicationEligible bool     `json:"independent_publication_eligible"`
}

type KnowledgeReleaseAssemblyPotentialConflict struct {
	ConflictID        string `json:"conflict_id"`
	PositiveReleaseID string `json:"positive_release_id"`
	PositiveClaimID   string `json:"positive_claim_id"`
	NegativeReleaseID string `json:"negative_release_id"`
	NegativeClaimID   string `json:"negative_claim_id"`
	ReviewRequired    bool   `json:"review_required"`
}

func BuildKnowledgeReleaseAssembly(
	store *BookKnowledgeStore,
	query KnowledgeReleaseAssemblyQuery,
) (*KnowledgeReleaseAssembly, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if query.Limit <= 0 {
		query.Limit = knowledgeAssemblyDefaultLimit
	}
	if query.Limit > knowledgeAssemblyMaxLimit {
		return nil, fmt.Errorf("limit must be between 1 and %d", knowledgeAssemblyMaxLimit)
	}
	query.Query = strings.TrimSpace(query.Query)
	if utf8.RuneCountInString(query.Query) > knowledgeAssemblyMaxQueryRunes {
		return nil, fmt.Errorf("query must not exceed %d characters", knowledgeAssemblyMaxQueryRunes)
	}

	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return nil, err
	}
	records, err := latestKnowledgeAssemblyReleaseRecords(manifest.Releases)
	if err != nil {
		return nil, err
	}
	releases := make([]KnowledgeRelease, 0, len(records))
	releaseIDs := make([]string, 0, len(records))
	for _, record := range records {
		release, err := store.LoadKnowledgeRelease(record.ReleaseID)
		if err != nil {
			return nil, fmt.Errorf("load selected release %q: %w", boundedEvidenceID(record.ReleaseID), err)
		}
		releasePayload, err := json.Marshal(release)
		if err != nil {
			return nil, fmt.Errorf("encode selected release %q: %w", boundedEvidenceID(record.ReleaseID), err)
		}
		if err := ValidateKnowledgeReleaseContract(releasePayload); err != nil {
			return nil, fmt.Errorf("selected release %q is invalid: %w", boundedEvidenceID(record.ReleaseID), err)
		}
		if release.BookID != record.BookID || release.ContentHash != record.ContentHash ||
			release.CreatedAt != record.CreatedAt {
			return nil, fmt.Errorf("selected release %q does not match its manifest record", boundedEvidenceID(record.ReleaseID))
		}
		releases = append(releases, *release)
		releaseIDs = append(releaseIDs, release.ReleaseID)
	}
	sort.Strings(releaseIDs)
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].ReleaseID < releases[j].ReleaseID
	})

	clusterMap := make(map[string]*KnowledgeReleaseAssemblyCluster)
	claimCount := 0
	for _, release := range releases {
		publication := CanonicalKnowledgePublicationIdentity(release.Book)
		for _, claim := range release.Analysis.Claims {
			normalized := normalizeKnowledgeAssemblyClaim(claim.Statement)
			if strings.TrimSpace(claim.ID) == "" || normalized == "" || len(claim.CitationIDs) == 0 {
				return nil, fmt.Errorf(
					"selected release %q contains an incomplete analysis claim",
					boundedEvidenceID(release.ReleaseID),
				)
			}
			base, polarity := splitKnowledgeAssemblyClaimPolarity(normalized)
			if base == "" {
				return nil, fmt.Errorf(
					"selected release %q contains a claim without a stable assertion",
					boundedEvidenceID(release.ReleaseID),
				)
			}
			clusterID := knowledgeAssemblyHashID("cluster", base)
			cluster := clusterMap[clusterID]
			if cluster == nil {
				cluster = &KnowledgeReleaseAssemblyCluster{
					ClusterID:           clusterID,
					NormalizedAssertion: base,
					Claims:              []KnowledgeReleaseAssemblyClaimRef{},
					PotentialConflicts:  []KnowledgeReleaseAssemblyPotentialConflict{},
				}
				clusterMap[clusterID] = cluster
			}
			cluster.Claims = append(cluster.Claims, KnowledgeReleaseAssemblyClaimRef{
				ReleaseID:                      release.ReleaseID,
				BookID:                         release.BookID,
				ClaimID:                        claim.ID,
				Statement:                      strings.TrimSpace(claim.Statement),
				Polarity:                       polarity,
				CitationIDs:                    uniqueSortedStrings(claim.CitationIDs),
				PublicationIdentity:            publication.Key,
				PublicationIdentityBasis:       publication.Basis,
				IndependentPublicationEligible: publication.IndependentSourceEligible,
			})
			claimCount++
		}
	}

	allClusters := make([]KnowledgeReleaseAssemblyCluster, 0, len(clusterMap))
	summary := KnowledgeReleaseAssemblySummary{
		ReleaseCount: len(releases),
		ClaimCount:   claimCount,
		ClusterCount: len(clusterMap),
	}
	for _, cluster := range clusterMap {
		finalizeKnowledgeAssemblyCluster(cluster)
		switch cluster.Status {
		case KnowledgeAssemblyStatusPotentialConflict:
			summary.PotentialConflictClusters++
		case KnowledgeAssemblyStatusCorroborated:
			summary.CorroboratedClusters++
		case KnowledgeAssemblyStatusSinglePublication:
			summary.SinglePublicationClusters++
		default:
			summary.InsufficientIdentity++
		}
		allClusters = append(allClusters, *cluster)
	}
	sort.Slice(allClusters, func(i, j int) bool {
		if allClusters[i].NormalizedAssertion != allClusters[j].NormalizedAssertion {
			return allClusters[i].NormalizedAssertion < allClusters[j].NormalizedAssertion
		}
		return allClusters[i].ClusterID < allClusters[j].ClusterID
	})

	assemblyID, err := knowledgeReleaseAssemblyID(releaseIDs, allClusters)
	if err != nil {
		return nil, err
	}
	filtered := filterKnowledgeAssemblyClusters(allClusters, query.Query)
	summary.MatchedClusterCount = len(filtered)
	hasMore := len(filtered) > query.Limit
	if hasMore {
		filtered = filtered[:query.Limit]
	}
	result := &KnowledgeReleaseAssembly{
		SchemaVersion:    KnowledgeReleaseAssemblySchemaVersion,
		AlgorithmVersion: KnowledgeAssemblyAlgorithmVersion,
		AssemblyID:       assemblyID,
		ReleaseIDs:       releaseIDs,
		Summary:          summary,
		Clusters:         append([]KnowledgeReleaseAssemblyCluster(nil), filtered...),
		ReturnedClusters: len(filtered),
		HasMore:          hasMore,
	}
	if err := ValidateKnowledgeReleaseAssembly(*result); err != nil {
		return nil, err
	}
	return result, nil
}

func ValidateKnowledgeReleaseAssembly(assembly KnowledgeReleaseAssembly) error {
	if assembly.SchemaVersion != KnowledgeReleaseAssemblySchemaVersion {
		return fmt.Errorf("schema_version must be %q", KnowledgeReleaseAssemblySchemaVersion)
	}
	if assembly.AlgorithmVersion != KnowledgeAssemblyAlgorithmVersion {
		return fmt.Errorf("algorithm_version must be %q", KnowledgeAssemblyAlgorithmVersion)
	}
	if strings.TrimSpace(assembly.AssemblyID) == "" {
		return fmt.Errorf("assembly_id is required")
	}
	if assembly.ReturnedClusters != len(assembly.Clusters) ||
		assembly.Summary.ReleaseCount != len(assembly.ReleaseIDs) ||
		assembly.Summary.ClusterCount < len(assembly.Clusters) ||
		assembly.Summary.MatchedClusterCount < len(assembly.Clusters) {
		return fmt.Errorf("assembly summary is inconsistent")
	}
	seenReleases := make(map[string]struct{}, len(assembly.ReleaseIDs))
	for _, releaseID := range assembly.ReleaseIDs {
		if strings.TrimSpace(releaseID) == "" {
			return fmt.Errorf("release_ids contains an empty value")
		}
		if _, duplicate := seenReleases[releaseID]; duplicate {
			return fmt.Errorf("release_ids contains duplicate %q", boundedEvidenceID(releaseID))
		}
		seenReleases[releaseID] = struct{}{}
	}
	seenClusters := make(map[string]struct{}, len(assembly.Clusters))
	for index, cluster := range assembly.Clusters {
		if err := requireContractFields(map[string]string{
			"cluster_id":           cluster.ClusterID,
			"normalized_assertion": cluster.NormalizedAssertion,
			"status":               cluster.Status,
		}); err != nil {
			return fmt.Errorf("clusters[%d]: %w", index, err)
		}
		if _, duplicate := seenClusters[cluster.ClusterID]; duplicate {
			return fmt.Errorf("clusters contains duplicate cluster_id %q", boundedEvidenceID(cluster.ClusterID))
		}
		seenClusters[cluster.ClusterID] = struct{}{}
		switch cluster.Status {
		case KnowledgeAssemblyStatusPotentialConflict,
			KnowledgeAssemblyStatusCorroborated,
			KnowledgeAssemblyStatusSinglePublication,
			KnowledgeAssemblyStatusInsufficientIdentity:
		default:
			return fmt.Errorf("clusters[%d].status is invalid", index)
		}
		if len(cluster.Claims) == 0 {
			return fmt.Errorf("clusters[%d].claims is required", index)
		}
		for claimIndex, claim := range cluster.Claims {
			if err := requireContractFields(map[string]string{
				"release_id":           claim.ReleaseID,
				"book_id":              claim.BookID,
				"claim_id":             claim.ClaimID,
				"statement":            claim.Statement,
				"polarity":             claim.Polarity,
				"publication_identity": claim.PublicationIdentity,
			}); err != nil {
				return fmt.Errorf("clusters[%d].claims[%d]: %w", index, claimIndex, err)
			}
			if claim.Polarity != KnowledgeAssemblyPolarityPositive &&
				claim.Polarity != KnowledgeAssemblyPolarityNegative {
				return fmt.Errorf("clusters[%d].claims[%d].polarity is invalid", index, claimIndex)
			}
			if len(claim.CitationIDs) == 0 {
				return fmt.Errorf("clusters[%d].claims[%d].citation_ids is required", index, claimIndex)
			}
		}
	}
	return nil
}

func ValidateKnowledgeReleaseAssemblyContract(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, required := range []string{
		"schema_version",
		"algorithm_version",
		"assembly_id",
		"release_ids",
		"summary",
		"clusters",
		"returned_clusters",
		"has_more",
	} {
		if value, exists := fields[required]; !exists || len(value) == 0 || string(value) == "null" {
			return fmt.Errorf("%s is required", required)
		}
	}
	var assembly KnowledgeReleaseAssembly
	if err := json.Unmarshal(raw, &assembly); err != nil {
		return err
	}
	return ValidateKnowledgeReleaseAssembly(assembly)
}

func latestKnowledgeAssemblyReleaseRecords(
	records []KnowledgeReleaseRecord,
) ([]KnowledgeReleaseRecord, error) {
	type timedReleaseRecord struct {
		record    KnowledgeReleaseRecord
		createdAt time.Time
	}
	latest := make(map[string]timedReleaseRecord)
	for _, record := range records {
		if err := requireContractFields(map[string]string{
			"release_id": record.ReleaseID,
			"book_id":    record.BookID,
			"created_at": record.CreatedAt,
		}); err != nil {
			return nil, fmt.Errorf("release manifest record: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("release manifest record %q has invalid created_at", boundedEvidenceID(record.ReleaseID))
		}
		current, exists := latest[record.BookID]
		if !exists || createdAt.After(current.createdAt) ||
			(createdAt.Equal(current.createdAt) && record.ReleaseID > current.record.ReleaseID) {
			latest[record.BookID] = timedReleaseRecord{
				record:    record,
				createdAt: createdAt,
			}
		}
	}
	result := make([]KnowledgeReleaseRecord, 0, len(latest))
	for _, timedRecord := range latest {
		result = append(result, timedRecord.record)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ReleaseID < result[j].ReleaseID
	})
	return result, nil
}

func finalizeKnowledgeAssemblyCluster(cluster *KnowledgeReleaseAssemblyCluster) {
	sort.Slice(cluster.Claims, func(i, j int) bool {
		if cluster.Claims[i].ReleaseID != cluster.Claims[j].ReleaseID {
			return cluster.Claims[i].ReleaseID < cluster.Claims[j].ReleaseID
		}
		return cluster.Claims[i].ClaimID < cluster.Claims[j].ClaimID
	})
	publications := make(map[string]struct{})
	independent := make(map[string]struct{})
	positive := make([]KnowledgeReleaseAssemblyClaimRef, 0)
	negative := make([]KnowledgeReleaseAssemblyClaimRef, 0)
	for _, claim := range cluster.Claims {
		publications[claim.PublicationIdentity] = struct{}{}
		if claim.IndependentPublicationEligible {
			independent[claim.PublicationIdentity] = struct{}{}
		}
		if claim.Polarity == KnowledgeAssemblyPolarityNegative {
			negative = append(negative, claim)
		} else {
			positive = append(positive, claim)
		}
	}
	cluster.PublicationCount = len(publications)
	cluster.IndependentPublicationCount = len(independent)
	for _, positiveClaim := range positive {
		for _, negativeClaim := range negative {
			seed := strings.Join([]string{
				cluster.ClusterID,
				positiveClaim.ReleaseID,
				positiveClaim.ClaimID,
				negativeClaim.ReleaseID,
				negativeClaim.ClaimID,
			}, "\x00")
			cluster.PotentialConflicts = append(
				cluster.PotentialConflicts,
				KnowledgeReleaseAssemblyPotentialConflict{
					ConflictID:        knowledgeAssemblyHashID("conflict", seed),
					PositiveReleaseID: positiveClaim.ReleaseID,
					PositiveClaimID:   positiveClaim.ClaimID,
					NegativeReleaseID: negativeClaim.ReleaseID,
					NegativeClaimID:   negativeClaim.ClaimID,
					ReviewRequired:    true,
				},
			)
		}
	}
	switch {
	case len(cluster.PotentialConflicts) > 0:
		cluster.Status = KnowledgeAssemblyStatusPotentialConflict
	case cluster.IndependentPublicationCount >= 2:
		cluster.Status = KnowledgeAssemblyStatusCorroborated
	case cluster.IndependentPublicationCount == 1:
		cluster.Status = KnowledgeAssemblyStatusSinglePublication
	default:
		cluster.Status = KnowledgeAssemblyStatusInsufficientIdentity
	}
}

func normalizeKnowledgeAssemblyClaim(value string) string {
	var builder strings.Builder
	space := true
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
			space = false
			continue
		}
		if !space {
			builder.WriteByte(' ')
			space = true
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func splitKnowledgeAssemblyClaimPolarity(normalized string) (string, string) {
	normalized = normalizeKnowledgeAssemblyClaim(normalized)
	englishMarkers := map[string]struct{}{"not": {}, "no": {}, "never": {}}
	fields := strings.Fields(normalized)
	for index, field := range fields {
		if _, negative := englishMarkers[field]; !negative {
			continue
		}
		fields = append(fields[:index], fields[index+1:]...)
		return strings.Join(fields, " "), KnowledgeAssemblyPolarityNegative
	}
	if index := strings.Index(normalized, "并非"); index >= 0 {
		return normalizeKnowledgeAssemblyClaim(
			normalized[:index] + normalized[index+len("并非"):],
		), KnowledgeAssemblyPolarityNegative
	}
	for _, marker := range []string{"不", "未", "没"} {
		if index := strings.Index(normalized, marker); index >= 0 {
			return normalizeKnowledgeAssemblyClaim(
				normalized[:index] + normalized[index+len(marker):],
			), KnowledgeAssemblyPolarityNegative
		}
	}
	return normalized, KnowledgeAssemblyPolarityPositive
}

func filterKnowledgeAssemblyClusters(
	clusters []KnowledgeReleaseAssemblyCluster,
	query string,
) []KnowledgeReleaseAssemblyCluster {
	normalizedQuery := normalizeKnowledgeAssemblyClaim(query)
	if normalizedQuery == "" {
		return append([]KnowledgeReleaseAssemblyCluster(nil), clusters...)
	}
	result := make([]KnowledgeReleaseAssemblyCluster, 0)
	for _, cluster := range clusters {
		if strings.Contains(cluster.NormalizedAssertion, normalizedQuery) {
			result = append(result, cluster)
			continue
		}
		for _, claim := range cluster.Claims {
			if strings.Contains(normalizeKnowledgeAssemblyClaim(claim.Statement), normalizedQuery) {
				result = append(result, cluster)
				break
			}
		}
	}
	return result
}

func knowledgeReleaseAssemblyID(
	releaseIDs []string,
	clusters []KnowledgeReleaseAssemblyCluster,
) (string, error) {
	seed := struct {
		AlgorithmVersion string                            `json:"algorithm_version"`
		ReleaseIDs       []string                          `json:"release_ids"`
		Clusters         []KnowledgeReleaseAssemblyCluster `json:"clusters"`
	}{
		AlgorithmVersion: KnowledgeAssemblyAlgorithmVersion,
		ReleaseIDs:       releaseIDs,
		Clusters:         clusters,
	}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "assembly-" + hex.EncodeToString(sum[:]), nil
}

func knowledgeAssemblyHashID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "-" + hex.EncodeToString(sum[:12])
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
