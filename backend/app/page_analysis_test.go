package app

import (
	"context"
	"strings"
	"testing"
)

func TestPageAnalysisRejectsEmptyContext(t *testing.T) {
	client := &fakeBookKnowledgeLLMClient{answer: "unused"}

	_, err := AnalyzePageWithClient(context.Background(), PageAnalysisRequest{
		Source:   "course",
		Title:    "有效学习",
		Question: "帮我总结当前页面",
		Model:    "qwen3.7-max",
	}, client)

	if err == nil || !strings.Contains(err.Error(), "context_sections is required") {
		t.Fatalf("AnalyzePageWithClient error = %v, want context_sections validation", err)
	}
	if len(client.messages) != 0 {
		t.Fatalf("LLM client was called for invalid request: %#v", client.messages)
	}
}

func TestPageAnalysisBuildsGroundedPrompt(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-page-test")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "MiniMax-M2.5")
	client := &fakeBookKnowledgeLLMClient{answer: "这页适合先提炼概念，再生成复习问题。"}

	resp, err := AnalyzePageWithClient(context.Background(), PageAnalysisRequest{
		Source:   "course",
		Title:    "有效学习",
		URL:      "/course/course-enid",
		Mode:     "study",
		Question: "分析当前课程页面，并给出学习建议",
		Model:    "qwen3.7-max",
		ContextSections: []PageAnalysisSection{
			{Title: "课程信息", Content: "讲师: 王老师\n简介: 讲如何学习"},
			{Title: "当前文章", Content: "主动回忆比重复阅读更有效。"},
		},
	}, client)
	if err != nil {
		t.Fatalf("AnalyzePageWithClient returned error: %v", err)
	}
	if resp.Answer != "这页适合先提炼概念，再生成复习问题。" {
		t.Fatalf("answer = %q", resp.Answer)
	}
	if resp.Model != "qwen3.7-max" || resp.Source != "course" || resp.Mode != "study" {
		t.Fatalf("response metadata = %#v", resp)
	}
	if resp.ContextStats.Sections != 2 || resp.ContextStats.Chars == 0 {
		t.Fatalf("context stats = %#v", resp.ContextStats)
	}
	if client.cfg.Model != "qwen3.7-max" {
		t.Fatalf("model = %q, want qwen3.7-max", client.cfg.Model)
	}
	if len(client.messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(client.messages))
	}
	prompt := client.messages[1].Content
	for _, want := range []string{
		"有效学习",
		"/course/course-enid",
		"课程信息",
		"当前文章",
		"主动回忆比重复阅读更有效",
		"分析当前课程页面",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
