package app

import (
	"strings"
	"testing"
)

func TestGenerateBookKnowledgePromptsIncludesTemplatesAndDynamicPrompts(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	prompts, err := GenerateBookKnowledgePrompts(store, "42")
	if err != nil {
		t.Fatalf("GenerateBookKnowledgePrompts returned error: %v", err)
	}
	if len(prompts) < 25 {
		t.Fatalf("prompts length = %d, want at least 25 static and dynamic prompts", len(prompts))
	}

	seen := map[string]BookKnowledgePrompt{}
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt.PromptID) == "" || strings.TrimSpace(prompt.Title) == "" || strings.TrimSpace(prompt.Prompt) == "" {
			t.Fatalf("prompt has empty required fields: %#v", prompt)
		}
		if !strings.Contains(prompt.Prompt, "claim_id") && !strings.Contains(prompt.Prompt, "chunk_id") {
			t.Fatalf("prompt %s does not require source citations: %s", prompt.PromptID, prompt.Prompt)
		}
		seen[prompt.PromptID] = prompt
	}

	if !seen["dynamic-chapter-map"].Dynamic {
		t.Fatalf("dynamic-chapter-map missing or not dynamic: %#v", seen["dynamic-chapter-map"])
	}
	if !seen["dynamic-claim-clusters"].Dynamic {
		t.Fatalf("dynamic-claim-clusters missing or not dynamic: %#v", seen["dynamic-claim-clusters"])
	}
	if !strings.Contains(seen["dynamic-chapter-map"].Prompt, "趋势过滤") {
		t.Fatalf("dynamic chapter prompt should include chapter titles: %s", seen["dynamic-chapter-map"].Prompt)
	}
	if !strings.Contains(seen["dynamic-claim-clusters"].Prompt, "趋势过滤") {
		t.Fatalf("dynamic claim prompt should include representative claim titles: %s", seen["dynamic-claim-clusters"].Prompt)
	}
	if !strings.Contains(seen["project-quant-rules"].Prompt, "paper-only") {
		t.Fatalf("quant rules prompt should include paper-only guardrail: %s", seen["project-quant-rules"].Prompt)
	}
	if !strings.Contains(seen["project-health-kb"].Prompt, "review_status") {
		t.Fatalf("health KB prompt should include review_status governance: %s", seen["project-health-kb"].Prompt)
	}
}

func TestGenerateBookKnowledgePromptsDoesNotExposePrivateProjectNames(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	prompts, err := GenerateBookKnowledgePrompts(store, "42")
	if err != nil {
		t.Fatalf("GenerateBookKnowledgePrompts returned error: %v", err)
	}

	for _, prompt := range prompts {
		combined := strings.Join([]string{
			prompt.PromptID,
			prompt.Category,
			prompt.Title,
			prompt.Description,
			prompt.Prompt,
		}, "\n")
		for _, forbidden := range []string{
			"health" + "-llm-driven",
			"macd" + "-analysis-claude",
			"/" + "Users" + "/",
			"li" + "qiuhua",
		} {
			if strings.Contains(combined, forbidden) {
				t.Fatalf("prompt %s exposes private token %q:\n%s", prompt.PromptID, forbidden, combined)
			}
		}
	}
}

func TestGenerateBookKnowledgePromptsUsesBookShape(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	prompts, err := GenerateBookKnowledgePrompts(store, "42")
	if err != nil {
		t.Fatalf("GenerateBookKnowledgePrompts returned error: %v", err)
	}
	var shapePrompt BookKnowledgePrompt
	for _, prompt := range prompts {
		if prompt.PromptID == "dynamic-book-shape" {
			shapePrompt = prompt
			break
		}
	}
	if shapePrompt.PromptID == "" {
		t.Fatal("dynamic-book-shape prompt missing")
	}
	for _, want := range []string{"1 章", "1 claims", "1 chunks", "42_量化分析_作者"} {
		if !strings.Contains(shapePrompt.Prompt, want) {
			t.Fatalf("dynamic book shape prompt missing %q: %s", want, shapePrompt.Prompt)
		}
	}
}
