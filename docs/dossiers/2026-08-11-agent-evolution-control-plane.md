# Agent 自我演化控制面 Dossier

**日期：** 2026-08-11

**状态：** 已通过 G1、G2；第一层最终复裁与浏览器复验完成，G3 及后续 Gate 待裁决

**交付分支：** `codex/agent-evolution-control-plane`

**已批准设计：**
[Agent 自我演化控制面设计](../plans/2026-08-11-agent-evolution-control-plane-design.md)

**实施计划：**
[Agent Evolution Control Plane Implementation Plan](../plans/2026-08-11-agent-evolution-control-plane.md)

## 目标与边界

把 `/agent-packages` 升级为人工控制的 Agent 演化中心，并用统一控制面编排
Agent 能力演化与知识供给演化。系统可自动发现问题、生成候选、评测和淘汰无收益
方案，但任何正式发布都必须经过人工批准。

控制面复用现有 Agent Package、Knowledge Release、评测、发布、反馈、重新验证和审计
能力。Worker 继续使用既有共享 Token；本功能不引入请求签名，不允许候选扩大工具或
数据权限，也不把用户正文、凭据或制品正文写入控制面记录。

## 分层交付验收

1. **演化态势与待办中心：** 交付只读待办、Agent 聚合视图、版本历史与详情对比。
2. **双环候选与自动评测：** 交付信号、任务、不可变候选、评分卡和独立 Worker；禁止发布。
3. **人工审批与受限发布：** 交付审批绑定、发布前复核、观察窗口与安全回滚。
4. **持续学习闭环：** 把有限生产观察转成新信号，展示真实效果与质量、成本趋势。

任一层存在失败测试、未解决评审问题或阻断 Gate 时，不进入下一层。

## Gate 裁决

| Gate | 状态 | 裁决依据 |
|---|---|---|
| G1 准入 | PASS | 目标、人工发布边界、双环范围、共享 Token 约束、四层验收和成功标准均已在批准设计中冻结。 |
| G2 可行性与风险压测 | PASS | History 排序说明与后端 `created_at` 分页契约一致，详情文案与可访问性标签按视图区分；自动化门禁和真实浏览器复验均通过。 |
| G3 测试 | PENDING | 第一层窄测、全量 Web smoke、`go test ./...`、前端构建、隐私与差异检查已通过；后续层测试尚未执行。 |
| G4 评审 | PENDING | 等待逐任务规格评审、代码质量评审和最终架构评审。 |
| G5 部署健康 | PENDING | 尚未部署；不得以预期输出代替健康证据。 |
| G6 上线验证 | PENDING | 尚未上线；等待真实浏览器与生产闭环验证。 |

## 第一层验收记录

第一层已实现只读控制面存储与事件、信号去重与优先级、租约与 outbox、只读 HTTP API，
以及 `/agent-packages` 的中文 Agent 演化中心。页面首屏改为紧凑态势条、风险待办队列、
线上版本对比和一行一个 Agent 的编队表；旧编译器保留在“创建候选”对话框中，只有用户
主动打开时才加载 Release，不会从待办中心触发生成或发布。

已取得的第一层实现证据：

- 新增 pure-helper/UI smoke 先红后绿；全部 `frontend-web/scripts/*smoke.mjs` 通过。
- `frontend` 的 `npm run build` 通过；仅保留既有大 chunk 提示，不把提示误报为失败。
- 隐私 smoke 与 `git diff --check` 通过。
- 使用非私有本地 fixture 在 1440×900 验证：态势条、队列和详情均在首屏，页面横向
  溢出为 0，下方 Agent 表从首屏内开始。
- 使用 390×844 验证：选择待办后仅显示详情，可返回队列；Back 恢复队列；长 ID 不
  撑破页面，横向溢出为 0。
- 浏览器验证空队列、部分 API 503、编译对话框按需打开、关闭后焦点返回触发按钮，
  以及未提交版本字段在关闭后再次打开仍保留。

上一轮规格评审指出的问题已经修复并重新验证：

- `critical/high/medium/low` 与 `p0/p1/p2/p3` 会先规范为 P0–P3，再参与排序、中文显示
  与 CSS；URL 保持规范值，API 查询按 `p0,critical,p1,high` 的固定顺序扩展别名。
- 对话框不再静态设置 `open`，支持时通过 `showModal()` 建立原生 modal，不支持时使用
  `open + aria-modal + inert` 降级；初始焦点进入关闭按钮，Esc 与原生 `cancel` 共用一次性
  关闭路径，避免重复触发。
- 真实浏览器 fixture 复验确认：`risk=p0` 可以命中 `critical-runtime-agent` 并显示中文 P0；
  对话框同时满足 `open` 与 `:modal`，焦点位于弹窗内；输入版本 `1.2.3-qa` 后在文本框按
  Esc，会关闭弹窗、移除 `drawer` 参数并把焦点返回“创建候选”按钮，重新打开后输入值仍保留。

后续质量评审发现的问题已经修复：

- 待办查询只包含开放状态以及需要人工处理的 `blocked/failed`，历史查询只包含
  `completed/rejected/superseded/rolled_back`；Fleet 与规则页不再消耗任务分页。
- 全页与编译器 Release 使用独立 latest-request 控制器；关闭或替换请求会立即清除自己的
  loading，旧请求不能覆盖新数据或错误。
- A→B 时立即清空旧详情和事件并渲染加载态；同一任务只切详情 tab 时保留已有详情。
- 抽屉关闭后的返焦不再等待 overview、runs、detail 或 Release 请求完成。
- 查询分流、请求取消/替换、A→B、慢请求返焦和历史行 audit 路由均由可执行 helper 与
  deferred promise 测试覆盖，不再依赖关键源码字符串断言。

本轮真实浏览器复验还先后发现 fixture 默认 client ID 不符合生产长度约束，以及历史行
点击拦截器误用外层 package 路由；两项均先补失败测试再修复。最终复验确认：

- 普通 fixture 中，Inbox 在 P0/P1 + 联合演化筛选下只显示 `run-failed`，不含完成或拒绝项；
  未筛选 History 精确显示 `run-completed` 与 `run-rejected`。
- 点击 `run-completed` 后 URL 保持 `view=history&run=run-completed&tab=audit`，详情显示审计事件。
- 慢请求 fixture 中从 A 快速切到 B，B 加载态不含 A 标题，完成后只显示 B。
- API 仍在 pending 时关闭抽屉，弹窗和 `drawer` 参数立即消失，队列仍保持加载态，焦点立即
  返回“创建候选”；Release 加载中关闭再打开不会永久转圈，并最终出现 `release-fixture`。
- 首屏中文、紧凑双栏、状态条和历史表均完成视觉巡检；没有把本地巡检写成部署或上线证据。

普通与慢请求场景均使用 1280×720 CSS px、`devicePixelRatio=2` 的应用内浏览器视口。

复验入口与命令均在仓库内：

```bash
node frontend-web/scripts/agent-evolution-console-smoke.mjs
EVOLUTION_FIXTURE_PORT=8898 node frontend-web/scripts/agent-evolution-console-fixture.mjs
EVOLUTION_FIXTURE_PORT=8897 EVOLUTION_FIXTURE_DELAY_MS=4000 \
  node frontend-web/scripts/agent-evolution-console-fixture.mjs
```

累计范围复审随后发现的三项问题已经修复并复验：

- `EvolutionOverview` 与只读 HTTP view 新增向后兼容的 `failed/completed` 权威总数，领域构建器
  对全部任务计数；页面显示“运行异常”和“已完成”，不再从 `open_runs` 猜测终态。
- production-like fixture 的 `open_runs` 只含 `run-open-a/awaiting_approval` 与
  `run-open-b/evaluating`，另显式返回 `failed=1/completed=1`；Inbox 仍通过独立任务查询显示
  需要人工处理的 `run-failed`，不混入 `run-completed`。
- History 的序号、标题、排序提示、加载/错误/空态和详情提示均使用历史语义；页面不再本地
  排序，完整保留 API 返回顺序，因此跨页严格遵循后端 `created_at DESC, run_id ASC` 的全局
  cursor 顺序，不宣称按 `updated_at` 排序。
- `beginEvolutionRouteState` 使用 view+risk+type+cursor scope key；scope 改变立即清空 runs 与
  next cursor，同一 scope 内切 run、详情 tab 或 drawer 保留当前列表。

最终 1280×720 CSS px、`devicePixelRatio=2` 浏览器复验确认：

- 状态条显示“运行异常 1”“已完成 1”，overview 响应终态总数与严格非终态 `open_runs` 一致。
- History 显示“01 / 按时间排序”“演化历史”“按创建时间倒序”，无更新时间排序声明；
  `run-rejected`（创建 11:00）排在 `run-completed`（创建 10:00）之前。
- 慢请求下从 Inbox 切 History 时旧开放、失败和终态行立即消失并显示“正在读取演化历史”；
  History 加载后改 P0，旧两行同样立即消失。
- 同一 scope 内选择 `run-completed`、切换证据 tab 或打开 drawer，当前历史列表保持不变。

最终复裁继续收紧两项可见契约：History 完整保留服务端顺序，fixture 按后端
`created_at DESC, run_id ASC` 生成响应，并以同毫秒不同纳秒的 `[z,a]` 回归用例验证前端不会
因 `Date.parse()` 丢失纳秒而重排；详情返回文案、返回 tab、section 与 tablist 的可访问性
标签均由视图 helper 提供。最终浏览器复验确认 History 返回链接为“返回演化历史”，链接保留
`view=history&tab=audit`，详情 region 与 tablist 的可访问名称分别为“演化历史详情”“审计详情”；
Inbox 保持“返回待办队列”“线上版本对比”“任务详情”，返回链接仍指向 Inbox 默认详情页签。

G3 仍等待项目级完整测试门禁，G4 仍等待独立规格与代码质量评审，G5/G6 仍等待实际
部署和线上验证；不得把本地 fixture 巡检当作上线证据。

## 当前断点

任务 6 最终复裁与 G2 浏览器复验完成，等待后续 Gate 裁决；任务 7 尚未开始。
后续实施必须按实施计划顺序推进，并把每层的真实测试、评审、部署和线上证据追加到本
dossier；失败或阻断必须原样记录并回到上游修复。
