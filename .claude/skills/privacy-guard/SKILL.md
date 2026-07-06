---
name: privacy-guard
description: "Use when changing dedao-gui configuration, documentation, prompts, exports, commits, or GitHub publishing paths where local absolute paths, personal identifiers, private project names, API keys, cookies, tokens, downloaded content, or macOS metadata could leak."
---

# Privacy Guard — dedao-gui 发布前隐私闸

> 目标:公开仓库只能包含可复用的代码、文档和配置契约,不能泄漏本机环境、私人项目名、账号痕迹或个人下载内容。

## 适用场景

- 改默认路径、配置读取、导出路径、CLI 参数、README/docs/plans、prompt 模板、MCP/KBase/NotebookLM 资料包。
- 准备 commit、push、PR、发布桌面包。
- 看到本机绝对路径、私人项目代号、token/cookie、`.DS_Store`、下载样本内容进入 diff。

## 硬规则

1. **不写本机绝对路径**:默认值必须来自环境变量或相对路径。优先使用现有契约:
   - `DEDAO_TOKENPLAN_ENV_FILE`
   - `DEDAO_WIKI_REPO_DIR`
   - `DEDAO_BOOK_KNOWLEDGE_ROOT`
   - `KBASE_SYSTEM_KB_EXPORT_PATH`
   - `DEDAO_KBASE_ROOT`
2. **不默认读取私人 sibling repo 配置**:需要外部 API key 或知识库根目录时,从 env/config 显式传入;缺失时返回清晰错误,不要静默 fallback 到某台机器上的路径。
3. **不在 prompt/docs 里写私人项目名**:用角色化名称,例如“健康知识库”“量化研究项目”“通用 wiki”,不要把个人仓库名当产品文案。
4. **不提交本地产物和敏感材料**:`.DS_Store`、`config.json`、token/cookie、下载的书籍样本、`frontend/dist`、`build/bin` 都不进 git。
5. **私有词用本地 denylist**:需要拦截具体私人用户名或私有项目代号时,写入 `.privacy-denylist.local` 或 `DEDAO_PRIVACY_EXTRA_GREP_PATTERNS`,不要把真实词写进公共脚本/文档。
6. **不帮用户改历史**:如果泄漏已经进 Git 历史,只在用户明确要求后再做 history rewrite;普通修复只改当前树。

## 必跑验证

在 commit/push/PR 前运行:

```bash
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

如果改了 Go/Wails/前端相关代码,再按 `AGENTS.md` 跑对应窄测、`go test ./...`、`cd frontend && npm run build`。`wails build` 可能重写 `frontend/wailsjs/go/models.ts`;若只产生尾随空白,清理后重跑 `git diff --check`。

## 修复模式

| 问题 | 修复 |
|---|---|
| 硬编码本机路径 | 换成 env var;没有 env 时返回相对路径或明确错误 |
| 私人项目名出现在 prompt/docs | 换成角色化、领域化名称 |
| `.DS_Store` 已被跟踪 | `git rm --cached <path>` 并确认 `.gitignore` 覆盖 |
| 需要示例配置 | 提供 `.example` 或 README 变量说明,不要填真实值 |
| 隐私 smoke 红 | 先修红,不要绕过;只有误报才调整脚本,并说明原因 |

## 提交边界

只 stage 本次任务相关文件。若工作区已有其他未提交改动,保留并在最终说明里标出,不要 `git add -A`。
