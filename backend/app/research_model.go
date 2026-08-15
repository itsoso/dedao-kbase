package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ResearchRolePlanner     ResearchModelRole = "planner"
	ResearchRoleExtractor   ResearchModelRole = "extractor"
	ResearchRoleSynthesizer ResearchModelRole = "synthesizer"
	ResearchRoleVerifier    ResearchModelRole = "verifier"

	ResearchVerifierVerified     = "verified"
	ResearchVerifierGaps         = "gaps"
	ResearchVerifierInsufficient = "insufficient"

	researchModelArrayMax      = 128
	researchModelToolCallsMax  = 32
	researchDecisionSummaryMax = 2000
	researchModelResponseMax   = 256 << 10
)

type ResearchModelRole string

type ResearchModelUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

type ResearchStageModel interface {
	Run(context.Context, ResearchModelRole, BookTokenPlanConfig, []BookKnowledgeMessage, ResearchModelReferences, any) (ResearchModelUsage, error)
}

type ResearchModelReferences struct {
	EvidenceIDs   []string
	CitationIDs   []string
	ConclusionIDs []string
}

type ResearchPlannedToolCall struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type ResearchPlannerOutput struct {
	DecisionSummary string                    `json:"decision_summary"`
	ToolCalls       []ResearchPlannedToolCall `json:"tool_calls"`
}

type ResearchExtractorOutput struct {
	DecisionSummary string                `json:"decision_summary"`
	Facts           []ResearchFact        `json:"facts"`
	Claims          []ResearchClaim       `json:"claims"`
	Measurements    []ResearchMeasurement `json:"measurements,omitempty"`
	Cases           []ResearchCase        `json:"cases,omitempty"`
}

type ResearchConclusionDraft struct {
	ConclusionID       string   `json:"conclusion_id"`
	Text               string   `json:"text"`
	SupportEvidenceIDs []string `json:"support_evidence_ids"`
	CitationIDs        []string `json:"citation_ids"`
	Confidence         float64  `json:"confidence"`
}

type ResearchSynthesizerOutput struct {
	DecisionSummary string                    `json:"decision_summary"`
	Conclusions     []ResearchConclusionDraft `json:"conclusions"`
}

type ResearchVerifierOutput struct {
	DecisionSummary       string   `json:"decision_summary"`
	Verdict               string   `json:"verdict"`
	VerifiedConclusionIDs []string `json:"verified_conclusion_ids"`
	Gaps                  []string `json:"gaps"`
	Warnings              []string `json:"warnings,omitempty"`
}

type researchStageModel struct {
	client BookKnowledgeLLMClientWithResult
}

func NewResearchStageModel(client BookKnowledgeLLMClientWithResult) ResearchStageModel {
	if client == nil {
		client = NewTokenPlanChatClient(nil)
	}
	return &researchStageModel{client: client}
}

func (m *researchStageModel) Run(
	ctx context.Context,
	role ResearchModelRole,
	config BookTokenPlanConfig,
	messages []BookKnowledgeMessage,
	references ResearchModelReferences,
	output any,
) (ResearchModelUsage, error) {
	if err := validateResearchModelOutputType(role, output); err != nil {
		return ResearchModelUsage{}, fmt.Errorf("%w: %v", ErrResearchInvalidModelOutput, err)
	}
	config.Model = normalizeBookTokenPlanModel(config.Model)
	applyStructuredQwenThinkingPolicy(&config)
	result, err := m.client.ChatWithResult(ctx, config, messages)
	if err != nil {
		return ResearchModelUsage{}, err
	}
	if err := decodeStrictBookJSON(result.Content, output, researchModelResponseMax); err != nil {
		return ResearchModelUsage{}, fmt.Errorf("%w: %v", ErrResearchInvalidModelOutput, err)
	}
	availableEvidence := stringBoolSet(references.EvidenceIDs...)
	availableCitations := stringBoolSet(references.CitationIDs...)
	availableConclusions := stringBoolSet(references.ConclusionIDs...)
	if err := validateResearchModelOutput(role, output, availableEvidence, availableCitations, availableConclusions); err != nil {
		return ResearchModelUsage{}, fmt.Errorf("%w: %v", ErrResearchInvalidModelOutput, err)
	}
	usage := ResearchModelUsage{}
	if result.Usage != nil {
		usage.InputTokens = result.Usage.PromptTokens
		usage.OutputTokens = result.Usage.CompletionTokens
		usage.TotalTokens = result.Usage.TotalTokens
		if result.Usage.CostUSD != nil {
			usage.CostUSD = *result.Usage.CostUSD
		}
	}
	return usage, nil
}

func validateResearchModelOutputType(role ResearchModelRole, output any) error {
	valid := false
	switch role {
	case ResearchRolePlanner:
		_, valid = output.(*ResearchPlannerOutput)
	case ResearchRoleExtractor:
		_, valid = output.(*ResearchExtractorOutput)
	case ResearchRoleSynthesizer:
		_, valid = output.(*ResearchSynthesizerOutput)
	case ResearchRoleVerifier:
		_, valid = output.(*ResearchVerifierOutput)
	default:
		return fmt.Errorf("unsupported research model role %q", role)
	}
	if !valid {
		return fmt.Errorf("research model role %q received an incompatible output type", role)
	}
	return nil
}

func validateResearchModelOutput(
	role ResearchModelRole,
	output any,
	availableEvidence, availableCitations, availableConclusions map[string]bool,
) error {
	decisionSummary := ""
	switch role {
	case ResearchRolePlanner:
		value := output.(*ResearchPlannerOutput)
		decisionSummary = value.DecisionSummary
		if value.ToolCalls == nil {
			return fmt.Errorf("planner tool_calls array is required")
		}
		if len(value.ToolCalls) > researchModelToolCallsMax {
			return fmt.Errorf("planner tool_calls exceeds %d items", researchModelToolCallsMax)
		}
		allowed := researchModelAllowedTools()
		for _, call := range value.ToolCalls {
			if !allowed[strings.TrimSpace(call.Tool)] {
				return fmt.Errorf("unsupported research tool %q", call.Tool)
			}
			if call.Arguments == nil {
				return fmt.Errorf("research tool arguments object is required")
			}
			encoded, err := json.Marshal(call.Arguments)
			if err != nil || len(encoded) > researchWorkerArgumentsMaxBytes {
				return fmt.Errorf("research tool arguments exceed supported bounds")
			}
		}
	case ResearchRoleExtractor:
		value := output.(*ResearchExtractorOutput)
		decisionSummary = value.DecisionSummary
		if value.Facts == nil || value.Claims == nil || value.Measurements == nil || value.Cases == nil {
			return fmt.Errorf("extractor facts, claims, measurements, and cases arrays are required")
		}
		if len(value.Facts) > researchModelArrayMax || len(value.Claims) > researchModelArrayMax ||
			len(value.Measurements) > researchModelArrayMax || len(value.Cases) > researchModelArrayMax {
			return fmt.Errorf("extractor array exceeds %d items", researchModelArrayMax)
		}
		for _, fact := range value.Facts {
			if strings.TrimSpace(fact.FactID) == "" || strings.TrimSpace(fact.Kind) == "" || strings.TrimSpace(fact.Summary) == "" {
				return fmt.Errorf("fact id, kind, and summary are required")
			}
			if fact.Confidence <= 0 || fact.Confidence > 1 || !validResearchModelReviewState(fact.ReviewState) {
				return fmt.Errorf("fact confidence and review_state are invalid")
			}
			if strings.TrimSpace(fact.OccurredAt) != "" {
				if _, err := time.Parse(time.RFC3339, fact.OccurredAt); err != nil {
					return fmt.Errorf("fact occurred_at must be RFC3339")
				}
			}
			if len(uniqueSortedResearchStrings(fact.EvidenceIDs)) == 0 {
				return fmt.Errorf("fact evidence_ids are required")
			}
			if err := requireResearchModelReferences(fact.EvidenceIDs, availableEvidence, "evidence"); err != nil {
				return err
			}
		}
		for _, claim := range value.Claims {
			if strings.TrimSpace(claim.ClaimID) == "" || strings.TrimSpace(claim.Kind) == "" ||
				strings.TrimSpace(claim.Topic) == "" || strings.TrimSpace(claim.Value) == "" {
				return fmt.Errorf("claim id, kind, topic, and value are required")
			}
			if claim.Confidence <= 0 || claim.Confidence > 1 || !validResearchModelReviewState(claim.ReviewState) {
				return fmt.Errorf("claim confidence and review_state are invalid")
			}
			if len(uniqueSortedResearchStrings(claim.EvidenceIDs)) == 0 {
				return fmt.Errorf("claim evidence_ids are required")
			}
			if err := requireResearchModelReferences(claim.EvidenceIDs, availableEvidence, "evidence"); err != nil {
				return err
			}
		}
		for _, measurement := range value.Measurements {
			if strings.TrimSpace(measurement.MeasurementID) == "" || strings.TrimSpace(measurement.Name) == "" ||
				strings.TrimSpace(measurement.OccurredAt) == "" || measurement.Confidence <= 0 || measurement.Confidence > 1 {
				return fmt.Errorf("measurement id, name, occurred_at, and confidence are required")
			}
			if _, err := time.Parse(time.RFC3339, measurement.OccurredAt); err != nil {
				return fmt.Errorf("measurement occurred_at must be RFC3339")
			}
			if len(uniqueSortedResearchStrings(measurement.EvidenceIDs)) == 0 {
				return fmt.Errorf("measurement evidence_ids are required")
			}
			if err := requireResearchModelReferences(measurement.EvidenceIDs, availableEvidence, "evidence"); err != nil {
				return err
			}
		}
		for _, researchCase := range value.Cases {
			if strings.TrimSpace(researchCase.CaseID) == "" ||
				(researchCase.Role != "historical" && researchCase.Role != "current") ||
				len(uniqueSortedResearchStrings(researchCase.EvidenceIDs)) == 0 {
				return fmt.Errorf("case id, historical/current role, and evidence are required")
			}
			if err := requireResearchModelReferences(researchCase.EvidenceIDs, availableEvidence, "evidence"); err != nil {
				return err
			}
		}
	case ResearchRoleSynthesizer:
		value := output.(*ResearchSynthesizerOutput)
		decisionSummary = value.DecisionSummary
		if value.Conclusions == nil {
			return fmt.Errorf("synthesizer conclusions array is required")
		}
		if len(value.Conclusions) > researchModelArrayMax {
			return fmt.Errorf("synthesizer conclusions exceeds %d items", researchModelArrayMax)
		}
		for _, conclusion := range value.Conclusions {
			if strings.TrimSpace(conclusion.ConclusionID) == "" || strings.TrimSpace(conclusion.Text) == "" ||
				len(uniqueSortedResearchStrings(conclusion.SupportEvidenceIDs)) == 0 || conclusion.CitationIDs == nil {
				return fmt.Errorf("conclusion id, text, and support evidence are required")
			}
			if err := requireResearchModelReferences(conclusion.SupportEvidenceIDs, availableEvidence, "evidence"); err != nil {
				return err
			}
			if err := requireResearchModelReferences(conclusion.CitationIDs, availableCitations, "citation"); err != nil {
				return err
			}
			if conclusion.Confidence <= 0 || conclusion.Confidence > 1 {
				return fmt.Errorf("conclusion confidence must be within (0,1]")
			}
		}
	case ResearchRoleVerifier:
		value := output.(*ResearchVerifierOutput)
		decisionSummary = value.DecisionSummary
		if value.VerifiedConclusionIDs == nil || value.Gaps == nil || value.Warnings == nil {
			return fmt.Errorf("verifier verified_conclusion_ids, gaps, and warnings arrays are required")
		}
		if len(value.VerifiedConclusionIDs) > researchModelArrayMax || len(value.Gaps) > researchModelArrayMax || len(value.Warnings) > researchModelArrayMax {
			return fmt.Errorf("verifier array exceeds %d items", researchModelArrayMax)
		}
		switch value.Verdict {
		case ResearchVerifierVerified, ResearchVerifierGaps, ResearchVerifierInsufficient:
		default:
			return fmt.Errorf("verifier verdict is invalid")
		}
		if err := requireResearchModelReferences(value.VerifiedConclusionIDs, availableConclusions, "conclusion"); err != nil {
			return err
		}
	}
	decisionSummary = strings.TrimSpace(decisionSummary)
	if decisionSummary == "" || len([]rune(decisionSummary)) > researchDecisionSummaryMax {
		return fmt.Errorf("decision_summary is required and must not exceed %d characters", researchDecisionSummaryMax)
	}
	return nil
}

func validResearchModelReviewState(value string) bool {
	return value == ResearchReviewPending || value == ResearchReviewVerified || value == ResearchReviewRejected
}

func requireResearchModelReferences(values []string, available map[string]bool, kind string) error {
	for _, value := range uniqueSortedResearchStrings(values) {
		if !available[value] {
			return fmt.Errorf("unreferenced %s %q", kind, value)
		}
	}
	return nil
}

func researchModelAllowedTools() map[string]bool {
	return stringBoolSet(
		ResearchToolSearchKnowledge, ResearchToolSearchPriorRuns,
		ResearchWorkerToolSearchChatlog,
		ResearchWorkerToolResolveChatIdentity, ResearchWorkerToolListIdentityConversations,
	)
}
