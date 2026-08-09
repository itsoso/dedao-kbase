# 包契约与阅读应用中文化实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将旧版 Agent Package 的包契约页与阅读应用页改造成职责明确、全中文、响应式的两个产品页面。

**Architecture:** 保留现有 Package API、路由、状态与事件绑定，在 `renderBookAgentPlatform` 中将非审计页面拆成包契约和阅读应用两个路由专属渲染器。共用中文策略/评测映射与既有检索、问答、证据组件；新增页面命名空间 CSS，避免影响 Agent 控制台和 v2 证据审计。

**Tech Stack:** 原生 JavaScript 模板渲染、CSS Grid、Node.js 源码契约 smoke、Go HTTP 服务。

---

### Task 1: 定义中文产品契约

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write the failing test**

新增断言，要求源码包含两个路由专属渲染器与中文标记：

```js
for (const marker of [
  "renderAgentPackageContract",
  "renderAgentReadingApp",
  "包契约总览",
  "阅读研究台",
  "推理模型",
  "关键词检索",
  "人工复核",
]) {
  assert.ok(js.includes(marker), `Chinese Agent pages should include ${marker}`);
}
for (const staleLabel of [
  "PACKAGE CONTRACT",
  "SHARED BOOK APP",
  ">Reader<",
  ">Grounded search<",
  ">Open the book<",
]) {
  assert.ok(!js.includes(staleLabel), `Chinese Agent pages should remove ${staleLabel}`);
}
```

同时要求 CSS 包含 `.package-contract`、`.reading-app`、折叠凭据和移动端单栏契约。

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL，缺少路由专属渲染器或中文标记。

**Step 3: Commit the red test**

```bash
git add frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "test(web): define Chinese package and reading pages"
```

### Task 2: 添加共用中文术语与页面渲染器

**Files:**
- Modify: `frontend-web/app.js:4820-5120`
- Test: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Add Chinese display helpers**

实现只影响展示的映射函数：

```js
function agentPolicyValueLabel(value) {
  return ({
    reasoning: "推理模型",
    lexical: "关键词检索",
    human_review: "人工复核",
    standard: "标准使用策略",
  })[value] || value || "未指定";
}
```

复用 `agentConsoleDisplayName` 和 `agentEvaluationMetricLabel`，技术原值仍保留在凭据区。

**Step 2: Implement `renderAgentPackageContract`**

渲染：

- 中文 Agent 名称与“包契约总览”标题；
- 中文路由导航；
- 版本、固定 Release、策略、评测四项摘要；
- 契约边界与中文评测指标双栏；
- 默认折叠的 Package hash、Release ID、内容哈希、评测套件和原始策略值。

不渲染检索、对话和证据账本交互，避免与阅读应用重复。

**Step 3: Implement `renderAgentReadingApp`**

渲染：

- “阅读研究台”和清理内部数字前缀后的书名；
- 中文路由导航和版本状态栏；
- 版本化阅读、包内检索、循证问答工作区；
- 引用/拒答边界侧栏；
- 证据账本与默认折叠的技术凭据。

复用现有 `renderBookAgentCapability`、`renderGroundedConversation`、
`renderBookAgentEvidence` 与事件表单 ID，保持数据流不变。

**Step 4: Route to the dedicated renderer**

在 `renderBookAgentPlatform` 中保留 v2 evidence-audit 和 Agent 控制台分支；对
`route.view === "package"` 与 `route.view === "app"` 分别调用新渲染器。

**Step 5: Run test to verify content passes**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: PASS。

**Step 6: Commit**

```bash
git add frontend-web/app.js
git commit -m "feat(web): localize package and reading pages"
```

### Task 3: 建立两个页面的响应式布局

**Files:**
- Modify: `frontend-web/styles.css:5090-5810`
- Test: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Extend the failing CSS contract**

要求页面命名空间包含：

```css
.package-contract__body { grid-template-columns: minmax(0, 1.35fr) minmax(300px, 0.65fr); }
.reading-app__body { grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr); }
.agent-page-technical code { overflow-wrap: anywhere; }
```

并在 `@media (max-width: 760px)` 下要求两个 body 均为单栏。

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: FAIL，缺少页面布局选择器。

**Step 3: Implement CSS**

沿用 Agent 控制台的墨绿/纸白/荧光绿变量，增加：

- 包契约的摘要、策略卡片、评测网格与折叠凭据；
- 阅读应用的主工作区、sticky 状态栏和证据账本；
- 所有长 ID 的 `overflow-wrap:anywhere`；
- 760px 以下单栏、状态栏前置、按钮和输入框满宽；
- `prefers-reduced-motion` 下取消新增动画。

**Step 4: Run test to verify it passes**

Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`

Expected: PASS。

**Step 5: Commit**

```bash
git add frontend-web/styles.css frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "feat(web): lay out Chinese package and reading pages"
```

### Task 4: 回归、隐私与浏览器验收

**Files:**
- Modify if required: `frontend-web/app.js`
- Modify if required: `frontend-web/styles.css`
- Modify if required: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Run all Web smoke tests**

Run:

```bash
set -e
for script in frontend-web/scripts/*smoke.mjs; do node "$script"; done
```

Expected: 全部 PASS。

**Step 2: Run Go and privacy checks**

Run:

```bash
go test ./... -timeout=300s
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: 全部退出 0。

**Step 3: Browser acceptance**

在本地静态站或发布候选中验证两个精确路由：

- 1366px：包契约与阅读应用各自分栏成立；
- 390px：单栏，`document.documentElement.scrollWidth === 390`；
- 两页没有旧英文产品标签；
- 阅读应用搜索与问答表单仍可提交；
- 技术凭据默认折叠，展开后 ID 完整可见；
- 浏览器控制台无应用错误。

**Step 4: Final commit if acceptance requires corrections**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "fix(web): finish Chinese Agent page acceptance"
```
