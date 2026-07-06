# CLAUDE.md

本文件给 Claude Code(claude.ai/code)在本仓库工作时的指南。

## Doc map — 读对文件

Claude Code 读本文件;**硬规则(build/test 命令、gofmt、命名、commit/PR、安全)的单一真源是 [`AGENTS.md`](AGENTS.md)** —— 本文件**不重述** AGENTS.md,只指路 + 加研发流程层。

| 我要做的事 | 读这个 |
|---|---|
| **系统全景 / 这系统是什么 / 有哪些能力 / 架构 / onboard 本项目** | **`docs/system-map/INDEX.md`**(若尚未建,用 `system-map` skill 起;agent 开工先读) |
| **把一句需求走完整流程**(需求→PRD→规划→研发→测试→构建→验证) | **`product-pipeline` skill**(双环 + 6 道 Gate + Dossier) |
| build / test / 运行 / gofmt / 命名 / commit / PR / 安全 硬约束 | **`AGENTS.md`**(权威,别在别处重写) |
| 当前在做的功能(在途) | `docs/dossiers/`(由 product-pipeline 写) |
| 已有功能设计文档 | `docs/plans/`(book-knowledge workbench / prompt-studio / ebook-wiki-sync 等) |
| 跨项目通用实践(四问 / 流水线契约 / 透明化契约) | `~/work/personal/PRACTICES/`(全局) |

## 项目概览

**得到课程下载桌面客户端** —— Wails 桌面 App:**Go 拥有后端与桌面壳,Vue3/Vite/TS/Pinia/element-plus 拥有 UI**。

- `main.go` 接线 Wails,内嵌 `frontend/dist`,绑定 `backend.App`。
- `backend/`:对外方法在 `app/`,API 封装在 `services/`,辅助在 `utils/`,请求/下载在 `request/`+`downloader/`,配置在 `config/`。
- `frontend/src/`:`views/`、`components/`、`stores/`(Pinia)、`router/`、`utils/`、`assets/`。
- **生成产物别手改/别提交**:`frontend/wailsjs/`(Wails 绑定,改 Go 绑定后重生成)、`frontend/dist`、`build/bin`、`config.json`、`.DS_Store`。
- 额外 CLI:`cmd/kbase-server`(`/api/*` 需 `KBASE_AUTH_TOKEN`,`/health` 故意不鉴权)、`cmd/book-mcp`(MCP server)。

能力面:首页/扫码登录/已购课程详情与音频/听书书架与文稿/电子书架与详情/锦囊/知识城邦;课程→PDF、文稿→Markdown、音频→MP3 等导出。**版权红线:仅个人学习用,内容版权归得到,勿传播。**

## 研发流程 skill(从成熟研发流程移植 + 适配)

`.claude/skills/` 下两个**研发流程**skill,是本项目开发的方法论骨架(已重新绑定到 Go/Wails/Vue 实况,不是其他项目的直接照搬):

- **`product-pipeline`** — 产品全生命周期总指挥。**触发**:「我想要 X」「加一个功能」「把这个需求走一遍流程」「从需求到上线」。它用 **6 道能失败能 STOP 的 Gate** 串起需求→PRD→规划→研发→测试→构建→验证,**Dossier**(`docs/dossiers/`)作可恢复脊柱。单文件小修/纯文档不必用它。
- **`system-map`** — 系统透明化层。**触发**:「这系统是什么/有哪些能力/架构/系统现状」。维护 `docs/system-map/INDEX.md`(agent 一遍读懂)+ **代码派生计数防漂移**(计数只准从 `docs/_generated/system-map.json` 引,绝不手打进叙事)。

两者闭环:product-pipeline 的 S1 读 system-map、S8 回写;system-map 的漂移闸挂在 `go test ./...`(本项目无 CI)。

## 几条贯穿纪律(其余去 AGENTS.md)

1. **测试绝不 `| tail`** —— tail 永远 exit 0,会吞掉 `go test`/`vue-tsc` 的失败 → 带红上线。直读结果行或 `set -o pipefail`。(AGENTS.md 已立此规,这是头号坑,重申。)
2. **改 Go 绑定 → 重生成 `frontend/wailsjs/`**(`wails dev` 或 `wails generate module`),别手改生成文件;前后端契约(`backend.App.Xxx` 签名 ↔ Vue 调用)对齐。
3. **长构建异步**:`wails build --clean` 触发后切别的活,别串行干等。
4. **Gate 不跳**:再小的改动,G3 测试闸(`go test ./...` + `cd frontend && npm run build`)与 G4 评审(碰鉴权/下载/版权时)不可省。
5. **先 grep 复用 > 新建**:dedao 客户端已有大量 service/store/组件,连接优先。
6. **从干净 `origin/main` 起分支**;`git fetch` + 看 PR 防并发抢先。

## 常用命令(详见 AGENTS.md)

```bash
wails dev                              # 跑桌面 App(Vite dev server)
wails build --clean                    # 构建 + 内嵌前端
go test ./...                          # Go 单测(先跑窄测再全量;别 | tail)
cd frontend && npm run build           # vue-tsc --noEmit 类型闸 + Vite build
node frontend/scripts/markdown-render-smoke.mjs   # 前端 smoke
node frontend/scripts/book-knowledge-ui-smoke.mjs
```
