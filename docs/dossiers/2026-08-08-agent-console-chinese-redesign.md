# Agent 控制台中文化重设计 Dossier

## 状态

已交付并通过 G1-G6。生产部署代码版本为
`e3f75a91f28ff6a6fed47fc40fab33b67ffcdaad`。

## 需求

将已发布 Book Agent 的 Agent 页面改造成中文优先、布局合理的专业控制台。
Package、Book App、证据审计 v2、鉴权、共享 Token、运行时 API 与不可变 Agent
Package 均保持不变。

## G1 定义 Gate

裁决：PASS。

- 用户选择“专业控制台型”并确认完整设计。
- 目标路由是 v1 Agent 页面；桌面与手机响应式均在范围内。
- 后端、发布协议、模型与检索算法明确排除。

## G2 可行性与风险 Gate

裁决：PASS。

- 现有页面使用服务端数据驱动的 JavaScript 模板和独立 CSS，可在不改 API 的情况下重排。
- 检索与对话已有稳定表单 ID、状态字段和事件绑定，可直接复用。
- v2 证据审计已有独立分支，新布局仅接管 v1 Agent 路由，降低回归风险。
- 主工作区存在未提交修改；实现使用独立 worktree，并只精确提交本任务文件。

## G3 测试 Gate

裁决：PASS。

- 新增源级 UI 契约测试并观察到预期 RED：专用控制台、中文状态、响应式布局、
  精简中文标题、中文证据账本和新静态资源版本在实现前均能被测试拦截。
- 实现后 `agent-console-ui-smoke.mjs`、证据审计相邻测试、Book Knowledge 相邻测试
  和 JavaScript 语法检查通过。
- 全部 17 个生产 Web smoke 通过。
- 桌面前端 `vue-tsc --noEmit` 与 Vite 生产构建通过。Vite 仍报告仓库既有的
  500 kB chunk 提示；主入口 981.50 kB，未超过现有 bundle gate。
- 在最终代码上执行 `go test ./... -timeout=300s` 通过；最慢的 `backend/app`
  首次非缓存运行耗时 98.674 秒。
- system-map 漂移检查、隐私检查、空白检查和干净工作区检查通过。
- 本地真实渲染使用模拟 API 数据，不读取或保存下载书籍正文：1366px 下主工作区
  与状态栏为 872/436 双栏；390px 下为 374px 单栏。两种尺寸的 document
  scroll width 均严格等于 viewport，控制台无 console error，检索交互返回一条
  带引用身份的结果。
- 线上验收发现实际书名携带内部数字前缀。新增行为测试先复现
  `128942_人工智能注意力机制研究助手`，再验证展示标题清理为
  `人工智能注意力机制研究助手`。
- 线上真实检索返回长 Release ID 后，390px 页面曾扩展到 531px。新增 CSS 契约
  测试先失败，再验证结果身份使用 `overflow-wrap:anywhere`，同时提升静态样式版本
  防止浏览器复用旧 CSS。

## G4 评审 Gate

裁决：PASS。

- 最终 diff 仅涉及设计/计划/验收文档、Web 渲染、样式、静态资源版本和对应 smoke。
- v1 Agent 路由显式进入新控制台；`agent-package.v2` 仍进入独立证据审计布局，
  Package 与 Book App 继续走原有渲染路径。
- 搜索与对话表单 ID、状态管理、并发防护、API 路径和事件绑定未改变。
- 未修改后端、鉴权、共享 Token、模型、检索算法、Package 内容或发布协议。
- 长 Agent ID、hash 和策略值可安全换行；技术身份默认折叠；移动端状态栏先于工作区。
- 未提交 token、cookie、本机路径、下载内容、构建产物或其他本地数据。

## G5 部署健康 Gate

裁决：PASS。

- 候选从 canonical main 的精确 Git archive 构建；最终源码归档 SHA-256 为
  `0443574769093ad844c88e86a93ddc34cdfadfe1c8b64a780e157a47a3beba4a`。
- Linux 服务账号执行桌面前端构建、全部桌面/Web smoke、`go mod verify`、
  `go vet ./...`、`go test ./... -timeout=300s`、CGO 构建和真实 Nginx 代理 smoke，
  全部通过。默认 Go module endpoint 首次连接超时，按既有受检路径切换到
  `goproxy.cn` 与 `sum.golang.google.cn` 后通过正常 go.sum 校验。
- 最终 Linux 候选与安装后二进制 SHA-256 均为
  `79ca0f805defefd73ff8bbf4a6081cf6f4b781f1d18f74793530cdf5ef9c2007`；
  独立临时端口健康探针在生产替换前返回精确目标版本。
- 最终切换创建 root-owned 回滚批次
  `/opt/dedao-kbase/backups/direct-e3f75a9-20260809T040915Z`，其中包含替换前的
  `96401d6` 二进制与 Web 树。替换、重启、状态、哈希和 loopback health 均在
  自动回滚 trap 保护下执行，未触发回滚。
- 一次中间切换因过严的静态 grep 在替换前停止；生产仍保持旧版本。该批次的候选
  已移动到 `/opt/dedao-kbase/backups/direct-96401d6-20260809T040249Z` 内保留审计，
  未删除或误用。
- 最终服务为 `active/running`，`ExecMainStatus=0`、`NRestarts=0`；loopback 与
  public health 均返回精确部署版本。

## G6 线上验证 Gate

裁决：PASS。

- 生产页面：
  `https://kbase.executor.life/agents/attention-mechanism-research-assistant?version=1.0.1`。
- 桌面 1366px 下主工作区与状态栏为 872/436 双栏，状态栏 sticky，显示 10 项
  可信评测，document scroll width 为 1366px。
- 手机 390px 下为 374px 单栏，状态栏位于工作区之前且为 static；初始和返回
  5 条真实检索结果后，document scroll width 均保持 390px。技术凭据默认折叠。
- 标题为 `人工智能注意力机制研究助手`，内部书籍编号不再出现在产品标题；
  Agent ID 仍作为技术身份保留。
- 查询“注意力机制的演化”返回 5 条带 Release/Claim/Citation 身份的固定证据。
- 相关问题生成 671 字循证回答并显示 3 条引用；无关天气问题返回
  `insufficient_evidence` 并明确拒答。
- 浏览器日志为空。公开匿名 Agent API 与 browser session 均返回 HTTP 401。
- 最终部署窗口 service journal 没有 panic、fatal、segmentation fault、failed-start
  或 error-level 模式；Nginx 配置检查通过，既有的其他虚拟主机重复名称 warning
  未影响 KBase。

## 回滚

权威回滚输入为
`/opt/dedao-kbase/backups/direct-e3f75a9-20260809T040915Z`。恢复其中同批次的
`kbase-server` 与 `frontend-web`，重启 `dedao-kbase`，然后要求 loopback 与 public
health 返回上一生产版本 `96401d60ae99663cddf9ba49a1db8d6f4c3a9070`。

任何测试、隐私、部署健康或线上功能验证失败时，均停止后续 Gate 并回到对应上游修复。
