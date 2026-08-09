# 百炼最新文本模型 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为 KBase 得到文章分析和书籍知识库分析增加百炼最新文本模型，同时保留 Qwen 3.7 Max 默认值。

**Architecture:** 继续复用前端单一 `knowledgeAnalysisModels` 白名单和后端 OpenAI-compatible Token Plan 客户端。前端只提交官方模型 ID；后端仅规范化常见显示名称，不新增动态目录接口或跨仓依赖。

**Tech Stack:** Vanilla JavaScript smoke tests、Go 单元测试、百炼 Token Plan OpenAI-compatible API。

---

### Task 1: 固化前端模型目录契约

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Modify: `frontend-web/app.js`

**Step 1: Write the failing test**

在 Web smoke 中断言以下 ID 与标签存在：

```js
for (const [id, label] of [
  ["qwen3.8-max-preview", "Qwen-3.8-Max（预览版）"],
  ["qwen3.7-max", "Qwen-3.7-Max"],
  ["qwen3.7-plus", "Qwen-3.7-Plus"],
  ["deepseek-v4-pro", "DeepSeek V4 Pro"],
  ["deepseek-v4-flash", "DeepSeek V4 Flash"],
  ["kimi-k2.7-code", "Kimi K2.7 Code"],
  ["glm-5.2", "GLM-5.2"],
  ["MiniMax-M2.5", "MiniMax-M2.5"],
]) {
  assert.ok(js.includes(`id: "${id}", label: "${label}"`));
}
```

并继续断言两个分析状态的默认值是 `qwen3.7-max`。

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL，指出 `qwen3.8-max-preview` 或 `glm-5.2` 尚未注册。

**Step 3: Write minimal implementation**

扩展 `knowledgeAnalysisModels`，只加入设计说明中的八个文本模型，不改两个默认模型状态。

**Step 4: Run test to verify it passes**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: PASS。

### Task 2: 规范化最新模型显示名称

**Files:**
- Modify: `backend/app/book_chat_test.go`
- Modify: `backend/app/book_chat.go`

**Step 1: Write the failing test**

新增表驱动测试：

```go
func TestNormalizeBookTokenPlanLatestModels(t *testing.T) {
    cases := map[string]string{
        "Qwen-3.8-Max-Preview": "qwen3.8-max-preview",
        "GLM-5.2": "glm-5.2",
        "DeepSeek V4 Pro": "deepseek-v4-pro",
        "DeepSeek V4 Flash": "deepseek-v4-flash",
        "Kimi K2.7 Code": "kimi-k2.7-code",
        "MiniMax M2.5": "MiniMax-M2.5",
    }
    // compare normalizeBookTokenPlanModel for every case
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run TestNormalizeBookTokenPlanLatestModels -count=1`

Expected: FAIL，现有函数会保留部分显示名称而不是返回官方 ID。

**Step 3: Write minimal implementation**

在 `normalizeBookTokenPlanModel` 的既有 switch 中增加对应 compact key 映射；官方 ID 原样输入时结果保持幂等。

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'TestNormalizeBookTokenPlanLatestModels|TestBookKnowledgeChatCanonicalizesQwenDisplayLabel' -count=1`

Expected: PASS。

### Task 3: 刷新缓存版本并完成发布前验证

**Files:**
- Modify: `frontend-web/index.html`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write the failing test**

在 Web smoke 中断言 `index.html` 包含新的 `20260809-tokenplan-models` 缓存版本标记。

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL，指出新版本标记缺失。

**Step 3: Write minimal implementation**

只更新 `app.js` 的查询版本，CSS 未变化，不改其版本。

**Step 4: Run focused and full verification**

Run:

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
go test ./backend/app -run 'TestNormalizeBookTokenPlanLatestModels|TestBookKnowledgeChatCanonicalizesQwenDisplayLabel' -count=1
go test ./...
cd frontend && npm run build
cd ..
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: 所有命令退出码为 0；状态中只包含本次模型功能相关文件。

### Task 4: 提交功能

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/index.html`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Modify: `backend/app/book_chat.go`
- Modify: `backend/app/book_chat_test.go`
- Create: `docs/plans/2026-08-09-tokenplan-latest-models-design.md`
- Create: `docs/plans/2026-08-09-tokenplan-latest-models.md`

**Step 1: Review diff**

确认没有密钥、Cookie、本机路径、下载内容或无关文件。

**Step 2: Commit only task files**

```bash
git add frontend-web/app.js frontend-web/index.html frontend-web/scripts/book-knowledge-web-smoke.mjs backend/app/book_chat.go backend/app/book_chat_test.go docs/plans/2026-08-09-tokenplan-latest-models.md
git commit -m "feat(kbase): add latest TokenPlan models"
```
