package app

import (
	"fmt"
	"strconv"
	"strings"
)

type BookKnowledgePrompt struct {
	PromptID     string `json:"prompt_id"`
	Category     string `json:"category"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Prompt       string `json:"prompt"`
	OutputFormat string `json:"output_format,omitempty"`
	Dynamic      bool   `json:"dynamic,omitempty"`
}

func GenerateBookKnowledgePrompts(store *BookKnowledgeStore, bookID string) ([]BookKnowledgePrompt, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	pkg, err := store.LoadPackage(bookID)
	if err != nil {
		return nil, err
	}
	prompts := make([]BookKnowledgePrompt, 0, len(staticBookKnowledgePrompts)+6)
	prompts = append(prompts, staticBookKnowledgePrompts...)
	prompts = append(prompts, dynamicBookKnowledgePrompts(pkg)...)
	return prompts, nil
}

var staticBookKnowledgePrompts = []BookKnowledgePrompt{
	staticPrompt("understand-core", "理解本书", "核心结论", "提炼本书最重要的观点", "请基于本书 claims 和 chunks，总结本书最核心的 5 个观点。每个观点必须引用 claim_id 或 chunk_id，并说明适合落地到什么场景。", "markdown"),
	staticPrompt("understand-map", "理解本书", "章节脉络", "按章节建立阅读地图", "请按章节梳理本书结构，说明每章解决的问题、关键论点和与下一章的关系。每章至少引用一个 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("understand-concepts", "理解本书", "概念词典", "提取核心概念和定义", "请提取本书最重要的 20 个概念，给出定义、上下位关系、常见误解和来源 claim_id 或 chunk_id。", "table"),
	staticPrompt("understand-one-page", "理解本书", "一页纸摘要", "生成适合复习的一页纸", "请把本书压缩成一页纸摘要：核心问题、核心答案、关键证据、行动建议。每个部分必须引用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("understand-feynman", "理解本书", "费曼讲解", "用通俗语言讲清楚", "请用费曼技巧解释本书：先用普通人能懂的语言讲，再指出哪些地方容易误解，并用 claim_id 或 chunk_id 标出依据。", "markdown"),
	staticPrompt("critic-assumptions", "批判阅读", "隐含假设", "找出书中未明说的前提", "请批判性分析本书，列出 8 个隐含假设。每个假设说明成立条件、失败场景、需要补充验证的数据，并引用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("critic-counterexamples", "批判阅读", "反例清单", "寻找可能推翻观点的情况", "请列出本书主要观点的可能反例或边界条件。每条反例都要对应一个 claim_id 或 chunk_id，并说明如何验证。", "markdown"),
	staticPrompt("critic-evidence", "批判阅读", "证据审计", "检查证据强弱", "请审计本书 claims 的证据质量，按强/中/弱分类，指出证据不足的观点和需要外部数据验证的地方。每项引用 claim_id 或 chunk_id。", "table"),
	staticPrompt("critic-contradictions", "批判阅读", "内部矛盾", "寻找书内冲突", "请寻找本书中可能互相冲突、张力较大或定义不一致的观点。每组冲突都引用相关 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("critic-debate", "批判阅读", "正反辩论", "用辩论方式分析", "请围绕本书最重要的 3 个观点分别写出支持方和反对方论证，并为双方都引用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("action-checklist", "行动转化", "行动清单", "转成可执行任务", "请把本书转化成 10 条可执行行动清单。每条包含触发条件、执行步骤、完成标准、风险提醒和来源 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("action-7-day", "行动转化", "7 天实践计划", "快速验证本书方法", "请设计一个 7 天实践计划来验证本书最可落地的方法。每天给出任务、产出物、复盘问题和来源 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("action-30-day", "行动转化", "30 天落地计划", "长期落地书中方法", "请设计一个 30 天落地计划，分为启动、练习、验证、复盘四阶段。每阶段必须引用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("action-decision", "行动转化", "决策清单", "转成决策辅助工具", "请把本书观点转成决策清单：适用情境、判断问题、红线、行动选项和验证指标。每项引用 claim_id 或 chunk_id。", "table"),
	staticPrompt("action-risk", "行动转化", "风险边界", "避免误用书中观点", "请列出使用本书方法时最容易误用的 10 个风险边界，说明错误用法、后果、修正方式和来源 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("project-health-kb", "项目导入", "Health KB 条目", "导入健康知识库", "请把本书转换成可导入健康知识库的知识条目：实体、claim、证据、适用人群、风险边界、review_status。每条都必须引用 claim_id 或 chunk_id。", "jsonl"),
	staticPrompt("project-quant-rules", "项目导入", "量化规则卡", "导入量化研究项目", "请把本书转换成 paper-only 量化规则卡：市场假设、入场条件、退出条件、风控条件、回测需求、禁止实盘说明。每张规则卡引用 claim_id 或 chunk_id。", "json"),
	staticPrompt("project-mcp", "项目导入", "MCP 工具说明", "生成可供模型调用的工具说明", "请为本书生成 MCP 使用说明：适合查询的问题、工具调用建议、返回字段解释和引用规范。必须说明如何使用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("project-notebooklm", "项目导入", "NotebookLM 首问", "生成首轮提问清单", "请为 NotebookLM 生成首轮提问清单：10 个问题，覆盖总结、反驳、行动、跨书比较和项目导入。每个问题都要求 NotebookLM 返回 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("project-wiki", "项目导入", "Wiki 页面", "生成 wiki 页面结构", "请把本书整理成 wiki 页面结构：概览、核心观点、关键概念、证据索引、行动计划。每个段落引用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("memory-cards", "记忆学习", "问答卡片", "生成复习卡片", "请为本书生成 30 张问答卡片。每张卡片包含问题、答案、难度、复习提示和来源 claim_id 或 chunk_id。", "json"),
	staticPrompt("memory-quiz", "记忆学习", "测验题", "生成自测题", "请基于本书生成 20 道自测题，包含单选、多选、简答和案例题。每题都引用 claim_id 或 chunk_id，并给出解析。", "markdown"),
	staticPrompt("memory-analogies", "记忆学习", "类比记忆", "用类比帮助理解", "请为本书最难理解的 10 个观点创建类比、反类比和适用边界，并引用 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("compare-other-book", "跨书比较", "跨书对比", "和另一书比较", "请生成一个跨书比较提纲：如果我要把本书与另一书比较，应比较哪些主题、观点、证据和行动建议。每个比较维度引用本书 claim_id 或 chunk_id。", "markdown"),
	staticPrompt("compare-consensus", "跨书比较", "共识地图", "提取可与其他书合并的共识", "请提取本书中适合与其他书形成共识地图的观点，按主题聚类，并为每条观点引用 claim_id 或 chunk_id。", "table"),
}

func staticPrompt(id, category, title, description, prompt, outputFormat string) BookKnowledgePrompt {
	return BookKnowledgePrompt{
		PromptID:     id,
		Category:     category,
		Title:        title,
		Description:  description,
		Prompt:       prompt,
		OutputFormat: outputFormat,
	}
}

func dynamicBookKnowledgePrompts(pkg *BookKnowledgePackage) []BookKnowledgePrompt {
	book := pkg.Book
	stats := bookPromptStats(pkg)
	topChapter := "暂无章节"
	if len(pkg.Chapters) > 0 {
		topChapter = pkg.Chapters[0].Title
	}
	keywords := inferBookPromptKeywords(book.Title)
	chapterTitles := samplePromptChapterTitles(pkg.Chapters, 6)
	claimTitles := samplePromptClaimTitles(pkg.Claims, 8)
	return []BookKnowledgePrompt{
		dynamicPrompt("dynamic-book-shape", "本书专属", "按本书结构分析", "根据章节、claims、chunks 数量生成分析路线",
			fmt.Sprintf("这本书《%s》当前知识包包含 %d 章、%d claims、%d chunks。请基于这个结构制定一条分析路线：先读哪些章节、优先验证哪些 claims、哪些 chunks 适合做证据。每个建议都引用 claim_id 或 chunk_id。", book.Title, stats.chapters, stats.claims, stats.chunks), "markdown"),
		dynamicPrompt("dynamic-chapter-map", "本书专属", "章节地图", "围绕第一章和全书结构展开",
			fmt.Sprintf("请以《%s》的章节结构为对象，重点从「%s」开始，结合这些章节线索：%s。生成章节地图：章节问题、关键观点、证据 chunk、后续追问。每章至少引用 claim_id 或 chunk_id。", book.Title, topChapter, strings.Join(chapterTitles, "、")), "markdown"),
		dynamicPrompt("dynamic-claim-clusters", "本书专属", "Claims 聚类", "把本书观点按主题聚类",
			fmt.Sprintf("请将《%s》的 %d 条 claims 聚类成 3-7 个主题。可优先参考这些代表性 claim 标题：%s。每个主题说明核心观点、代表 claim_id、关联 chunk_id、可落地场景和风险边界。", book.Title, stats.claims, strings.Join(claimTitles, "、")), "table"),
		dynamicPrompt("dynamic-project-fit", "本书专属", "项目适配建议", "判断适合导入哪些项目",
			fmt.Sprintf("请判断《%s》更适合导入哪些项目或知识库：健康知识库、量化研究项目、NotebookLM、通用 wiki。请按适配度排序，并引用 claim_id 或 chunk_id 说明原因。", book.Title), "markdown"),
		dynamicPrompt("dynamic-keyword-route", "本书专属", "主题路线", "根据书名关键词生成分析入口",
			fmt.Sprintf("这本书标题中可能涉及这些主题：%s。请据此生成 8 个高价值追问，每个追问说明要检索哪些 claim_id 或 chunk_id，以及预期输出格式。", strings.Join(keywords, "、")), "markdown"),
	}
}

func dynamicPrompt(id, category, title, description, prompt, outputFormat string) BookKnowledgePrompt {
	item := staticPrompt(id, category, title, description, prompt, outputFormat)
	item.Dynamic = true
	return item
}

type promptStats struct {
	chapters int
	claims   int
	chunks   int
}

func bookPromptStats(pkg *BookKnowledgePackage) promptStats {
	return promptStats{
		chapters: len(pkg.Chapters),
		claims:   len(pkg.Claims),
		chunks:   len(pkg.Chunks),
	}
}

func samplePromptChapterTitles(chapters []BookKnowledgeChapter, limit int) []string {
	titles := make([]string, 0, limit)
	for _, chapter := range chapters {
		title := strings.TrimSpace(chapter.Title)
		if title == "" {
			continue
		}
		titles = append(titles, title)
		if len(titles) >= limit {
			break
		}
	}
	if len(titles) == 0 {
		return []string{"暂无章节标题"}
	}
	return titles
}

func samplePromptClaimTitles(claims []BookKnowledgeClaim, limit int) []string {
	titles := make([]string, 0, limit)
	for _, claim := range claims {
		title := strings.TrimSpace(claim.Title)
		if title == "" {
			title = strings.TrimSpace(claim.Summary)
		}
		if title == "" {
			continue
		}
		titles = append(titles, title)
		if len(titles) >= limit {
			break
		}
	}
	if len(titles) == 0 {
		return []string{"暂无 claim 标题"}
	}
	return titles
}

func inferBookPromptKeywords(title string) []string {
	candidates := []string{"核心观点", "行动方法", "证据验证"}
	title = strings.ToLower(title)
	switch {
	case strings.Contains(title, "量化"), strings.Contains(title, "交易"), strings.Contains(title, "投资"), strings.Contains(title, "macd"):
		candidates = append([]string{"量化规则", "回测假设", "风控边界"}, candidates...)
	case strings.Contains(title, "健康"), strings.Contains(title, "医学"), strings.Contains(title, "营养"), strings.Contains(title, "睡眠"):
		candidates = append([]string{"健康知识", "适用人群", "风险边界"}, candidates...)
	case strings.Contains(title, "ai"), strings.Contains(title, "人工智能"), strings.Contains(title, "模型"):
		candidates = append([]string{"AI 概念", "技术路线", "产业落地"}, candidates...)
	}
	for len(candidates) < 5 {
		candidates = append(candidates, "主题 "+strconv.Itoa(len(candidates)+1))
	}
	return candidates[:5]
}
