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
* 新增 kbase Web UI：提供独立浏览器页面，可用同一个 Bearer token 浏览书籍、分页选书、检索 chunks/claims、调用 TokenPlan 进行单书对话，并查看 System KB 导出与线上任务。
* 新增项目导出：支持导出为 `health_system_kb_v2` 健康知识库格式，以及 `quant_rule_cards` 量化规则卡草案。
* 新增 Health Authority Pack：为 health-llm-driven 导出 `health_authority_pack_v1` 审核包，保留书籍、章节、claim、citation 和 source hash，并阻断诊断、治疗、剂量、用药调整和急救指导用途。
* 优化登录二维码流程：在缺失或失效 CSRF token 时自动刷新首页状态并重试，降低扫码二维码加载失败概率。
* 优化书籍知识库 UI：新增专业化工作台布局、搜索、章节/claims/chunks/MCP/NotebookLM tabs 和历史记录侧栏。

### kbase HTTP 服务

本服务面向个人私有部署，API 路由必须配置 `KBASE_AUTH_TOKEN`。未配置 token 时，`/health` 仍可探活，但 `/api/*` 会拒绝访问。

```bash
cd /opt/dedao-gui
KBASE_AUTH_TOKEN="replace-with-long-secret" \
KBASE_BOOK_KNOWLEDGE_ROOT="/opt/dedao-kbase/book_knowledge" \
DEDAO_DOWNLOAD_ROOT="/opt/dedao-kbase/downloads" \
KBASE_SYSTEM_KB_EXPORT_PATH="/opt/dedao-kbase/artifacts/system_kb_export.json" \
go run ./cmd/kbase-server --addr 127.0.0.1:8719
```

对外域名建议由 Nginx/Caddy/Cloudflare Tunnel 终止 TLS 后反代到本地端口：

- `GET /health`：无需 token，用于服务探活。
- `GET /api/books?page=1&page_size=30&q=关键词&sort=updated_at_desc`：分页列出书籍知识包。
- `GET /api/books/{book_id}/prompts`：读取书籍 Prompt 模板。
- `POST /api/books/{book_id}/chat`：服务端调用 TokenPlan 进行单书对话，返回 answer、sources 和 context stats。
- `GET /api/books/{book_id}/chat-history?limit=20`：读取本地 SQLite 对话历史。
- `GET /api/dedao/session`：读取安全登录摘要，只返回登录状态、当前用户安全字段和账号数量。
- `POST /api/dedao/auth/qrcode`：生成得到扫码登录二维码和轮询 token，不返回 Cookie。
- `POST /api/dedao/auth/check`：轮询扫码登录状态，成功后服务端保存登录配置，只返回状态、安全用户字段和 session 摘要。
- `GET /api/dedao/courses?page=1&page_size=15&q=关键词`：分页读取已购课程，只返回浏览所需字段，不返回 Cookie、DRM token 或原始跳转 URL。
- `GET /api/dedao/courses/{enid}`：读取课程详情和首批文章目录，只返回课程学习页需要的安全字段。
- `GET /api/dedao/courses/{enid}/articles?count=30&max_id=0`：分页读取课程文章目录。
- `GET /api/dedao/articles/{article_enid}?type=course`：读取课程文章并转换为 Markdown，供 Web 阅读页渲染。
- `GET /api/dedao/ebooks?page=1&page_size=15&q=关键词`：分页读取已购电子书书架，只返回浏览所需字段，不返回 Cookie 或 DRM token。
- `GET /api/dedao/ebooks/{enid}`：读取电子书详情和目录，只返回阅读页需要的安全字段。
- `GET /api/dedao/ebooks/{enid}/chapters/{chapter_id}/pages?index=0&count=8&offset=0`：读取单章节受限页数的解密 SVG 页面；服务端使用阅读 token，但不会把 token 下发给浏览器。
- `GET /api/jobs?limit=30`：读取线上任务列表。
- `POST /api/jobs`：为当前书籍或电子书创建后台任务，例如 `{"type":"notebooklm_export","book_id":"..."}`、`{"type":"book_export","book_id":"...","target":"health_system_kb_v2"}`、`{"type":"dedao_ebook_download","ebook_id":67929,"ebook_enid":"...","download_type":1}` 或 `{"type":"dedao_ebook_sync_kbase","ebook_id":67929,"ebook_enid":"..."}`。
- `GET /api/jobs/{job_id}`：读取任务状态、错误和导出结果。
- `GET /api/search?q=关键词&limit=5`：检索书籍 chunks/claims。
- `POST /api/projects/health/authority-pack/refresh?limit=25`：生成 `health_authority_pack_v1` 审核包，只面向 health 项目。
- `GET /api/projects/health/authority-pack/export?format=jsonl`：导出 Health Authority Pack JSONL，供 health-llm-driven dry-run import。
- `GET /api/system-kb/manifest`：返回 System KB export 摘要。
- `GET /api/system-kb/export`：返回 health/proofroom 导入用的 `system_kb_export.json`。

#### kbase Web UI

浏览器页面由 `cmd/kbase-server` 托管，仍然复用同一个 `KBASE_AUTH_TOKEN` 访问 `/api/*`：

```bash
cd frontend-web
npm install
npm run build

cd ..
KBASE_AUTH_TOKEN="replace-with-long-secret" \
KBASE_BOOK_KNOWLEDGE_ROOT="/opt/dedao-kbase/book_knowledge" \
DEDAO_DOWNLOAD_ROOT="/opt/dedao-kbase/downloads" \
KBASE_SYSTEM_KB_EXPORT_PATH="/opt/dedao-kbase/artifacts/system_kb_export.json" \
KBASE_WEB_DIR="$PWD/frontend-web/dist" \
go run ./cmd/kbase-server --addr 127.0.0.1:8719
```

打开 `http://127.0.0.1:8719/`，在页面顶部填写服务地址和同一个 token 后即可连接。页面提供全局导航，可切到书库、学习、课程、电子书架、登录、个人中心、任务、System KB、Skills/API 和运维状态。`/user/login` 支持得到扫码登录，轮询成功后由服务端保存 Cookie；`/user/profile` 只展示安全登录摘要；`/course` 提供只读课程列表、关键词筛选和分页，点击课程进入 `/course/{enid}` 后可查看课程详情、文章目录并阅读 Markdown 文章；`/ebook` 提供电子书架、关键词筛选和分页，点击电子书进入 `/ebook/{enid}` 后可查看书籍详情、目录并阅读受限批次 SVG 页面；书架行内可选择 HTML/PDF/EPUB 并创建下载任务，也可创建“加入书籍知识库”任务。左侧支持分页和书名筛选，中栏提供检索与 TokenPlan 对话，右侧保留章节、claims、chunks、Jobs、System KB、Skills/API 和 Ops 详情。Jobs 面板可为当前书籍创建 NotebookLM、`health_system_kb_v2`、`quant_rule_cards` 导出任务并查看状态。线上部署可通过 Nginx Basic Auth 保护浏览器页面,并把 `/browser/session-token` 精确路由到 kbase-server;Basic Auth 通过后页面会自动填充 Bearer token。TokenPlan API Key、Dedao Cookie 和电子书阅读 token 只读取服务端环境/配置，不会下发到浏览器。

下载任务默认写入 `DEDAO_DOWNLOAD_ROOT`；未配置时依次使用 `DEDAO_KBASE_DOWNLOAD_ROOT`、`DEDAO_KBASE_ROOT/downloads`、`KBASE_BOOK_KNOWLEDGE_ROOT` 的同级 `downloads`。不要把生产下载目录配置为本机个人路径。

每本书籍入库时会生成 `quality_report.json`，并在书籍 manifest 中写入 `quality_status`、`quality_score` 和更新时间。`usable` 可进入项目验证，`needs_review` 只能作为辅助材料，`rejected` 会被 health/proofroom 项目集合过滤。

Health Authority Pack 是给 health-llm-driven 的审核输入，不是直接运行时知识。Dedao-only claim 只能作为健康教育、问题准备和上下文检索候选；涉及诊断、治疗、剂量、用药调整或急救指导的记录必须由 health 侧 review gate 继续阻断或人工审核。

#### kbase Agent Skills

`cmd/kbase-server` 也提供给 OpenClaw、Hermes、Reva、health、proofroom 等系统发现和安装用的 skills 描述。Discovery 文档可公开读取,真正调用仍需同一个 Bearer token:

- `GET /.well-known/dedao-kbase-skills.json`
- `GET /api/skills`
- `GET /api/skills/dedao.book.search/manifest.json`
- `GET /api/skills/dedao.book.search/openapi.json`
- `GET /api/skills/dedao.book.search/SKILL.md`
- `POST /api/skills/dedao.book.search/invoke`

示例:

```bash
export DEDAO_KBASE_BASE_URL="https://kbase.executor.life"
export DEDAO_KBASE_TOKEN="replace-with-long-secret"

curl "$DEDAO_KBASE_BASE_URL/.well-known/dedao-kbase-skills.json"

curl -H "Authorization: Bearer $DEDAO_KBASE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"arguments":{"query":"强化学习","limit":5}}' \
  "$DEDAO_KBASE_BASE_URL/api/skills/dedao.book.search/invoke"
```

首批只读 skills:

- `dedao.book.search`:检索书籍 chunks/claims。
- `dedao.book.get_context`:按 `book_id` 读取书籍上下文。
- `dedao.system_kb.manifest`:读取 System KB export 摘要。
- `dedao.system_kb.export`:读取完整 System KB export。

health/Reva 不应直接把 dedao 返回的 draft claim 当运行时权威;应先导入自身知识库并通过 review gate。proofroom 可直接把结果作为证据线索和溯源材料。

Web 对话历史默认写入 SQLite；当 `kbase-server` 以 `CGO_ENABLED=0` 构建、`go-sqlite3` 不可用时，会显式写入同目录 `book_chat_history.jsonl`，保证 cross-compiled 线上部署仍可保存和读取 history。

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
