---
slug: 2026-06-28-web-kbase-tokenplan-chat
status: planning
current_stage: S3
last-reviewed: 2026-06-28
---

# Dossier: Web KBase TokenPlan Chat

## 用户原话需求(逐字)
> web上也要具备跟Token Plan中的大模型对话的能力，左侧书籍要分页，具备GUI相关的能力，给出你的规划。

## 四问(feature-plan)
- **Q1 用户价值**:浏览器版 KBase 可以像桌面书籍知识库一样分页选书、用 TokenPlan 分析书籍、恢复历史记录。
- **Q2 边界(What NOT)**:不做匿名聊天、不把 TokenPlan API Key 下发前端、不做 streaming、多书 notebook 或自动导入 health/proofroom。
- **Q3 最简实现**:kbase HTTP 新增分页、prompts、chat、history API; `frontend-web` 增加分页书籍栏和 chat panel。
- **Q4 风险**:TokenPlan 调用会产生外部请求和成本;Web 聊天输出只能作为 draft/source,不能绕过 health/Reva review gate。

## ASCII 数据流

```text
Browser Workbench
  -> Bearer-protected kbase HTTP API
  -> BookKnowledgeStore + BookKnowledgeChat
  -> TokenPlan OpenAI-compatible chat completions
  -> answer + sources + context stats + local chat history
```

## 阶段产出物(链接)
| 阶段 | 产出 | 链接 |
|---|---|---|
| S1 Discovery | 现状图 | 本文件 Discovery 节 |
| S2 PRD | 设计文档 | `docs/plans/2026-06-28-web-kbase-tokenplan-chat-design.md` |
| S3 规划 | 实施计划 | `docs/plans/2026-06-28-web-kbase-tokenplan-chat.md` |

## Gate 裁决记录
| Gate | 裁决 | 依据 / 日期 |
|---|---|---|
| G1 准入 | PASS | 用户明确要 Web 版 TokenPlan 对话、书籍分页和 GUI 能力;2026-06-28 |
| G2 可行性 | PASS | 桌面端已有 `backend/app/book_chat.go`、Prompt、history;Web 只需加 HTTP facade 和 UI;2026-06-28 |
| G3 测试 | 待跑 | |
| G4 评审 | 待跑 | 涉及 Bearer 鉴权、TokenPlan 外部请求和 secret 边界 |
| G5 部署健康 | 待跑 | |
| G6 验证 | 待线上验证 | |

## Discovery 现状图(带 file:line)
- `backend/app/book_chat.go` 已实现 TokenPlan config、prompt/context 构造、chat completions 调用和响应解析。
- `backend/app/book_chat_history.go` 已实现本地 SQLite chat history。
- `backend/app/book_prompts.go` 已实现静态和动态书籍 Prompt 模板。
- `frontend/src/views/BookKnowledge.vue` 桌面端已有对话 tab、prompt 模板、历史记录、Markdown 渲染和来源列表。
- `frontend-web/src/App.vue` 目前只有书籍列表、搜索、详情和 System KB,还没有 chat panel 或分页。
- `backend/app/kbase_http.go` 当前 `/api/books` 一次性返回全部书籍,并缺少 prompts/chat/history 路由。

## 待拍板决策(STOP 问人)
- 已拍板:先做非流式、单书、只读 Web TokenPlan 对话和分页书籍栏。

## 沉淀(S8)
- 待上线后记录验证命令、线上 URL 和 TokenPlan secret 边界。
