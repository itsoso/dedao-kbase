# Agent 自我演化控制面设计

## 背景

`/agent-packages` 当前同时承担 Agent Package 目录和候选编译入口。页面使用大标题、
宽松留白和逐版本平铺列表，首屏主要空间被编译表单占用。用户能看到 Package、Agent
和 Book App 入口，却无法快速回答更重要的问题：哪些 Agent 正在退化、哪些知识已经
过期、哪些候选值得批准，以及最近一次演化是否真正改善了线上效果。

系统已经具备版本化 Agent Package、知识 Release、候选编译、可信评测、受限发布、
运行审计、知识反馈和重新验证等基础能力，但这些能力仍是离散入口，没有统一状态、
统一待办、统一评分卡和统一审批链。

本设计把该页面升级为“Agent 演化中心”，并新增统一演化控制面。系统自动发现问题、
生成候选、运行评测和淘汰无收益方案；任何正式发布仍必须经过人工批准。

## 已确认的产品决策

- 同时建设 Agent 能力演化和知识供给演化，形成完整双环。
- `/agent-packages` 首屏优先服务异常与待办处置，不再优先展示编译器。
- 所有正式发布都需要人工批准。
- 系统可以自动过滤无收益候选，但不能自动发布候选。
- 演化成功使用多目标评分卡判断，而不是只看单一质量指标。
- 安全、权限、隐私、引用完整性和固定回归测试是不可降低的硬门槛。
- 采用统一控制面和独立 Worker。
- Worker 继续使用既有共享 Token，不新增请求签名机制。
- 协议保持平台中立，首版沿用现有部署环境交付。
- 采用分层验收，任何一层未通过不得进入下一层。

## 方案选择

采用统一演化控制面，把现有 `compile / evaluate / publish`、知识反馈、重新验证、
运行审计和观察数据编排成一条可恢复、可解释、可审批的流水线。

不采用以下方案：

- 只重做页面：能改善视觉和操作密度，但不能形成真实演化闭环。
- 直接建设全自动优化实验室：需要更大的评测数据、在线流量和成本预算，也会扩大
  自动决策风险，不符合人工控制边界。
- 让 Worker 各自维护演化状态：会产生重复队列、状态漂移和无法统一审批的问题。
- 自动发布低风险候选：仍然绕过人工发布决策，与既定治理要求冲突。

## 演化双环

### Agent 能力环

```text
运行反馈 / 审计 / 成本与延迟
  -> 能力缺口信号
  -> 提示词、模型、检索或工具策略候选
  -> 基线对比评测
  -> 人工批准
  -> 新 Agent Package
  -> 观察窗口
```

Agent 环可以调整提示词配置、模型能力选择、检索策略、工具规则和安全策略，但候选
不得扩大现有权限。任何需要新增工具或扩大数据范围的变化必须进入独立需求流程，
不能作为自动演化候选处理。

### 知识供给环

```text
新来源 / 内容过期 / 冲突证据 / 用户反馈
  -> 知识缺口信号
  -> 重新验证或知识重建
  -> Knowledge Release 候选
  -> 影响分析与评测
  -> 人工批准
  -> 新 Knowledge Release
  -> 观察窗口
```

知识环输出不可变 Release。新 Release 可能继续触发 Agent 环，检查当前 Agent 的检索、
提示词、评测集或引用边界是否需要升级。

### Combined 演化

当问题同时涉及知识和 Agent 策略时，控制面创建 `combined` 演化任务。Combined 候选
必须分别展示知识变化和 Agent 变化的独立贡献，避免用两类变化互相掩盖回归。

## 状态机

主状态流：

```text
detected
  -> triaged
  -> generating
  -> evaluating
  -> awaiting_approval
  -> approved
  -> publishing
  -> observing
  -> completed
```

旁路状态：

- `blocked`：硬门槛、前置条件或重试预算阻断。
- `rejected`：人工拒绝候选。
- `failed`：阶段执行失败且尚未恢复。
- `superseded`：基线或更新候选已经取代当前任务。
- `rolled_back`：发布后触发安全回滚。

所有状态转换由控制面事务执行。Worker 只能提交阶段结果，不能直接改变审批或发布
状态。非法转换返回明确冲突，不做静默兼容。

## 核心数据模型

### EvolutionSignal

描述一次可验证的演化触发：

- `signal_id`
- `signal_type`
- `source_type`
- `source_id`
- `package_id`
- `release_id`
- `severity`
- `observed_value`
- `baseline_value`
- `deduplication_key`
- `evidence_refs`
- `observed_at`

信号不保存任意用户正文、Cookie、Token 或模型私密输入。需要解释的内容只保存有限
原因码、指标和可解析制品引用。

### EvolutionRun

表示一次完整演化任务：

- `run_id`
- `run_type`: `agent_policy / knowledge_release / combined`
- `package_id`
- `baseline_package_version`
- `baseline_release_ids`
- `risk_level`
- `priority_score`
- `status`
- `trigger_signal_ids`
- `current_candidate_id`
- `failure_code`
- `failure_message`
- `created_at / updated_at`

### EvolutionCandidate

候选内容不可变，通过内容哈希绑定：

- `candidate_id`
- `run_id`
- `candidate_type`
- `content_hash`
- `artifact_ref`
- `baseline_identity`
- `change_summary`
- `generator_version`
- `created_at`

候选变化必须生成新身份，不允许覆盖原制品。

### EvolutionScorecard

绑定候选、线上基线、测试集和评分器版本：

- `scorecard_id`
- `candidate_id`
- `baseline_identity`
- `suite_version`
- `scorer_version`
- `hard_gates`
- `metrics`
- `weighted_score`
- `baseline_score`
- `delta`
- `decision`
- `failure_case_refs`

### EvolutionApproval

批准记录只对一个候选哈希和一个线上基线有效：

- `approval_id`
- `run_id`
- `candidate_id`
- `candidate_content_hash`
- `baseline_identity`
- `decision`
- `reason_code`
- `note`
- `approved_by`
- `created_at`
- `expires_at`

候选、评测套件或线上基线发生变化后，原批准自动失效。

### EvolutionObservation

记录发布后的有限观察结果：

- `observation_id`
- `run_id`
- `published_identity`
- `window_start / window_end`
- `metrics`
- `hard_gate_incidents`
- `outcome`
- `rollback_identity`

## 信号与优先级

Agent 能力信号包括：

- 任务完成率持续下降；
- 错误拒答或无法回答比例上升；
- 用户反馈错误、无用或缺少证据；
- 工具失败、超时或权限拒绝；
- 延迟或成本异常；
- 模型能力退化、下线或受支持能力变化；
- 人工提出提示词、检索或策略改进；
- 固定回归评测发现退化。

知识供给信号包括：

- 新来源或新版本进入知识库；
- 当前 Release 超过新鲜度期限；
- 新证据与现有结论冲突；
- 引用失效或来源不可访问；
- 用户反馈内容错误、过时或覆盖不足；
- Agent 因缺少证据重复拒答；
- 重新验证失败或生成新候选 Release。

控制面按去重键、时间窗口和对象关系聚合信号。待办优先级由风险、影响范围、预计
收益和等待时间共同计算。优先级只决定排序，不能绕过硬门槛或人工审批。

## 多目标评分卡

默认评分权重：

| 指标 | 权重 | 说明 |
|---|---:|---|
| 回答质量 | 30% | 正确性、完整性、可执行性 |
| 证据质量 | 25% | 引用有效率、支撑度、独立来源 |
| 任务完成率 | 20% | 是否完成用户目标 |
| 稳定性 | 10% | 错误率、超时率、工具成功率 |
| 成本 | 10% | 单任务和观察窗口预算 |
| 延迟 | 5% | P50 与 P95 对比 |

硬门槛不计入加权分：

- 安全策略不得退化；
- 权限范围不得扩大；
- 隐私检查必须通过；
- 引用完整性必须满足 Package 契约；
- 固定回归测试必须通过；
- 发布和回滚路径必须可用。

候选进入审批队列必须通过全部硬门槛，且默认综合评分相对线上基线至少提升 3 分。
阈值和权重必须版本化，历史评分卡不得因配置更新被重写。

## 人工审批与受限发布

人工操作只有：

- 批准并发布；
- 拒绝并填写原因；
- 要求重新生成；
- 暂缓处理。

审批详情必须展示触发原因、版本差异、评分卡、失败案例、预计收益、风险、成本、
影响范围和回滚版本。发布 Worker 在执行前再次验证批准记录、候选哈希、评测版本和
线上基线。

发布后进入观察期。系统可以在硬门槛异常、错误率激增或引用完整性下降时自动回滚，
但回滚后的重新发布仍需要新的人工批准。

## 控制面与 Worker

统一控制面负责：

- 接收、去重和聚合演化信号；
- 创建演化任务并执行状态转换；
- 分发受限工作项；
- 保存候选、评分卡、批准和观察索引；
- 验证发布前置条件；
- 提供待办、时间线和审计查询。

独立 Worker：

- `knowledge-evolution-worker`：知识过期、冲突、覆盖缺口、重新验证和 Release 候选。
- `agent-evolution-worker`：提示词、模型、检索和工具策略候选。
- `evaluation-worker`：线上基线与候选的固定套件对比评测。
- `release-worker`：验证批准后执行发布、观察和安全回滚。

Worker 使用既有共享 Token 调用受限控制面接口。工作协议不得接受任意命令、脚本
路径或 shell 参数。

## 存储与可靠性

新增独立 `evolution_control.sqlite3`，至少包含：

- `evolution_signals`
- `evolution_runs`
- `evolution_candidates`
- `evolution_scorecards`
- `evolution_approvals`
- `evolution_observations`
- `evolution_events`
- `evolution_worker_leases`
- `evolution_outbox`

候选和评测报告保存为内容寻址的不可变制品；SQLite 只保存身份、状态、索引和关系。

可靠性约束：

- 每个信号、候选、评测和发布请求都有幂等键；
- Worker 使用事务租约领取任务并定期心跳；
- 租约过期后任务可重新领取，但发布不能重复执行；
- 每个阶段有独立重试预算和退避策略；
- 重试耗尽进入 `blocked`，展示有限错误码和处理建议；
- 状态转换、事件记录和 outbox 写入同一事务；
- 所有失败对控制面可见，禁止空捕获和静默成功。

## HTTP 接口

控制面提供：

```text
GET  /api/evolution/overview
GET  /api/evolution/runs
GET  /api/evolution/runs/:run_id
GET  /api/evolution/runs/:run_id/events

POST /api/evolution/signals
POST /api/evolution/runs/:run_id/triage
POST /api/evolution/runs/:run_id/generate
POST /api/evolution/runs/:run_id/evaluate
POST /api/evolution/runs/:run_id/approve
POST /api/evolution/runs/:run_id/reject
POST /api/evolution/runs/:run_id/publish
POST /api/evolution/runs/:run_id/rollback
POST /api/evolution/runs/:run_id/retry
```

现有 Agent Package、Knowledge Release、反馈、重新验证和审计接口保持兼容。新增控制面
通过内部适配器调用这些能力，不复制其业务规则。

## Web 信息架构

`/agent-packages` 改为紧凑演化工作台：

1. 顶部紧凑工具栏：标题、搜索、范围、时间窗口和“创建候选”。
2. 态势条：待审批、阻断、知识过期、运行异常和今日完成。
3. 左侧待办队列：风险、类型、原因、预计收益、等待时间和状态。
4. 右侧详情：线上与候选差异、评分卡、证据、审计和审批操作。
5. 下方 Agent 聚合表：一个 Agent 一行，版本历史折叠展示。
6. 次级视图：待办、全部 Agent、演化历史和演化规则。

当前大标题区删除，编译器移入“创建候选”侧边抽屉。Package、Agent 和 Book App 入口
收进每行操作区和详情快捷入口。技术 ID 作为次级信息，不再充当主要可读标题。

筛选、当前待办、详情标签和分页同步到 URL，例如：

```text
/agent-packages?view=inbox&risk=p0,p1&type=combined&run=run-123
```

桌面端使用队列和详情双栏；小屏幕使用队列到详情的两层导航。所有操作使用语义化
按钮或链接，支持键盘、可见焦点和异步状态播报。

## 分层交付

### 第一层：演化态势与待办中心

- 重构 Agent Package 页面；
- 接入现有 Package、Release、反馈、复核、审计和运行状态；
- 实现 Agent 聚合列表、异常队列、版本历史和详情对比；
- 保持只读，不生成候选、不触发发布。

### 第二层：双环候选与自动评测

- 实现信号、任务、候选和评分卡；
- 接入知识、Agent 和评测 Worker；
- 自动去重、分级、生成候选、对比评测和淘汰无收益候选；
- 候选仍不能发布。

### 第三层：人工审批与受限发布

- 实现审批记录、内容哈希绑定和审批失效；
- 接入发布 Worker、观察窗口和自动回滚；
- 打通拒绝、暂缓和要求重生成流程。

### 第四层：持续学习闭环

- 把发布后反馈转化为新信号；
- 展示演化原因、变化内容和真实结果；
- 按 Agent、知识源和时间查看质量与成本趋势。

## 测试与验收

按 TDD 分层覆盖：

- 状态机：所有合法和非法转换；
- 存储：迁移、幂等、事务、并发领取、租约、事件与 outbox；
- API：共享 Token、分页、筛选、审批约束和响应脱敏；
- Worker：领取、心跳、续租、崩溃、超时、重试耗尽和重复执行保护；
- 评测：固定基线、固定数据集、固定评分卡和硬门槛；
- 发布：过期审批、哈希不匹配、基线漂移、重复发布和回滚；
- Web：筛选、深链接、前进后退、待办选择、版本聚合和审批确认；
- 可访问性：键盘操作、焦点、状态播报和语义控件；
- 端到端：Agent 环、知识环和 Combined 环各一条完整链路。

完整门禁包括相关窄测、并发测试、`go test ./...`、前端 smoke、前端生产构建、
system-map 生成与漂移检查、隐私检查和 `git diff --check`。

## 成功标准

1. 用户进入页面后无需滚动即可看到最高优先级演化待办。
2. 同一 Agent 聚合为一行，能查看当前线上版本、健康状态和演化历史。
3. 知识和 Agent 信号能进入同一队列并形成可解释的 Combined 任务。
4. 无收益候选自动淘汰，任何发布都需要有效人工批准。
5. 候选、评分卡、批准、发布和观察形成完整审计链。
6. Worker 崩溃或租约过期不会丢任务或重复发布。
7. 线上基线或候选变化会使旧批准失效。
8. 硬门槛异常能够自动回滚，回滚后不能自动重新发布。
9. 页面支持深链接、键盘操作、明确错误和可恢复状态。
10. API、日志、制品和文档不泄露 Token、Cookie、用户正文或机器专用路径。

## 非目标

- 自动发布候选；
- Agent 自行扩大工具或数据权限；
- 无固定评测集的随机提示词优化；
- 基于少量在线流量的自动模型切换；
- 让大模型直接修改控制面状态；
- 第一版引入复杂多租户权限系统。
