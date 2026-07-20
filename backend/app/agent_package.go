package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var agentPackageIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const (
	AgentPackageSchemaVersion = "agent-package.v1"
	AgentPackageDraft         = "draft"
	AgentPackagePublished     = "published"
	AgentPackageSuperseded    = "superseded"

	AgentToolAllow               = "allow"
	AgentToolRequireConfirmation = "require_confirmation"
	AgentToolBlock               = "block"
)

type AgentPackage struct {
	SchemaVersion    string                       `json:"schema_version"`
	PackageID        string                       `json:"package_id"`
	Version          string                       `json:"version"`
	ContentHash      string                       `json:"content_hash"`
	LifecycleState   string                       `json:"lifecycle_state"`
	Supersedes       string                       `json:"supersedes,omitempty"`
	Releases         []AgentPackageReleaseRef     `json:"releases"`
	RetrievalPolicy  AgentPackageRetrievalPolicy  `json:"retrieval_policy"`
	ModelPolicy      AgentPackageModelPolicy      `json:"model_policy"`
	PromptProfiles   []AgentPackagePromptProfile  `json:"prompt_profiles"`
	ToolPolicy       AgentPackageToolPolicy       `json:"tool_policy"`
	SafetyPolicy     AgentPackageSafetyPolicy     `json:"safety_policy"`
	EvaluationPolicy AgentPackageEvaluationPolicy `json:"evaluation_policy"`
	UIManifest       AgentPackageUIManifest       `json:"ui_manifest"`
	CreatedAt        string                       `json:"created_at,omitempty"`
	PublishedAt      string                       `json:"published_at,omitempty"`
}

type AgentPackageReleaseRef struct {
	ReleaseID   string   `json:"release_id"`
	ContentHash string   `json:"content_hash"`
	CitationIDs []string `json:"citation_ids"`
}

type AgentPackageRetrievalPolicy struct {
	Strategy              string   `json:"strategy"`
	AllowedSourceTypes    []string `json:"allowed_source_types"`
	RequireCitations      bool     `json:"require_citations"`
	MaxContextChunks      int      `json:"max_context_chunks"`
	EmbeddingProvider     string   `json:"embedding_provider,omitempty"`
	EmbeddingModel        string   `json:"embedding_model,omitempty"`
	EmbeddingVersion      string   `json:"embedding_version,omitempty"`
	EmbeddingEndpointHash string   `json:"embedding_endpoint_hash,omitempty"`
	RerankerVersion       string   `json:"reranker_version,omitempty"`
}

type AgentPackageModelPolicy struct {
	PreferredCapability string   `json:"preferred_capability"`
	Fallbacks           []string `json:"fallbacks,omitempty"`
	MaxCostUSD          float64  `json:"max_cost_usd"`
	TimeoutMS           int      `json:"timeout_ms"`
}

type AgentPackagePromptProfile struct {
	ProfileID    string `json:"profile_id"`
	OutputSchema string `json:"output_schema"`
}

type AgentPackageToolPolicy struct {
	Tools []AgentPackageToolRule `json:"tools"`
}

type AgentPackageToolRule struct {
	MCPServer string `json:"mcp_server"`
	ToolName  string `json:"tool_name"`
	Decision  string `json:"decision"`
}

type AgentPackageSafetyPolicy struct {
	UsagePolicy       string   `json:"usage_policy"`
	AbstentionReasons []string `json:"abstention_reasons"`
	EscalationTarget  string   `json:"escalation_target"`
}

type AgentPackageEvaluationPolicy struct {
	SuiteVersion  string             `json:"suite_version"`
	MinimumScores map[string]float64 `json:"minimum_scores"`
}

type AgentPackageUIManifest struct {
	Capabilities []string `json:"capabilities"`
}

func FinalizeAgentPackage(pkg AgentPackage) (AgentPackage, error) {
	hash, err := AgentPackageContentHash(pkg)
	if err != nil {
		return AgentPackage{}, err
	}
	pkg.ContentHash = hash
	return pkg, nil
}

func AgentPackageContentHash(pkg AgentPackage) (string, error) {
	normalized, err := normalizeAgentPackageForHash(pkg)
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

func ValidateAgentPackage(pkg AgentPackage, store *BookKnowledgeStore, knownTools []string) error {
	if pkg.SchemaVersion != AgentPackageSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentPackageSchemaVersion)
	}
	if err := requireContractFields(map[string]string{
		"package_id":                        pkg.PackageID,
		"version":                           pkg.Version,
		"content_hash":                      pkg.ContentHash,
		"lifecycle_state":                   pkg.LifecycleState,
		"retrieval_policy.strategy":         pkg.RetrievalPolicy.Strategy,
		"model_policy.preferred_capability": pkg.ModelPolicy.PreferredCapability,
		"safety_policy.usage_policy":        pkg.SafetyPolicy.UsagePolicy,
		"safety_policy.escalation_target":   pkg.SafetyPolicy.EscalationTarget,
		"evaluation_policy.suite_version":   pkg.EvaluationPolicy.SuiteVersion,
	}); err != nil {
		return err
	}
	if !agentPackageIDPattern.MatchString(pkg.PackageID) {
		return fmt.Errorf("package_id must contain only URL-safe letters, digits, dot, underscore, or hyphen")
	}
	if err := validateAgentPackageState(pkg); err != nil {
		return err
	}
	wantHash, err := AgentPackageContentHash(pkg)
	if err != nil {
		return err
	}
	if pkg.ContentHash != wantHash {
		return fmt.Errorf("content_hash does not match deterministic package content")
	}
	if len(pkg.Releases) == 0 {
		return fmt.Errorf("releases must pin at least one published release")
	}
	if err := validateAgentPackageRetrieval(pkg.RetrievalPolicy); err != nil {
		return err
	}
	if err := validateAgentPackageModel(pkg.ModelPolicy); err != nil {
		return err
	}
	if err := validateAgentPackagePrompts(pkg.PromptProfiles); err != nil {
		return err
	}
	if err := validateAgentPackageTools(pkg.ToolPolicy, knownTools); err != nil {
		return err
	}
	if err := validateAgentPackageSafety(pkg.SafetyPolicy); err != nil {
		return err
	}
	if err := validateAgentPackageEvaluation(pkg.EvaluationPolicy); err != nil {
		return err
	}
	if err := validateAgentPackageUI(pkg.UIManifest); err != nil {
		return err
	}
	return validateAgentPackageReleases(pkg, store)
}

func validateAgentPackageState(pkg AgentPackage) error {
	switch pkg.LifecycleState {
	case AgentPackageDraft, AgentPackagePublished, AgentPackageSuperseded:
	default:
		return fmt.Errorf("unsupported lifecycle_state %q", pkg.LifecycleState)
	}
	return nil
}

func validateAgentPackageRetrieval(policy AgentPackageRetrievalPolicy) error {
	switch policy.Strategy {
	case "lexical", "vector", "hybrid", "graph":
	default:
		return fmt.Errorf("unsupported retrieval_policy.strategy %q", policy.Strategy)
	}
	if len(uniqueTrimmedStrings(policy.AllowedSourceTypes)) == 0 {
		return fmt.Errorf("retrieval_policy.allowed_source_types is required")
	}
	if !policy.RequireCitations {
		return fmt.Errorf("retrieval_policy.require_citations must be true")
	}
	if policy.MaxContextChunks <= 0 {
		return fmt.Errorf("retrieval_policy.max_context_chunks must be positive")
	}
	if policy.Strategy == "vector" || policy.Strategy == "hybrid" {
		if err := requireContractFields(map[string]string{
			"retrieval_policy.embedding_provider":      policy.EmbeddingProvider,
			"retrieval_policy.embedding_model":         policy.EmbeddingModel,
			"retrieval_policy.embedding_version":       policy.EmbeddingVersion,
			"retrieval_policy.embedding_endpoint_hash": policy.EmbeddingEndpointHash,
			"retrieval_policy.reranker_version":        policy.RerankerVersion,
		}); err != nil {
			return err
		}
		if policy.RerankerVersion != AgentSemanticRerankerVersion {
			return fmt.Errorf("retrieval_policy.reranker_version %q is not supported", policy.RerankerVersion)
		}
		if err := validateAgentSHA256("retrieval_policy.embedding_endpoint_hash", policy.EmbeddingEndpointHash); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentPackageModel(policy AgentPackageModelPolicy) error {
	if strings.TrimSpace(policy.PreferredCapability) == "" {
		return fmt.Errorf("model_policy.preferred_capability is required")
	}
	if policy.MaxCostUSD < 0 {
		return fmt.Errorf("model_policy.max_cost_usd must not be negative")
	}
	if policy.TimeoutMS <= 0 {
		return fmt.Errorf("model_policy.timeout_ms must be positive")
	}
	return nil
}

func validateAgentPackagePrompts(profiles []AgentPackagePromptProfile) error {
	if len(profiles) == 0 {
		return fmt.Errorf("prompt_profiles is required")
	}
	seen := make(map[string]struct{}, len(profiles))
	for index, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) == "" || strings.TrimSpace(profile.OutputSchema) == "" {
			return fmt.Errorf("prompt_profiles[%d] requires profile_id and output_schema", index)
		}
		if _, ok := seen[profile.ProfileID]; ok {
			return fmt.Errorf("duplicate prompt profile %q", profile.ProfileID)
		}
		seen[profile.ProfileID] = struct{}{}
	}
	return nil
}

func validateAgentPackageTools(policy AgentPackageToolPolicy, knownTools []string) error {
	known := make(map[string]struct{}, len(knownTools))
	for _, tool := range knownTools {
		known[strings.TrimSpace(tool)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(policy.Tools))
	for index, rule := range policy.Tools {
		key := strings.TrimSpace(rule.MCPServer) + "/" + strings.TrimSpace(rule.ToolName)
		if key == "/" {
			return fmt.Errorf("tool_policy.tools[%d] requires mcp_server and tool_name", index)
		}
		if _, ok := known[key]; !ok {
			return fmt.Errorf("unknown tool %q", key)
		}
		switch rule.Decision {
		case AgentToolAllow, AgentToolRequireConfirmation, AgentToolBlock:
		default:
			return fmt.Errorf("tool %q has unsupported decision %q", key, rule.Decision)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate tool rule %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateAgentPackageSafety(policy AgentPackageSafetyPolicy) error {
	switch policy.UsagePolicy {
	case BookUsageStandard, BookUsageEvidenceOnly:
	default:
		return fmt.Errorf("unsupported safety_policy.usage_policy %q", policy.UsagePolicy)
	}
	if len(uniqueTrimmedStrings(policy.AbstentionReasons)) == 0 {
		return fmt.Errorf("safety_policy.abstention_reasons is required")
	}
	if strings.TrimSpace(policy.EscalationTarget) == "" {
		return fmt.Errorf("safety_policy.escalation_target is required")
	}
	return nil
}

func validateAgentPackageEvaluation(policy AgentPackageEvaluationPolicy) error {
	if strings.TrimSpace(policy.SuiteVersion) == "" {
		return fmt.Errorf("evaluation_policy.suite_version is required")
	}
	if len(policy.MinimumScores) == 0 {
		return fmt.Errorf("evaluation_policy.minimum_scores is required")
	}
	for _, metric := range []string{
		"retrieval", "retrieval_precision", "citations", "faithfulness", "abstention",
		"tool_choice", "tool_arguments", "task_completion", "latency", "cost",
	} {
		if _, ok := policy.MinimumScores[metric]; !ok {
			return fmt.Errorf("required evaluation metric %q is missing", metric)
		}
	}
	for metric, threshold := range policy.MinimumScores {
		if strings.TrimSpace(metric) == "" {
			return fmt.Errorf("evaluation_policy.minimum_scores contains an empty metric")
		}
		if threshold < 0 || threshold > 1 {
			return fmt.Errorf("evaluation threshold %q must be between 0 and 1", metric)
		}
		if threshold == 0 {
			return fmt.Errorf("evaluation threshold %q must be greater than zero", metric)
		}
	}
	return nil
}

func validateAgentPackageUI(manifest AgentPackageUIManifest) error {
	allowed := map[string]struct{}{
		"reader": {}, "search": {}, "grounded_chat": {}, "evidence": {},
		"quiz": {}, "action_plan": {},
	}
	if len(manifest.Capabilities) == 0 {
		return fmt.Errorf("ui_manifest.capabilities is required")
	}
	for _, capability := range manifest.Capabilities {
		if _, ok := allowed[strings.TrimSpace(capability)]; !ok {
			return fmt.Errorf("unknown ui capability %q", capability)
		}
	}
	return nil
}

func validateAgentPackageReleases(pkg AgentPackage, store *BookKnowledgeStore) error {
	if store == nil {
		return fmt.Errorf("published release store is required")
	}
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return err
	}
	published := make(map[string]KnowledgeReleaseRecord, len(manifest.Releases))
	for _, record := range manifest.Releases {
		published[record.ReleaseID] = record
	}
	allowedSources := stringSet(pkg.RetrievalPolicy.AllowedSourceTypes)
	seen := make(map[string]struct{}, len(pkg.Releases))
	citationReleases := make(map[string]string)
	for index, ref := range pkg.Releases {
		if strings.TrimSpace(ref.ReleaseID) == "" {
			return fmt.Errorf("releases[%d].release_id is required", index)
		}
		if strings.TrimSpace(ref.ContentHash) == "" {
			return fmt.Errorf("releases[%d].content_hash is required for an immutable reference", index)
		}
		if _, ok := seen[ref.ReleaseID]; ok {
			return fmt.Errorf("duplicate release reference %q", ref.ReleaseID)
		}
		seen[ref.ReleaseID] = struct{}{}
		record, ok := published[ref.ReleaseID]
		if !ok {
			return fmt.Errorf("published release %q was not found", ref.ReleaseID)
		}
		if record.ContentHash != ref.ContentHash {
			return fmt.Errorf("release %q content hash does not match pinned content hash", ref.ReleaseID)
		}
		release, err := store.LoadKnowledgeRelease(ref.ReleaseID)
		if err != nil {
			return fmt.Errorf("load published release %q: %w", ref.ReleaseID, err)
		}
		sourceType := strings.TrimSpace(release.Book.SourceType)
		if sourceType == "" {
			return fmt.Errorf("release %q source type is required", ref.ReleaseID)
		}
		if !allowedSources[sourceType] {
			return fmt.Errorf("release %q source type %q is outside retrieval policy", ref.ReleaseID, sourceType)
		}
		switch release.UsagePolicy {
		case BookUsageStandard:
		case BookUsageEvidenceOnly:
			if pkg.SafetyPolicy.UsagePolicy != BookUsageEvidenceOnly {
				return fmt.Errorf("release %q usage policy %q cannot be downgraded to package policy %q", ref.ReleaseID, release.UsagePolicy, pkg.SafetyPolicy.UsagePolicy)
			}
		default:
			return fmt.Errorf("release %q usage policy %q is unsupported", ref.ReleaseID, release.UsagePolicy)
		}
		if pkg.RetrievalPolicy.RequireCitations && len(ref.CitationIDs) == 0 {
			return fmt.Errorf("release %q citation_ids is required", ref.ReleaseID)
		}
		availableCitations := make(map[string]struct{}, len(release.Citations))
		for _, citation := range release.Citations {
			if _, exists := availableCitations[citation.CitationID]; exists {
				return fmt.Errorf("release %q contains duplicate citation %q", ref.ReleaseID, citation.CitationID)
			}
			availableCitations[citation.CitationID] = struct{}{}
		}
		for _, citationID := range uniqueTrimmedStrings(ref.CitationIDs) {
			if _, ok := availableCitations[citationID]; !ok {
				return fmt.Errorf("release %q citation %q cannot be resolved", ref.ReleaseID, citationID)
			}
			if previousReleaseID, ok := citationReleases[citationID]; ok && previousReleaseID != ref.ReleaseID {
				return fmt.Errorf("citation %q is ambiguous across multiple releases %q and %q", citationID, previousReleaseID, ref.ReleaseID)
			}
			citationReleases[citationID] = ref.ReleaseID
		}
	}
	return nil
}

func normalizeAgentPackageForHash(pkg AgentPackage) (AgentPackage, error) {
	payload, err := json.Marshal(pkg)
	if err != nil {
		return AgentPackage{}, err
	}
	var normalized AgentPackage
	if err := json.Unmarshal(payload, &normalized); err != nil {
		return AgentPackage{}, err
	}
	normalized.ContentHash = ""
	normalized.LifecycleState = ""
	normalized.Supersedes = ""
	normalized.CreatedAt = ""
	normalized.PublishedAt = ""
	for index := range normalized.Releases {
		normalized.Releases[index].CitationIDs = sortedUniqueStrings(normalized.Releases[index].CitationIDs)
	}
	sort.Slice(normalized.Releases, func(i, j int) bool {
		if normalized.Releases[i].ReleaseID == normalized.Releases[j].ReleaseID {
			return normalized.Releases[i].ContentHash < normalized.Releases[j].ContentHash
		}
		return normalized.Releases[i].ReleaseID < normalized.Releases[j].ReleaseID
	})
	normalized.RetrievalPolicy.AllowedSourceTypes = sortedUniqueStrings(normalized.RetrievalPolicy.AllowedSourceTypes)
	normalized.ModelPolicy.Fallbacks = uniqueTrimmedStrings(normalized.ModelPolicy.Fallbacks)
	sort.Slice(normalized.ToolPolicy.Tools, func(i, j int) bool {
		left := normalized.ToolPolicy.Tools[i].MCPServer + "/" + normalized.ToolPolicy.Tools[i].ToolName
		right := normalized.ToolPolicy.Tools[j].MCPServer + "/" + normalized.ToolPolicy.Tools[j].ToolName
		return left < right
	})
	normalized.SafetyPolicy.AbstentionReasons = sortedUniqueStrings(normalized.SafetyPolicy.AbstentionReasons)
	normalized.UIManifest.Capabilities = sortedUniqueStrings(normalized.UIManifest.Capabilities)
	return normalized, nil
}

func sortedUniqueStrings(values []string) []string {
	result := uniqueTrimmedStrings(values)
	sort.Strings(result)
	return result
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range uniqueTrimmedStrings(values) {
		result[value] = true
	}
	return result
}
