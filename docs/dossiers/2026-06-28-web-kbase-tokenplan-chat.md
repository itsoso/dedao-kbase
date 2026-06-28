---
slug: 2026-06-28-web-kbase-tokenplan-chat
status: online
current_stage: S8
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
| G3 测试 | PASS | `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`; `CGO_ENABLED=0 go test ./backend/app -run 'TestKBaseHTTPHandlerServesBookPromptsChatAndHistory' -count=1`; `node frontend-web/scripts/web-kbase-ui-smoke.mjs`; `cd frontend-web && npm run build`; `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server`; `git diff --check`;2026-06-28 |
| G4 评审 | PASS | HTTP chat route remains Bearer-protected;browser only receives KBase Bearer token;TokenPlan API Key stays server-side;2026-06-28 |
| G5 部署健康 | PASS | 已同步 `frontend-web/dist` 和 `kbase-server`;`nginx -t` 通过;`dedao-kbase.service` active;`https://kbase.executor.life/health` 200;2026-06-28 |
| G6 验证 | PASS | 线上 `/api/books?page=1&page_size=1`、`/prompts`、`/chat`、`/chat-history` 均通过;chat 返回 `answer_chars=478 sources=5 model=MiniMax-M2.5`;根路径保持 Basic Auth 401;2026-06-28 |

## Discovery 现状图(带 file:line)
- `backend/app/book_chat.go` 已实现 TokenPlan config、prompt/context 构造、chat completions 调用和响应解析。
- `backend/app/book_chat_history.go` 已实现本地 SQLite chat history。
- `backend/app/book_prompts.go` 已实现静态和动态书籍 Prompt 模板。
- `frontend/src/views/BookKnowledge.vue` 桌面端已有对话 tab、prompt 模板、历史记录、Markdown 渲染和来源列表。
- `frontend-web/src/App.vue` 已新增分页书籍栏、Prompt 模板、TokenPlan chat panel、sources 和 history 恢复。
- `backend/app/kbase_http.go` 已新增 `/api/books` 分页以及 prompts/chat/history 路由。
- `backend/app/book_chat_history.go` 已新增 `CGO_ENABLED=0` 时的 JSONL history fallback;线上 cross-compiled binary 已验证可保存 history。

## 待拍板决策(STOP 问人)
- 已拍板:先做非流式、单书、只读 Web TokenPlan 对话和分页书籍栏。

## 沉淀(S8)
- 线上入口: `https://kbase.executor.life/`。浏览器页面由 Nginx Basic Auth 保护,登录后通过 `/browser/session-token` 自动填充 `KBASE_AUTH_TOKEN`。
- TokenPlan secret 已写入 `/etc/dedao-kbase/kbase.env` 的 `DEDAO_TOKENPLAN_*`;验证只输出 present 状态,不记录密钥。
- `CGO_ENABLED=0` 部署需要 JSONL history fallback,否则 `go-sqlite3` stub 会导致 chat 500。
