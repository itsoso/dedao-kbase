package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeResearchResultClient struct {
	result BookKnowledgeLLMResult
	err    error
	calls  int
	config BookTokenPlanConfig
}

func (c *fakeResearchResultClient) ChatWithResult(_ context.Context, config BookTokenPlanConfig, _ []BookKnowledgeMessage) (BookKnowledgeLLMResult, error) {
	c.calls++
	c.config = config
	return c.result, c.err
}

func TestResearchModelRunsRoleSpecificStrictOutputs(t *testing.T) {
	boolFalse := false
	config := BookTokenPlanConfig{APIKey: "synthetic", BaseURL: "https://provider.invalid/v1", Model: "Qwen-3.7-Plus", EnableThinking: &boolFalse}
	tests := []struct {
		role    ResearchModelRole
		content string
		output  any
		check   func(*testing.T, any)
	}{
		{
			role:    ResearchRolePlanner,
			content: `{"decision_summary":"Search bounded private history","tool_calls":[{"tool":"search_chatlog","arguments":{"talker_ref":"room-a","time_from":"2032-01-01T00:00:00Z","time_to":"2032-01-02T00:00:00Z","limit":20}}]}`,
			output:  &ResearchPlannerOutput{},
			check: func(t *testing.T, output any) {
				if len(output.(*ResearchPlannerOutput).ToolCalls) != 1 {
					t.Fatalf("planner = %#v", output)
				}
			},
		},
		{
			role:    ResearchRoleExtractor,
			content: `{"decision_summary":"Extract typed grounded records","facts":[{"fact_id":"fact-a","kind":"observation","summary":"Synthetic fact","evidence_ids":["evidence-a"],"confidence":0.9,"review_state":"pending"}],"claims":[],"measurements":[{"measurement_id":"measurement-a","name":"ct","value":18,"occurred_at":"2032-01-01T00:00:00Z","evidence_ids":["evidence-a"],"confidence":0.9}],"cases":[{"case_id":"case-a","role":"current","age":34,"stage_day":4,"evidence_ids":["evidence-a"]}]}`,
			output:  &ResearchExtractorOutput{},
			check: func(t *testing.T, output any) {
				if len(output.(*ResearchExtractorOutput).Facts) != 1 || len(output.(*ResearchExtractorOutput).Measurements) != 1 || len(output.(*ResearchExtractorOutput).Cases) != 1 {
					t.Fatalf("extractor = %#v", output)
				}
			},
		},
		{
			role:    ResearchRoleSynthesizer,
			content: `{"decision_summary":"Synthesize grounded result","conclusions":[{"conclusion_id":"conclusion-a","text":"Synthetic conclusion","support_evidence_ids":["evidence-a"],"citation_ids":["citation-a"],"confidence":0.9}]}`,
			output:  &ResearchSynthesizerOutput{},
			check: func(t *testing.T, output any) {
				if len(output.(*ResearchSynthesizerOutput).Conclusions) != 1 {
					t.Fatalf("synthesizer = %#v", output)
				}
			},
		},
		{
			role:    ResearchRoleVerifier,
			content: `{"decision_summary":"All conclusions are grounded","verdict":"verified","verified_conclusion_ids":["conclusion-a"],"gaps":[]}`,
			output:  &ResearchVerifierOutput{},
			check: func(t *testing.T, output any) {
				if output.(*ResearchVerifierOutput).Verdict != ResearchVerifierVerified {
					t.Fatalf("verifier = %#v", output)
				}
			},
		},
	}
	for _, testCase := range tests {
		t.Run(string(testCase.role), func(t *testing.T) {
			client := &fakeResearchResultClient{result: BookKnowledgeLLMResult{
				Content: testCase.content,
				Usage:   &BookKnowledgeLLMUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
			}}
			model := NewResearchStageModel(client)
			usage, err := model.Run(context.Background(), testCase.role, config,
				[]BookKnowledgeMessage{{Role: "user", Content: "Available [evidence:evidence-a] [citation:citation-a] [conclusion:conclusion-a]"}}, testCase.output)
			if err != nil {
				t.Fatal(err)
			}
			if usage.InputTokens != 11 || usage.OutputTokens != 7 || usage.TotalTokens != 18 || client.calls != 1 {
				t.Fatalf("usage=%#v calls=%d", usage, client.calls)
			}
			if client.config.Model != "qwen3.7-plus" || client.config.EnableThinking == nil || *client.config.EnableThinking {
				t.Fatalf("normalized model config = %#v", client.config)
			}
			testCase.check(t, testCase.output)
		})
	}
}

func TestResearchModelRejectsNonStrictOrUnsupportedOutput(t *testing.T) {
	overLimitFacts := make([]string, researchModelArrayMax+1)
	for index := range overLimitFacts {
		overLimitFacts[index] = fmt.Sprintf(`{"fact_id":"fact-%d","kind":"observation","summary":"x","evidence_ids":["evidence-a"],"confidence":0.8,"review_state":"pending"}`, index)
	}
	tests := []struct {
		name    string
		role    ResearchModelRole
		content string
		output  any
		want    string
	}{
		{"markdown wrapped json", ResearchRolePlanner, "```json\n{\"decision_summary\":\"x\",\"tool_calls\":[]}\n```", &ResearchPlannerOutput{}, "strict JSON"},
		{"unknown field", ResearchRolePlanner, `{"decision_summary":"x","tool_calls":[],"hidden_reasoning":"private"}`, &ResearchPlannerOutput{}, "unknown field"},
		{"unsupported tool", ResearchRolePlanner, `{"decision_summary":"x","tool_calls":[{"tool":"write_chatlog","arguments":{}}]}`, &ResearchPlannerOutput{}, "unsupported"},
		{"over limit array", ResearchRoleExtractor, `{"decision_summary":"x","facts":[` + strings.Join(overLimitFacts, ",") + `],"claims":[]}`, &ResearchExtractorOutput{}, "exceeds"},
		{"unreferenced evidence", ResearchRoleExtractor, `{"decision_summary":"x","facts":[{"fact_id":"fact-a","kind":"observation","summary":"x","evidence_ids":["evidence-missing"],"confidence":0.8,"review_state":"pending"}],"claims":[]}`, &ResearchExtractorOutput{}, "unreferenced evidence"},
		{"unreferenced measurement", ResearchRoleExtractor, `{"decision_summary":"x","facts":[],"claims":[],"measurements":[{"measurement_id":"measurement-a","name":"ct","value":18,"occurred_at":"2032-01-01T00:00:00Z","evidence_ids":["evidence-missing"],"confidence":0.8}]}`, &ResearchExtractorOutput{}, "unreferenced evidence"},
		{"conclusion without support", ResearchRoleSynthesizer, `{"decision_summary":"x","conclusions":[{"conclusion_id":"conclusion-a","text":"x","support_evidence_ids":[],"citation_ids":[],"confidence":0.8}]}`, &ResearchSynthesizerOutput{}, "support"},
		{"unreferenced citation", ResearchRoleSynthesizer, `{"decision_summary":"x","conclusions":[{"conclusion_id":"conclusion-a","text":"x","support_evidence_ids":["evidence-a"],"citation_ids":["citation-missing"],"confidence":0.8}]}`, &ResearchSynthesizerOutput{}, "unreferenced citation"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeResearchResultClient{result: BookKnowledgeLLMResult{Content: testCase.content}}
			_, err := NewResearchStageModel(client).Run(context.Background(), testCase.role,
				BookTokenPlanConfig{APIKey: "synthetic", Model: "qwen-plus"},
				[]BookKnowledgeMessage{{Role: "user", Content: "Available [evidence:evidence-a] [citation:citation-a]"}}, testCase.output)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want %q", err, testCase.want)
			}
		})
	}
}
