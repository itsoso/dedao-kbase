---
slug: 2026-06-28-dedao-kbase-skills
status: building
current_stage: S5
last-reviewed: 2026-06-28
---

# Dossier: Dedao KBase Skills

## 用户原话需求(逐字)
> skills相关的规划也进入开发，开发测试之后上线

## 四问(feature-plan)
- **Q1 用户价值**:OpenClaw、Hermes、Reva、health、proofroom 等系统可以安装或发现 dedao-kbase 能力,按需检索书籍知识与 System KB export。
- **Q2 边界(What NOT)**:不开放匿名 invoke;不把 draft claim 直接提升为 health/Reva 运行时权威;不新增写操作、重抽取或删除能力。
- **Q3 最简实现**:在 `cmd/kbase-server` 内提供 public discovery + per-skill manifest/OpenAPI/SKILL.md,并让 `invoke` 复用现有 Bearer token。
- **Q4 风险**:私有书籍知识库暴露面扩大;必须保证 discovery 可公开但 invoke 401 fail-closed。

## ASCII 数据流

```text
Agent / downstream system
  -> public discovery: /.well-known/dedao-kbase-skills.json
  -> public skill docs: /api/skills/{skill}/...
  -> protected invoke with Authorization: Bearer <token>
  -> backend/app.kbaseHTTPHandler
  -> BookKnowledgeStore / system_kb_export.json
```

## 阶段产出物(链接)
| 阶段 | 产出 | 链接 |
|---|---|---|
| S1 Discovery | 现状图 | 本文件 Discovery 节 |
| S3 规划 | 实施计划 | `docs/plans/2026-06-28-dedao-kbase-skills.md` |
| S5 实现 | 分支 | `codex/web-kbase-ui-kbase` |

## Gate 裁决记录
| Gate | 裁决 | 依据 / 日期 |
|---|---|---|
| G1 准入 | PASS | 用户明确要求将 skills 规划进入开发并上线;2026-06-28 |
| G2 可行性 | PASS | 复用现有 HTTP server、BookKnowledgeStore、Bearer token,不引入新认证系统;2026-06-28 |
| G3 测试 | IN PROGRESS | 新增 TDD 覆盖 public discovery、protected invoke、authenticated invocation |
| G4 评审 | IN PROGRESS | 需要确认 public descriptor 不泄漏 token,invoke 无 token 401 |
| G5 部署健康 | 待跑 | |
| G6 验证 | 待线上验证 | |

## Discovery 现状图(带 file:line)
- `backend/app/kbase_http.go` 已有 `/health`、`/api/books`、`/api/search`、`/api/system-kb/*`,且 `/api/*` 统一 Bearer 鉴权。
- `backend/app/book_mcp.go` 已有本地 stdio MCP 工具: list/search/get_chapter/get_context,可复用工具命名与输入概念。
- `README.md` 已声明 kbase 可给 health/proofroom 暴露检索和 System KB export。
- 线上 Nginx 当前将 `/api/*` 反代到 `127.0.0.1:8719`,浏览器页面由 `/var/www/kbase.executor.life` 托管。

## 待拍板决策(STOP 问人)
- 已拍板:先开放 read-only skill discovery + protected invoke。

## 沉淀(S8)
- 待完成后记录线上 discovery URL、验证命令和部署状态。
