# Agent 控制台中文化重设计 Dossier

## 状态

已通过定义、可行性、测试与评审 Gate；部署和线上验证待执行。

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

裁决：PENDING。

## G6 线上验证 Gate

裁决：PENDING。

任何测试、隐私、部署健康或线上功能验证失败时，均停止后续 Gate 并回到对应上游修复。
