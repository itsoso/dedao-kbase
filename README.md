# 得到课程下载桌面端

> wails + go + vue 构建的《得到》APP 课程下载桌面客户端

技术栈如下：

> 1. [wails快速入门](https://wails.io/zh-Hans/)
> 2. [Vue3.x](https://cn.vuejs.org/guide/introduction.html)
> 3. [Vue Router 4.x](https://router.vuejs.org/zh/introduction.html)
> 4. [vue3 element-plus](https://element-plus.gitee.io/zh-CN/)
> 5. [typeScript](https://www.typescriptlang.org/zh/docs/)
> 6. [Vite](https://cn.vitejs.dev/)
> 7. [pinia](https://pinia.vuejs.org/zh/)

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/yann0917/dedao-gui)
[![Go Report Card](https://goreportcard.com/badge/github.com/yann0917/dedao-gui)](https://goreportcard.com/report/github.com/yann0917/dedao-gui)

## 特别声明

仅供个人学习使用，请尊重版权，内容版权均为得到所有，请勿传播内容！！！

仅供个人学习使用，请尊重版权，内容版权均为得到所有，请勿传播内容！！！

仅供个人学习使用，请尊重版权，内容版权均为得到所有，请勿传播内容！！！

## 特性

* 展示首页内容
* 可扫码登录
* 可查看**购买**的课程，课程详情，课程文章列表，可播放课程音频
* 可查看听书书架列表，听书文稿，可播放每天听本书音频
* 可查看电子书架列表，电子书详情，书评，可加入书架
* 可查看已购买的锦囊
* 可查看知识城邦
* 课程可生成PDF，文稿生成 Markdown 文档，也可生成 mp3 文件
* 每天听本书可下载音频，文稿生成 pdf、 Markdown 文档
* 电子书可下载 pdf，html, epub 等格式

## 本 fork 更新点

> 以下能力主要面向个人书籍知识库、NotebookLM Bridge、本地 MCP 和跨项目知识复用。

### 2026-06-27

* 新增「书籍知识库」工作台：自动从已下载电子书 HTML 提取章节、chunks、claims、citations，并以本地 `book_knowledge` 目录保存。
* 新增电子书「下载并入 Wiki」入口：下载电子书 HTML 后可触发 `llms-wikis ingest-ebook` 与 `pipeline/compiler.py --changed-only`，将书籍重新抽取到 wiki 知识库。
* 新增书籍对话能力：接入阿里云 TokenPlan OpenAI-compatible API，默认模型为 `qwen3.7-max`，支持总结本书、分析本书、行动清单、规则卡和自由问答。
* 新增对话历史：每次书籍分析完成后写入本地 SQLite，可在书籍知识库中查看、恢复历史记录。
* 新增 Markdown 渲染视图：对话答案支持「渲染 / Markdown」切换，优化标题、列表、表格、引用块和代码块显示。
* 新增多书并行分析：按 `book_id` 管理分析 loading 状态，切换到其他书时可以继续发送新请求。
* 新增 NotebookLM Bridge：可导出 `book.md`、`claims.md`、`notebooklm-prompt.md` 资料包，一键打开 NotebookLM，并保存每本书对应的 NotebookLM 链接。
* 新增 MCP 能力：提供 `cmd/book-mcp` stdio server，可向其他大模型暴露书籍列表、检索、章节读取、导出等工具。
* 新增在线 kbase HTTP 服务：提供 `cmd/kbase-server`，可部署到 `kbase.executor.life`，用 Bearer token 向 health/proofroom 暴露书籍检索和 System KB export。
* 新增项目导出：支持导出为 `health_system_kb_v2` 健康知识库格式，以及 `quant_rule_cards` 量化规则卡草案。
* 优化登录二维码流程：在缺失或失效 CSRF token 时自动刷新首页状态并重试，降低扫码二维码加载失败概率。
* 优化书籍知识库 UI：新增专业化工作台布局、搜索、章节/claims/chunks/MCP/NotebookLM tabs 和历史记录侧栏。

### 架构概览

本 fork 在原有 Wails 桌面端上新增了一条本地书籍知识库链路。`frontend/src/views/BookKnowledge.vue` 是工作台入口，只负责书籍选择、搜索、对话、历史恢复、NotebookLM 操作和导出按钮；所有数据读写都通过 Wails 生成的 `frontend/wailsjs/go/backend/App.*` 调用后端。

后端边界集中在 `backend/book_knowledge.go`，它把前端可调用方法转发到 `backend/app` 中的领域模块：

```mermaid
flowchart LR
  UI[BookKnowledge.vue] --> Wails[Wails App bindings]
  Wails --> Facade[backend/book_knowledge.go]
  Facade --> Store[BookKnowledgeStore]
  Facade --> Chat[TokenPlan chat]
  Facade --> Export[Export / NotebookLM / MCP]
  Store --> Files[book_knowledge JSON + JSONL]
  Chat --> Store
  Chat --> History[SQLite chat history]
  Chat --> TokenPlan[OpenAI-compatible API]
  Export --> KBase[health_system_kb_v2]
  Export --> Quant[quant_rule_cards]
  Export --> NotebookLM[NotebookLM package]
```

核心数据模型定义在 `backend/app/book_knowledge.go`：一本书由 `BookKnowledgeBook`、`Chapter`、`Chunk`、`Claim`、`Citation` 组成，并保存到本地 `book_knowledge` 根目录。默认根目录可通过 `DEDAO_BOOK_KNOWLEDGE_ROOT` 覆盖；每本书有独立目录，结构包括 `manifest.json`、`chapters.jsonl`、`chunks.jsonl`、`claims.jsonl`、`citations.jsonl`。对话层位于 `backend/app/book_chat.go`，从本地知识包构造上下文，调用 TokenPlan OpenAI-compatible API，并把成功回答写入 `book_chat_history.sqlite3`。外部集成包括 `cmd/book-mcp` 的 stdio MCP 工具、`cmd/kbase-server` 的 Bearer token 私有 HTTP 检索服务，以及 NotebookLM Bridge 导出的 markdown 资料包和 notebook 链接。

### kbase HTTP 服务

本服务面向个人私有部署，API 路由必须配置 `KBASE_AUTH_TOKEN`。未配置 token 时，`/health` 仍可探活，但 `/api/*` 会拒绝访问。浏览器页面由 Nginx Basic Auth 保护；登录后 Web UI 会通过 `/browser/session-token` 自动换取同源 Bearer token 并写入浏览器本地存储。自动化客户端仍应直接使用 `Authorization: Bearer <KBASE_AUTH_TOKEN>` 调用 `/api/*`。

```bash
cd /opt/dedao-gui
KBASE_AUTH_TOKEN="replace-with-long-secret" \
KBASE_SOURCE_AGENT_TOKEN="replace-with-separate-agent-secret" \
KBASE_BOOK_KNOWLEDGE_ROOT="/opt/dedao-kbase/book_knowledge" \
KBASE_SYSTEM_KB_EXPORT_PATH="/opt/dedao-kbase/artifacts/system_kb_export.json" \
KBASE_REVERIFICATION_TICK_SECONDS="30" \
KBASE_REVERIFICATION_COOLDOWN_SECONDS="300" \
KBASE_REVERIFICATION_STALE_SECONDS="900" \
go run ./cmd/kbase-server --addr 127.0.0.1:8719
```

对外域名建议由 Nginx/Caddy/Cloudflare Tunnel 终止 TLS 后反代到本地端口：

- `GET /health`：无需 token，用于服务探活。
- `GET /api/books`：列出书籍知识包。
- `GET /api/search?q=关键词&limit=5`：检索书籍 chunks/claims。
- `GET /api/system-kb/manifest`：返回 System KB export 摘要。
- `GET /api/system-kb/export`：返回 health/proofroom 导入用的 `system_kb_export.json`。
- `POST /api/knowledge/releases/{release_id}/feedback`：接收消费者的结构化反馈；`stale`、`conflict`、`rejected` 会幂等创建异步复核任务。
- `GET /api/knowledge/releases/{release_id}/reverification`：查看该 release 的复核任务状态、候选分析哈希、质量裁决和枚举错误码。

复核任务由 `kbase-server` 后台处理，不依赖本地来源 Agent。任务会通过跨进程文件锁合并同一 release 的并发异常反馈，并以冷却时间限制模型调用频率；服务重启后会恢复超时的 `running` 任务，服务取消或分析期间内容变化则重新排队。复核只生成当前知识包的候选分析与质量报告，已有 release 保持不可变；显式发布时还会校验最新复核状态及候选哈希，未解决或已过期的候选不能发布。

### 微信/WC Plus 来源工作台

在线 Web UI 新增 `/wechat-source` 和 `/wcplus-source`。`/wcplus-source` 是来源控制面，可查看本地 Agent 心跳、WC Plus 健康状态、订阅和同步运行；旧的直连代理工具收在“本地 API 诊断”中。浏览器不会直接访问本机 WC Plus 端口。

桌面版 `/wcplus-source` 默认也使用同源 `/api/*`。如果 Wails 桌面壳没有和 kbase HTTP 服务同源运行，在页面顶部的 `KBase API Base URL` 填入 kbase 地址，例如 `http://127.0.0.1:8719`，并确保本机已写入 `KBASE_AUTH_TOKEN` 对应的 Bearer token。kbase 只允许 Wails/localhost/127.0.0.1 这类桌面来源跨域调用 `/api/*`，不会对任意网页开放 CORS。

推荐使用“本地 Agent + 在线 KBase 控制面”：`wcplus-agent` 只访问本机 loopback WC Plus API，并通过出站 HTTPS 租用同步任务、上传文章和回报计数。不要把 WC Plus 的 loopback API 通过公网隧道暴露，也不要把微信 cookie 或 WC Plus 请求参数上传到 KBase。

在线服务使用独立的 `KBASE_SOURCE_AGENT_TOKEN` 保护 `/api/source-agent/*`。该 token 必须与浏览器/API 使用的 `KBASE_AUTH_TOKEN` 分离，并使用不含空格的可打印 ASCII 字符。

本机 Agent 配置契约：

```bash
export KBASE_REMOTE_URL="https://kbase.example.invalid"
export KBASE_SOURCE_AGENT_ID="wcplus-agent-1"
export KBASE_SOURCE_AGENT_TOKEN="replace-with-source-agent-secret"
# 以 WC Plus 当前界面或启动日志显示的 API 端口为准；9.483 实测为 5002。
export WCPLUSPRO_BASE_URL="http://127.0.0.1:5002"
export WCPLUS_AGENT_STATE_DIR="./state/wcplus-agent"
```

构建、检查和安装用户级 LaunchAgent：

```bash
bash scripts/build-wcplus-agent-macos.sh --check
bash scripts/build-wcplus-agent-macos.sh
bash scripts/install-wcplus-agent-macos.sh --check
build/bin/wcplus-agent doctor
bash scripts/install-wcplus-agent-macos.sh
```

安装器会将相对状态路径解析为安装时的绝对路径，生成权限为 `600` 的 LaunchAgent plist，并只在进程异常退出时按受限间隔重启。可用 `WCPLUS_AGENT_LOG_DIR`、`WCPLUS_AGENT_INSTALL_DIR`、`WCPLUS_AGENT_PLIST_PATH`、`WCPLUS_AGENT_POLL_SECONDS` 和 `WCPLUS_AGENT_RESTART_SECONDS` 覆盖默认值。

卸载默认保留 SQLite outbox 和日志，避免未上传数据丢失：

```bash
bash scripts/uninstall-wcplus-agent-macos.sh
bash scripts/uninstall-wcplus-agent-macos.sh --delete-state --delete-logs
```

使用删除参数时，状态和日志目录必须通过对应环境变量提供绝对路径；这是防止从错误工作目录删除相对路径的保护措施。

Agent 优先读取 `WCPLUSPRO_BASE_URL`，也兼容 `WCPLUS_BASE_URL`。WC Plus 9.483 实测使用 `http://127.0.0.1:5002`，旧版和旧文档可能使用 `5001`；请以 WC Plus 当前界面或启动日志显示的端口为准。两者都未设置时，Agent 为兼容旧版仍回退到 `http://127.0.0.1:5001`。出于安全边界，Agent 会拒绝非 loopback WC Plus 地址。

旧的同机直连模式仍可用于诊断。WC Plus API 暂时不可达时，可在“本地 API 诊断”中使用手动导入，或选择 `.txt` / `.md` 文件填入正文后再导入。

常用代理接口：

- `GET /api/wcplus/env/check`：检查 WC Plus 服务和公众号列表 API。
- `GET /api/wcplus/gzh/list`、`GET /api/wcplus/gzh/articles`、`GET /api/wcplus/article/content`：加载公众号、文章和正文。
- `POST /api/wcplus/import/article`、`POST /api/wcplus/import/account`：导入单篇或一批文章到书籍知识库。
- `POST /api/wcplus/import/raw`：将粘贴的标题、公众号、原文链接和 Markdown/纯文本正文直接导入书籍知识库，不依赖 WC Plus API 联通性。
- `POST /api/wcplus/task/new`、`POST /api/wcplus/task/control`、`GET /api/wcplus/task/all`：创建、启动和查看下载任务。
- `GET /api/wcplus/search`、`GET /api/wcplus/article/search-title`、`GET /api/wcplus/search-gzh`：全文、标题和公众号候选检索。
- `GET /api/wcplus/report/reading-data`、`GET /api/wcplus/report/statistic-data`、`GET /api/wcplus/article/gzh`、`GET /api/wcplus/like-articles`、`GET /api/wcplus/request/gzh`：阅读/统计/公众号详情等辅助查询代理。
- `GET /api/wcplus/export/text`、`GET /api/wcplus/export/gzh-csv`、`POST /api/wcplus/export/all-articles-xlsx`：触发 TXT/CSV/XLSX 导出。
- `POST /api/wcplus/batch-import/gzh`：按公众号昵称批量创建 WC Plus 同步任务；可传 `import_to_kbase=true`、`wait_for_completion=true`、`import_limit`，在任务完成后直接导入书籍知识库。

### NotebookLM Bridge 使用方式

1. 在「电子书架」中下载并入 Wiki，或先下载电子书 HTML 后进入「书籍知识库」。
2. 在「书籍知识库」选择目标书籍，打开 `NotebookLM` tab。
3. 点击「导出资料包」，生成 `book.md`、`claims.md`、`notebooklm-prompt.md` 和 `upload-guide.md`。
4. 点击「打开 NotebookLM」，在 NotebookLM 中创建 notebook 并上传 `book.md`、`claims.md`。
5. 点击「复制上传指南」或打开 `upload-guide.md`，按步骤复制提示词到 NotebookLM。
6. 将 NotebookLM 页面链接保存回 dedao-gui，后续可从同一本书继续打开。

### 注：

1. 下载均在后台执行，下载完毕弹框会关闭，等待弹窗关闭或者点击确定下载后关闭，均会在后台执行下载程序。
2. 如果遇到 `496 NoCertificate` 消息提示，请登录网页版进行图形验证码验证。
3. 本应用上登录后再登录官方网页版会导致保存的 cookie 失效，使用 `rm -rf ~/.config/dedao/config.json` 删除配置信息后重新登陆本应用即可。

## 安装

构建请查看[wails 文档](https://wails.io/zh-Hans/docs/introduction)

1. `运行 go install github.com/wailsapp/wails/v2/cmd/wails@latest` 安装 Wails CLI。
2. clone 该项目，从项目目录，执行 `wails build`，即可构建二进制文件

### 安装依赖

wails 构建需要安装以下依赖：

* Go 1.21+
* NPM (Node 15+)

如果需要下载相应格式的内容，请按照下载需求，安装下列依赖：

#### pdf下载

* google chrome
  > 课程生成 PDF 需要借助 [Google-Chrome](https://www.google.cn/intl/zh-CN/chrome/)的渲染引擎
* wkhtmltopdf
  > 电子书转 PDF 需要借助[wkhtmltopdf](https://wkhtmltopdf.org/downloads.html)

#### 音频下载

* ffmpeg
  > 音频需要借助 [ffmpeg](https://ffmpeg.org/) 合成

### 功能截图如下：

![](image/Snipaste_2023-04-16_21-11-23.png)
![](image/Snipaste_2023-04-17_00-01-03.png)
![](image/Snipaste_2023-04-16_21-09-18.png)
![](image/Snipaste_2023-02-21_19-13-26.png)
![](image/Snipaste_2023-02-21_19-14-14.png)
![](image/Snipaste_2023-02-21_19-14-27.png)
![](image/Snipaste_2023-02-21_19-15-12.png)
![](image/Snipaste_2023-02-21_19-15-44.png)
![](image/Snipaste_2023-02-21_19-25-03.png)

## Stargazers over time

[![Stargazers over time](https://starchart.cc/yann0917/dedao-gui.svg)](https://starchart.cc/yann0917/dedao-gui)

## License

[MIT](./LICENSE) © yann0917

---
# First-party WeChat source agent

Build the native macOS agent with `scripts/build-source-agent-macos.sh`. Install
it with explicit `KBASE_REMOTE_URL`, `KBASE_SOURCE_AGENT_ID`, and a dedicated
`KBASE_SOURCE_AGENT_TOKEN` using `scripts/install-source-agent-macos.sh`. MP
sessions remain in macOS Keychain and enrollment listens on loopback only.
Production scheduling must remain disabled until the bounded G6 probe in the
collector delivery dossier passes.
