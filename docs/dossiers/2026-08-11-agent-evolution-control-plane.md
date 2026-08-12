# Agent 自我演化控制面 Dossier

**日期：** 2026-08-11

**状态：** 第一层已通过 G1-G6，并以 revision
`389c5afe9506fc67b2ad73b5e216fd50f2edfbf4` 上线；第二层任务 7、8 已完成本地实现，
G3 本地验证与应用层第四轮独立 G4 复审通过；发布面上一轮 G4 复审为 NO-GO，修复后正在
重新复审并通过，G5-G6 尚未执行

**交付分支：** 第一层 `codex/agent-evolution-control-plane`；第二层
`codex/agent-evolution-layer-2`

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
| G3 测试 | PASS | 第一层发布范围的前端构建、全部桌面/Web smoke、`go vet ./...`、`go test ./... -timeout=300s -count=1`、部署静态/行为 smoke、隐私、系统地图和差异检查全部通过；后续层仍须各自重新过 Gate。 |
| G4 评审 | PASS | 第一层经历多轮独立规格与代码质量复审；终态统计、scope 串数据、History 语义、全局 cursor 顺序及纳秒精度问题均修复后，最终规格与质量裁决均为 PASS。 |
| G5 部署健康 | PASS | 从干净 `main` 精确归档 revision `389c5af`，由生产服务账号使用 Go 1.25.12 与 Node 22.18.0 重复构建并通过前后端测试；双二进制 revision、独立 SHA-256 和生产配置检查通过。原子切换完成，loopback 与公网健康检查均返回精确 revision；KBase 与 Worker 均为 active、退出码 0、重启计数 0，切换后五分钟内无 warning。 |
| G6 上线验证 | PASS | 已登录应用内浏览器在 `https://kbase.executor.life/agent-packages` 完成 1280×720 真实巡检：中文标题、紧凑首屏、四个视图、候选创建对话框、历史/规则/Fleet 导航均通过，无横向溢出；匿名演化 API 返回 401，认证请求返回 200。 |

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

### 第一层生产发布记录

- 发布 revision：`389c5afe9506fc67b2ad73b5e216fd50f2edfbf4`。
- 生产服务账号完成 `npm ci`、前端构建、全部桌面/Web smoke、部署静态与行为 smoke、
  隐私和系统地图检查、`go mod verify`、`go vet ./...`、
  `go test ./... -timeout=300s -count=1`，随后从同一源码构建 KBase 与 Worker。
- Worker `build-info` 与 KBase `/health` 均绑定精确 revision；双二进制安装后 SHA-256 与候选
  分别一致，Worker `check-config` 与 KBase `--check-config` 使用生产配置通过。
- 切换脚本在替换前创建范围化备份，并对现有 SQLite 使用在线一致性备份；切换成功，未触发
  自动回滚。loopback 与公网 `/health` 均返回新 revision，两个 systemd 服务均为 running、
  `ExecMainStatus=0`、`NRestarts=0`。
- 匿名 `/api/evolution/overview` 返回 401，使用既有共享 Token 的认证请求返回 200；没有新增
  请求签名、没有改 Token、环境文件、Nginx 或下载内容。
- 线上 1280×720 巡检确认：页面标题为“Agent 演化中心”，不存在旧 `Agent Packages` 标题，
  四个中文视图和候选创建对话框均可操作，页面宽度与视口一致、无横向溢出。

第一层 G1-G6 已全部通过。后续候选生成、评测、审批发布和观察闭环各层仍须重新执行自己的
G1-G6，不得继承本层结论，也不得把本地 fixture 巡检当作上线证据。

## 当前断点

任务 6（第一层）已完成并通过 G1-G6。任务 7 已实现内容寻址、不可覆盖的候选存储，
Agent/知识生成适配器，共享 Worker Token 下的租约、续租、生成、完成和失败 HTTP 协议，
以及同批交付的 `agent-evolution-worker` 与 `knowledge-evolution-worker`；生成路径只把任务推进到
`evaluating` 并排队评估，不调用 Agent Package 或 Knowledge Release 发布接口。

任务 7 聚焦验收已通过：候选、生成、Worker 客户端与 Worker 生命周期后端测试通过，两个
Worker 命令测试通过，系统地图重新生成且漂移检查通过，隐私 smoke 与 `git diff --check`
通过。

任务 8 已实现版本化权重、三分最小收益、硬门禁、历史原始指标与组件贡献持久化，新增独立
`evaluation-worker`，并在只读详情中显示评分卡。组合运行必须携带同时包含 Agent 编译与知识
重验证快照的单一内容寻址候选；知识快照冻结生成时的质量报告、反馈 assessment 与任务身份，
后续反馈不能改写历史评估输入。隐私门禁由候选制品的确定性扫描证据驱动，不再硬编码通过；
真实可信套件哈希、隐私证据身份和知识快照身份共同形成评分套件身份并写入评分卡。

首次第二层独立 G4 评审裁决为 **NO-GO**，原样记录的问题为：组合运行可凭 Agent 半份候选进入
审批；隐私门禁硬编码；评分卡、工作完成与状态迁移存在崩溃窗口；过期租约没有生产恢复调用；
知识候选等待未触发重验证且会耗尽失败预算；评估读取可变反馈；评分卡没有绑定真实套件哈希，
也没有持久化组件贡献。没有进行合并、推送或部署。

上述问题已回到实现层修复，并增加对应回归：组合候选双组件完整性、生成时自动排队重验证、
不消耗 attempt 的显式延期、领取前过期租约恢复、知识快照对后续反馈不变、凭据/私有路径隐私
门禁、评分套件与组件贡献历史加载，以及评估工作完成和状态迁移的同事务故障注入。故障注入
确认事务失败时 work 保持 `leased`、run 保持 `evaluating`、outbox 为 0；重试可完成。

第二次独立 G4 复审仍裁决为 **NO-GO**，但范围已收敛为 0 个 Critical、3 个 Important：隐私
扫描未覆盖 `api_key/apikey/client_secret` 且仅在制品落盘后执行；评分卡数据库唯一键未允许
权重或真实套件身份变化后的历史版本并存；生产路径缺少由人工把 `detected` 运行推进到
`generating` 并排队首个生成任务的受控入口。该裁决同样没有被带红进入合并或部署。

第二次复审问题现已回到实现层修复：候选规范化和落盘边界复用既有成熟敏感信息检测器，覆盖
API key、client secret、共享 Token 与私有绝对路径，并以回归确认命中时数据库和制品目录均
不产生候选；评分器版本强制绑定权重版本与真实套件身份，使既有唯一键可安全保存同一候选的
多代评分卡；新增认证保护的人工分诊 API，原子冻结已发布 Agent/Knowledge 基线、记录两段
状态事件并只排队首个生成任务，不审批、不发布。分诊的第二事件故障注入确认 run、events 与
work 同事务回滚；重复人工调用返回同一任务。第三次独立 G4 复审待执行。

最新本地 G3 证据：`go test ./... -timeout=600s -count=1` 通过（`backend/app` 142.872s），
三个演化 Worker 命令测试与演化生成、评估、隐私、评分、分诊、延期和跨 Store 领取的聚焦
`-race` 测试通过；`go vet ./...`、`go mod verify`、`frontend` 的 `npm run build`、全部
`frontend-web/scripts/*smoke*.mjs`、隐私 smoke 与 `git diff --check` 均通过。系统地图已重新
生成且漂移检查通过。一次与其他重测试并行的全量运行曾触发既有 Evidence Audit 40ms 租约
时序用例失败；初次单独连续 20 次通过，停止并发后全量复跑通过。G5 准备期间再次出现后，
独立连续 50 次复现出 2 次失败，证明文件型 manifest 心跳偶尔超过测试专用的 40ms 租约。
测试现改为 1s 租约/100ms 心跳并仍等待 1.5s，继续验证“排队任务等待超过一个租约仍不预领”；
生产协调器未修改，断言未放宽，修后连续 10 次通过。第二层仍不能宣告 G4/G5/G6 通过：
必须取得新的独立复审裁决，并在干净主干上
完成部署健康与线上验证。

第三次独立 G4 复审为 **NO-GO**：0 个 Critical、1 个 Important，另有 1 个 Minor。Important
指出首次分诊后若同一 Agent Package 发布头变化，重复分诊会用新头构造不同任务身份并冲突；
Minor 指出多代评分卡相同 `created_at` 时“最新”查询缺少稳定次序。两项均返回实现层处理：
`generating` 运行现在会在读取任何可变发布头之前，直接用已冻结基线找回首次生成任务；回归
覆盖首次分诊、发布新版本、再次分诊仍返回原任务和原基线。评分卡最新查询增加插入顺序的稳定
次级排序，并进入第四次独立 G4 复审。

第四次独立 G4 复审裁决为 **PASS / GO**：0 个 Critical、0 个 Important、0 个 Minor。
复审确认 generating Run 会先用冻结基线恢复原 Work，再接触可变 published head；新发布
`1.1.0` 后重放仍返回原 Work 与 `1.0.0` 基线；评分卡同时间戳读取稳定。前三轮修复的隐私
前置阻断、组合候选双组件、不可变知识快照、事务化评估终结、租约恢复与 defer、HTTP 鉴权、
共享 Token、无请求签名、无自动审批或发布旁路均未回归。第二层由此通过 G3-G4，可以进入
干净主干集成与 G5-G6，但不得把本地验证视为已部署或已上线。

G5 准备检查随后发现生产部署契约只安装 `book-job-worker`，尚未安装本层新增的 Agent、
Knowledge 与 Evaluation Worker；此时直接部署会出现 API 已上线但演化任务无人消费的半交付，
因此没有进入生产切换。实现层已补充一个 systemd 模板和三 Worker 范围化原子切换：三者共享
既有 Worker Token，但使用独立进程与稳定 Worker ID；安装前分别校验 revision、配置与
SHA-256，失败会恢复全部三者原有二进制、unit、enabled/active 状态，不改环境、数据库或知识
制品。行为 smoke 覆盖首次安装、升级、哈希不符和第三个 Worker 启动失败时整组回滚。该新增
发布面仍须重新通过全量验证与独立 G4 复审，之后才能进入真实 G5。

首次发布面独立复审裁决为 **FAIL / NO-GO**：0 个 Critical、5 个 Important、1 个 Minor。
问题包括旧 Worker 停止失败被忽略、错误 trap 安装晚于备份/暂存写入、只做瞬时 active 检查
且未绑定 component/revision、systemctl 查询错误被误判为正常 inactive/disabled、README 未把
三个 candidate 路径传入干净的服务账号环境，以及 unit 未校验 SHA-256。该裁决没有被带入提交
或部署。

这些问题已经回到实现层逐项修复：切换和回滚都严格验证 stop 结果；trap 在首次备份写入前
生效，并区分暂存、停服、替换三个失败阶段；候选在任何写入前校验独立 SHA-256、精确 component
和 revision，unit 同样校验源与 staged SHA；`check-live` 使用生产共享 Token 向 KBase 提交带
capability/version/revision 的真实心跳，systemd 在每次启动前重复执行；启动后等待稳定窗口，
要求三个实例持续 enabled/active 且 `NRestarts=0`；systemctl 的正常非零状态与 D-Bus/权限错误
被严格区分。README 已显式传递 candidate 路径、unit SHA 与精确 revision。

生产切换行为 smoke 直接执行同一脚本，现覆盖首次安装、升级、二进制哈希不符、组件互换、
unit 哈希不符、systemctl 查询异常、第二个 candidate 暂存失败、第三个 Worker 启动失败、停服
失败与稳定窗口掉线，并验证整组回滚及可重试清理。`check-live` 的成功与 401 失败均由真实
`httptest` HTTP 往返覆盖。第二层全量 `go test ./... -timeout=600s -count=1` 再次通过
（`backend/app` 126.917s）；前端构建、两套全部 smoke、`go vet ./...`、`go mod verify`、隐私、
系统地图、部署静态/行为 smoke、YAML 解析和差异检查均通过。三个真实 Worker 二进制从相同
测试 revision 构建，`build-info`、`check-config` 与独立 SHA-256 均通过。

本轮全量测试首次运行暴露另一处既有 book-job Worker 测试使用 250ms 文件型 SQLite 租约的
调度抖动：聚焦用例独立连续 100 次通过，但高负载全量运行中续租被拖过租约后失败。测试专用
租约改为 5s、续租 100ms，并在完成后等待 350ms（超过三个续租周期），仍同时验证“确实续租”
与“完成后停止续租”；生产 Worker 未修改。聚焦复验和第二次全量运行均通过。

第五轮发布面独立复审仍裁决为 **FAIL / NO-GO**：0 个 Critical、2 个 Important、1 个 Minor。
第一项指出真实非 development revision 被错误编码进 capability 的诊断 `code`，而服务端仅接受
固定错误枚举，导致生产 `check-live` 必然 400；第二项指出 Bash 在 `func || handler` 条件上下文
中禁用整个函数体的 `errexit`，回滚/恢复中间命令可能失败后继续并被后续成功掩盖；Minor 指出
README 上线验证使用裸 Worker 名称，安装目录未必在 PATH。再次保持未提交、未部署。

修复后，capability health 新增独立、受限校验并随既有 JSON 持久化的 `revision` 字段，诊断
`code` 恢复为空枚举；真实 Evolution Worker 客户端通过完整 KBase HTTP handler 使用共享 Token
提交 `version=1.0.1/revision=0123456789abcdef`，并从 Source Agent Store 读回相同值。回滚、状态
恢复、暂存清理中的每个关键 stop/install/remove/daemon-reload/enable/start/state check 都显式
`|| return 1`，不再依赖条件上下文中的 `errexit`；新增故障注入确认旧 evaluation 二进制恢复
失败后不会继续 daemon-reload 或误报完整恢复。README 改用 `${KBASE_EVOLUTION_BINARY_DIR}` 下的
绝对二进制路径并逐个精确比对 revision。上述修复正在进入第六轮独立复审，仍未进入 G5。

第六轮发布面独立复审裁决为 **PASS / GO**：0 个 Critical、0 个 Important、0 个 Minor。
复审重新执行真实 production revision 的共享 Token 心跳与持久化窄测、三个 Worker CLI 包、
Bash 语法、完整切换/回滚行为 smoke、部署契约 smoke 和差异检查，确认 revision 字段契约、
条件上下文中的显式错误传播、回滚 install 故障停止点、绝对路径 revision 验证均已闭合。
部署前 `check-live` 同时改为复用 systemd 的 `$(hostname)-<worker>` 稳定身份，不创建长期离线的
临时 Agent 条目。第二层应用与发布面由此通过 G3-G4，可以进入干净主干集成与真实 G5；该
裁决不表示已经部署或上线。

后续实施必须按实施计划顺序推进，并把每层的真实测试、评审、部署和线上证据追加到本
dossier；失败或阻断必须原样记录并回到上游修复。
