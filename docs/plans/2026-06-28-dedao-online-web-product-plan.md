# Dedao Online Web Product Plan

## 结论

桌面版可以演进为在线 Web 版，但不应简单把 Wails UI 全量搬到浏览器。更稳妥的产品形态是 **Dedao Private Learning Workbench**：在线 Web 承接学习、检索、知识库、AI 对话、导出和跨系统调用；桌面/后台 Worker 承接登录、下载、媒体转码、PDF/EPUB 生成和本地文件处理。

原因：

- 已上线的 `cmd/kbase-server` 和 `frontend-web` 证明在线私有部署可行。
- 书籍知识库、检索、TokenPlan 对话、history、System KB、skills 已经有服务端 API。
- 桌面版大量能力依赖本机 cookie、Chrome、ffmpeg、wkhtmltopdf、文件系统和长任务，不适合直接放到同步 HTTP 页面里执行。
- 内容版权边界要求产品保持个人私有部署，不能做公开内容分发平台。

## 产品定位

面向个人学习和跨项目知识复用的私有在线工作台。

核心价值：

- 随时访问个人书籍知识库，不依赖打开桌面 App。
- 围绕一本书学习：检索、提问、总结、行动化、规则化、复习。
- 将得到内容抽取成可被 health、Reva、proofroom、NotebookLM、MCP/skills 调用的结构化知识。
- 把下载、抽取、转码、导出等重任务从 UI 操作变成可观测的后台任务。

非目标：

- 不做公开多用户内容平台。
- 不绕过得到账号、版权、访问限制。
- 不把 draft claim 直接作为 health/Reva 运行时权威知识。
- 不把 TokenPlan 或 Dedao cookie 暴露到浏览器。

## 当前能力基线

已在线：

- `frontend-web`: 浏览器 KBase Workbench。
- `cmd/kbase-server`: Bearer-protected `/api/*`。
- 书籍分页、检索、详情、Prompt、TokenPlan 单书对话、Markdown 渲染、history。
- System KB manifest/export。
- Agent skills discovery 和 Bearer-protected invoke。
- Basic Auth 后自动填充 `KBASE_AUTH_TOKEN`。

桌面仍独占：

- 扫码登录、cookie 管理、账号切换。
- 首页、课程、听书、电子书架、锦囊、知识城邦。
- 课程文章、音视频播放、下载弹窗。
- PDF、Markdown、MP3、HTML、EPUB 等生成。
- 电子书「下载并入 Wiki」和 NotebookLM Bridge 的完整操作面。
- 本机路径、Chrome、ffmpeg、wkhtmltopdf 依赖。

## 目标用户与场景

### 场景 1: 在线学习

用户打开 `kbase.executor.life`，选择一本书，左侧找书/检索，中间与大模型对话，右侧看章节、claims、chunks。目标是学习和复习，而不是管理下载流程。

### 场景 2: 内容入库

用户在 Web 上查看待处理书籍、触发下载/抽取任务、查看任务日志和产物状态。后台 Worker 执行长任务，Web 只发起和观察。

### 场景 3: 跨系统复用

health/Reva/proofroom 通过 skills 或 `/api/*` 调用 dedao-kbase。返回内容默认是 source/draft，需要目标系统自己的 review gate。

### 场景 4: 个人资料库管理

用户管理已抽取书籍、NotebookLM 链接、导出包、System KB 编译状态、任务失败重试。

## 产品模块规划

### A. Learning Workbench

优先级最高。当前 Web 已接近 MVP。

能力：

- 书籍分页、过滤、排序。
- 当前书/全库检索。
- 大模型问答、总结、分析、行动、规则卡。
- Prompt 模板和本书专属 Prompt。
- Markdown answer 渲染。
- sources 和 context stats。
- history 恢复。
- 右侧参考详情。

下一步增强：

- 多轮对话线程，不只保存单轮 history。
- 引用点击定位到 chapter/chunk/claim。
- 学习卡片、测验题、复习计划。
- 跨书比较和主题 notebook。

### B. Library & Ingestion Center

把桌面「电子书架 / 下载并入 Wiki」迁到在线任务中心。

能力：

- 列出得到电子书架和本地已入库书籍。
- 显示状态：未下载、已下载 HTML、已抽取、已编译 System KB、已导出 NotebookLM。
- 触发任务：下载 HTML、抽取 book_knowledge、编译 System KB、导出 NotebookLM 包。
- 任务日志、失败原因、重试。

实现原则：

- Web 只发起任务，不直接做文件处理。
- 后台 Worker 持有 Dedao cookie 和本地工具依赖。
- 所有任务写入 job store，UI 轮询或 SSE 查看状态。

### C. Dedao Account & Content Browser

迁移桌面「首页 / 课程 / 听书 / 电子书架 / 锦囊 / 知识城邦」中的只读浏览能力。

能力：

- 私有登录状态检查。
- 电子书、课程、听书列表。
- 详情页、章节列表、文稿阅读。
- 音频播放链接代理。
- 加入/移出书架等账号动作。

边界：

- 不默认开放公网匿名访问。
- Dedao cookie 只在服务端保存。
- 账号写动作需要二次确认和审计日志。

### D. Export & Interop

承接桌面导出能力和跨系统调用。

能力：

- NotebookLM export package。
- health_system_kb_v2 export。
- quant_rule_cards export。
- MCP/skills 描述和 invoke。
- Proofroom evidence package。

增强：

- 导出历史和版本。
- Export diff。
- 目标系统导入状态回写。

### E. Admin & Ops

在线化后必须补齐运维界面。

能力：

- 服务健康、存储路径、System KB version。
- TokenPlan config present 状态。
- Dedao login status。
- Worker queue。
- 最近错误、任务日志、磁盘占用。
- 备份/恢复入口。

## 推荐路线

### Phase 0: KBase Web 完整化

目标：把当前 Web 从「可用」打磨成学习主入口。

交付：

- 多轮对话线程。
- 引用点击定位。
- Prompt Studio Web 版。
- 学习卡片/测验题。
- NotebookLM Bridge Web 入口。
- 基础 Admin 状态页。

验收：

- 不打开桌面 App，也能完成一本书的学习、提问、复习、导出。

### Phase 1: Ingestion Web 化

目标：迁移「电子书下载并入 Wiki」链路。

交付：

- Job API: create/list/get/cancel/retry。
- Worker: download ebook HTML、extract book_knowledge、compile System KB。
- Web 任务中心。
- 失败可观测，不吞错误。

验收：

- Web 上能选择一本已购买电子书，触发入库，最终在 Learning Workbench 可对话。

### Phase 2: Dedao Content Browser

目标：迁移桌面主导航中读多写少的浏览能力。

交付：

- 登录状态页。
- 课程/听书/电子书架列表。
- 详情页和章节页。
- 文稿阅读。
- 下载任务入口复用 Phase 1 job system。

验收：

- 日常浏览和选择内容不再依赖桌面 UI。

### Phase 3: Media & Export Online

目标：迁移 PDF/Markdown/MP3/EPUB 等产物生成。

交付：

- 统一 Export Job。
- PDF/EPUB/Markdown/MP3 产物管理。
- 产物下载链接和保留策略。
- ffmpeg/Chrome/wkhtmltopdf 依赖容器化或固定部署环境。

验收：

- Web 发起导出，后台生成产物，UI 可查看日志和下载产物。

### Phase 4: Full Private Web Console

目标：桌面版能力在线闭环。

交付：

- 账号切换。
- 设置中心。
- 用户中心。
- 任务审计。
- 备份恢复。
- Web-only 操作手册。

验收：

- 桌面端退化为本地辅助工具或备用入口；主流程在私有 Web 完成。

## 技术架构

```text
Browser Web
  -> Nginx Basic Auth / optional SSO
  -> kbase-server / dedao-web-server
  -> service APIs
  -> job queue
  -> worker processes
  -> book_knowledge / artifacts / media outputs
  -> external services: Dedao, TokenPlan, NotebookLM, downstream skills callers
```

关键拆分：

- `frontend-web`: 主 Web UI。
- `cmd/kbase-server`: 现有 KBase API，可继续扩展或拆出 `cmd/dedao-web-server`。
- `backend/app`: 领域能力复用层。
- `backend/services`: Dedao API wrappers。
- `job store`: SQLite 起步，后续可迁 Postgres。
- `worker`: 负责下载、抽取、转码、导出等长任务。

## 安全与合规边界

- 所有内容访问默认私有部署。
- `/health` 可公开；业务 API 需要 Bearer 或更强认证。
- 浏览器不接触 Dedao cookie、TokenPlan API Key、服务器文件绝对路径的敏感部分。
- 下载产物需要短期签名链接或受保护路由。
- 导入 health/Reva 的内容必须保持 draft/review gate。
- 账号写动作、批量下载、删除产物需要确认和审计。

## 关键风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| 版权与内容分发边界 | 高 | 个人私有部署;不做公开分享;产物路由受保护 |
| Dedao 登录/cookie 在线化 | 高 | 服务端保存;加密存储;不下发浏览器;支持重新登录 |
| 长任务阻塞 HTTP | 高 | Job queue + Worker;UI 轮询/SSE |
| Chrome/ffmpeg/wkhtmltopdf 部署漂移 | 中 | 容器化或固定服务器依赖检查 |
| Web 面积变大导致安全面扩大 | 高 | 最小公开路由;鉴权中间件;操作审计 |
| 复制桌面 UI 导致产品臃肿 | 中 | 以学习工作流重组，而不是照搬菜单 |

## MVP 定义

最推荐的 MVP 不是完整桌面复制，而是：

1. Learning Workbench 完整学习闭环。
2. 电子书入库任务中心。
3. NotebookLM/System KB/skills 导出闭环。
4. Admin 状态页。

此时用户可以不打开桌面 App 完成核心价值：选书、入库、学习、对话、导出、给其他系统调用。

## 成功指标

- 一本文档从 Web 触发入库到可对话的成功率。
- TokenPlan 对话答案的可追溯来源覆盖率。
- Web 端完成学习任务的平均步骤数。
- 任务失败可诊断率：失败必须有明确 error 和日志。
- 桌面端依赖下降：核心学习流程不再需要 Wails。

## 下一步建议

1. 先立项 Phase 0 + Phase 1，不启动全量桌面复制。
2. 为 Phase 1 写 PRD 和 Dossier：`docs/dossiers/<date>-web-ingestion-center.md`。
3. 先补 `Job` 抽象和任务状态页，再迁移下载/抽取链路。
4. 将当前 `cmd/kbase-server` 的 API 分组整理为 `/api/kbase/*`、`/api/jobs/*`、`/api/admin/*`，避免未来路由膨胀。
5. 保留桌面版作为本地 fallback，直到 Web 下载、抽取、导出稳定。
