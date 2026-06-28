package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type PageAnalysisRequest struct {
	Source          string                `json:"source"`
	Title           string                `json:"title"`
	URL             string                `json:"url,omitempty"`
	Mode            string                `json:"mode,omitempty"`
	Question        string                `json:"question"`
	Model           string                `json:"model,omitempty"`
	MaxContextChars int                   `json:"max_context_chars,omitempty"`
	ContextSections []PageAnalysisSection `json:"context_sections"`
}

type PageAnalysisSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type PageAnalysisResponse struct {
	Answer       string                   `json:"answer"`
	Model        string                   `json:"model"`
	Mode         string                   `json:"mode"`
	Source       string                   `json:"source"`
	ContextStats PageAnalysisContextStats `json:"context_stats"`
	CreatedAt    string                   `json:"created_at"`
}

type PageAnalysisContextStats struct {
	Sections int `json:"sections"`
	Chars    int `json:"chars"`
}

func AnalyzePage(ctx context.Context, request PageAnalysisRequest) (*PageAnalysisResponse, error) {
	return AnalyzePageWithClient(ctx, request, NewTokenPlanChatClient(nil))
}

func AnalyzePageWithClient(
	ctx context.Context,
	request PageAnalysisRequest,
	client BookKnowledgeLLMClient,
) (*PageAnalysisResponse, error) {
	if client == nil {
		client = NewTokenPlanChatClient(nil)
	}
	source := normalizePageAnalysisSource(request.Source)
	mode := normalizePageAnalysisMode(request.Mode)
	question := strings.TrimSpace(request.Question)
	if question == "" {
		question = defaultPageAnalysisQuestion(mode)
	}
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if request.MaxContextChars <= 0 {
		request.MaxContextChars = 12000
	}

	contextText, stats := buildPageAnalysisContext(request, request.MaxContextChars)
	if strings.TrimSpace(contextText) == "" {
		return nil, fmt.Errorf("context_sections is required")
	}

	cfg, err := LoadBookTokenPlanConfig()
	if err != nil {
		return nil, err
	}
	if model := strings.TrimSpace(request.Model); model != "" {
		cfg.Model = model
	}

	messages := []BookKnowledgeMessage{
		{
			Role:    "system",
			Content: "你是 dedao-gui Web 版的学习分析助手。只能基于用户提供的当前课程或电子书页面上下文回答；区分页面事实、你的推理和建议；不要编造页面外内容。",
		},
		{
			Role:    "user",
			Content: buildPageAnalysisPrompt(source, mode, strings.TrimSpace(request.Title), strings.TrimSpace(request.URL), question, contextText),
		},
	}
	answer, err := client.Chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	return &PageAnalysisResponse{
		Answer:       answer,
		Model:        cfg.Model,
		Mode:         mode,
		Source:       source,
		ContextStats: stats,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func buildPageAnalysisContext(request PageAnalysisRequest, maxChars int) (string, PageAnalysisContextStats) {
	var builder strings.Builder
	stats := PageAnalysisContextStats{}
	appendSection := func(title, content string) bool {
		title = strings.TrimSpace(title)
		content = strings.TrimSpace(content)
		if content == "" {
			return true
		}
		if title == "" {
			title = "页面片段"
		}
		section := fmt.Sprintf("## %s\n%s", title, content)
		if builder.Len()+len([]rune(section))+2 > maxChars {
			remaining := maxChars - builder.Len() - 2
			if remaining <= 80 {
				return false
			}
			section = trimRunes(section, remaining)
		}
		builder.WriteString(section)
		builder.WriteString("\n\n")
		stats.Sections++
		return true
	}

	for _, section := range request.ContextSections {
		if !appendSection(section.Title, section.Content) {
			break
		}
	}
	stats.Chars = len([]rune(builder.String()))
	return strings.TrimSpace(builder.String()), stats
}

func buildPageAnalysisPrompt(source, mode, title, pageURL, question, contextText string) string {
	if title == "" {
		title = "未命名页面"
	}
	return fmt.Sprintf(`请分析当前学习页面。

页面类型: %s
页面标题: %s
页面地址: %s
任务模式: %s
问题: %s

要求:
- 先给结论，再给依据。
- 只基于下面的页面上下文，不要补充未提供的内容。
- 对课程或电子书学习给出可执行建议。
- 如果上下文不足，明确说明缺口。
- 输出 Markdown。

页面上下文:
%s`, source, title, pageURL, mode, question, contextText)
}

func normalizePageAnalysisSource(source string) string {
	switch strings.TrimSpace(source) {
	case "course", "ebook", "book", "page":
		return strings.TrimSpace(source)
	default:
		return "page"
	}
}

func normalizePageAnalysisMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "summary", "study", "questions", "actions", "chat":
		return strings.TrimSpace(mode)
	default:
		return "study"
	}
}

func defaultPageAnalysisQuestion(mode string) string {
	switch mode {
	case "summary":
		return "总结当前页面的核心内容和重点。"
	case "questions":
		return "基于当前页面生成适合复习的关键问题和参考答案。"
	case "actions":
		return "把当前页面内容转化成下一步学习行动清单。"
	case "chat":
		return ""
	default:
		return "分析当前页面的重点、难点和建议学习路径。"
	}
}
