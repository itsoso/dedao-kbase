# 书籍任务独立 Worker 交付档案

## 当前状态

- 阶段：S6 测试与评审完成
- 状态：G3/G4 已通过，等待合并主干、生产部署和上线验证
- 分支：`codex/book-job-worker`
- 设计提交：`5dbb765`

## S0 Intake

用户要求在完成线上巡检后继续解决电子书任务的笼统失败与部署中断问题。讨论中
确认以下决策：

- 排队任务自动继续，运行中任务升级中断后人工重试；
- 重试创建新任务并通过 `retry_of` 保留原历史；
- 本次直接交付独立 `book-job-worker`；
- Worker 纳入 `/sources/agents`，使用同一版本和共享 Worker Token；
- 首版提供诊断与受控重启，不执行任意系统命令。

当前绕过方式是用户在任务失败后重新回到电子书页手工创建任务，但页面只显示
`job execution failed`，无法判断是否应该重新登录或重试。

## S1 现状勘察

- 任务记录保存在 `jobs.json`，执行由 `kbase-server` 内 goroutine 完成。
- KBase 启动会把旧的 `queued/running` 任务统一标记失败。
- 线上失败任务的完成时间完全一致，日志为 `failed: interrupted`，证明根因是服务
  重启而非书籍、登录或下载源。
- 当前错误清洗函数把所有任务失败统一转换为 `job execution failed`。
- 现有 Source Agent 控制面已经提供心跳、版本、能力健康、命令和受限升级协议，
  可复用而无需另建管理面。
- 仓库已经使用 SQLite 和 WAL/busy-timeout 模式，可复用现有依赖。

## G1 准入

- 裁决：PASS
- 理由：问题在生产真实发生，直接影响电子书下载和知识库入库闭环；最小端到端
  切片是持久队列、独立执行、人工安全重试和可观测状态。

## S2 需求定义

- 设计：[`../plans/2026-08-09-book-job-worker-design.md`](../plans/2026-08-09-book-job-worker-design.md)
- 核心原则：不自动重放未知副作用；历史不可覆盖；错误可操作但不泄密；Web 发布
  与下载执行解耦。

## G2 可行性与风险压测

- 裁决：PASS
- 已确认风险：运行中任务可能已完成部分下载或入库，禁止自动重放。
- 已确认存储：SQLite 事务队列，不使用 JSON 跨进程锁或外部 Redis。
- 已确认回滚：停止 Worker，原子导出旧版 JSON，再恢复旧 KBase。
- 已确认安全：复用共享 Worker Token；受控重启只允许优雅退出；不接受 shell。

## S3 规划

- 实施计划：[`../plans/2026-08-09-book-job-worker.md`](../plans/2026-08-09-book-job-worker.md)
- 实现方式：隔离分支、TDD、逐任务提交。

## S4/S5 分解与实施进展

- 任务队列已迁移为 SQLite 单一事实源，并保留幂等旧 JSON 导入与兼容导出。
- 独立 `book-job-worker` 已具备租约、续租、中断归类、人工重试和受控重启。
- KBase 已收敛为认证控制面，不再在进程内执行书籍任务。
- `/sources/agents` 和任务中心已提供 Worker 诊断、受控重启、阶段与重试历史。
- 双进程发布契约已加入同 revision 构建、分别 SHA-256、Worker
  `build-info`/`check-config`、SQLite 在线备份和旧 JSON 原子回滚导出。
- Worker systemd unit 使用共享环境文件以及稳定 Agent/Worker ID；只通过
  `Wants`/`After` 排序，不用 `Requires`，保证 KBase 单独重启不连带停止 Worker。
- 一个备份批次覆盖 KBase/Worker 二进制、Web、SQLite、旧 JSON 和 Worker unit；
  回滚先停止 Worker 与 KBase、冻结写入、导出兼容 JSON，再恢复旧服务文件。
- 首次发布允许 Worker、unit、SQLite 和旧 JSON 不存在；备份批次记录
  present/absent，回滚删除本次新增的 Worker/unit，但保留 SQLite 与下载内容。
- Task 7 验证已通过：同 revision 双二进制真实构建与分立哈希、Worker
  `build-info`/`check-config`、命令包测试、`go test ./...`、部署/system-map/privacy
  smoke 和 diff whitespace 检查。
- Task 7 评审修复把切换/回滚抽成生产唯一脚本，并增加直接执行该脚本的行为
  smoke；临时目录 mock 只替换 systemd、sudo、SQLite、健康检查等外部边界。
- 行为 smoke 覆盖首次安装与升级成功、替换后故障、present/absent 恢复、旧 unit
  的 enabled/active 与 disabled/inactive 恢复、导出顺序、SQLite/下载保留，以及
  首装回滚无残留 systemd enable 链接。
- 源码归档在解压前比较本地/远端 SHA-256，远端源码目录必须原先不存在；CI 不再
  静默忽略用户创建失败，并使用远端 URL、共享 Source Agent Token 和稳定 ID 执行
  Worker `check-config`。
- Task 7 二次评审把替换窗口扩成逐阶段故障矩阵：server/Worker/Web/unit 各次移动
  以及 unit 后 daemon-reload 都在首次安装和升级两种状态下触发真实回滚；候选
  install/Web copy 的 trap 前失败也验证不会停止现有服务。
- 回滚只在 Worker 已 enabled 时尝试 disable；unit 缺失且 systemd 清理链接后返回
  非零时以 `is-enabled` 后置条件判定，既不吞掉未清理错误，也不因缺失 unit 中断。
- README 的 sudo allowlist 和 export 集合由 smoke 与 cutover 必需变量机械比较；
  行为 mock 会删除 allowlist 外的 `KBASE_*` 环境，验证没有透传完整 operator 环境。
- G4 首轮评审发现并阻断 8 个问题：回滚后 legacy JSON 不会再次导入、重试链可绕过
  直接父级唯一性、共享 Bearer 可触发重启、成功提交与重启竞态、电子书日志泄露、
  496 错误分类、Worker 阶段/能力健康缺失，以及部署数据库路径可能错配。
- 队列修复加入 legacy fingerprint 增量对账、较新 SQLite 状态与 commit receipt 保护、
  `retry_root` 回填和链级唯一索引；独立评审通过回滚再升级、损坏 JSON、迁移、并发
  和多代重试复核。
- 控制面修复把重启限制为可信 Cookie 会话加同源 CSRF，执行成功先事务 Complete 并
  清理 receipt 再响应重启；496 端到端映射 `authentication_required`，心跳新增当前
  阶段和固定安全的能力健康文案。协议、迁移、API、UI 和竞态独立评审均通过。
- 隐私与部署修复移除电子书链路中的签名 URL、Token、绝对路径、章节 ID 和原始错误；
  cutover 在任何备份或服务变更前校验 Worker SQLite 规范路径。负例日志测试和完整
  故障窗口行为 smoke 经独立评审通过。
- 最终组合 G3 在提交 `ffe5e04` 上重新执行：`go test ./... -timeout=300s -count=1`、
  `go vet ./...`、前端生产构建、全部 `frontend-web` smoke、部署静态/行为 smoke、
  privacy/system-map smoke、`node --check` 和 `git diff --check` 均退出 0。
- 首次生产 G5 在替换后被拒绝：真实 `jobs.json` 含一个已成功的历史
  `notebooklm_export` 任务，新迁移器把未知类型误判为非法；同时 cutover 在
  `systemctl start` 后立即探活，没有等待端口就绪。自动回滚因同一 legacy 校验失败
  停在安全恢复边界，随后人工从同批备份恢复旧 Server/Web、撤下首次安装的
  Worker/unit、恢复原环境文件；旧 `jobs.json` 与备份哈希一致，公网和 loopback
  均回到 `e19de867`。
- G5 回流修复把未知历史类型限制为只读终态记录：不能领取、执行或重试，结果只保留
  五个强类型安全字段；SQLite 与 legacy 导出双向测试会剔除 session、签名 URL、
  Headers/API Key 和嵌套敏感数据。独立评审先 BLOCK 宽松过滤，再确认白名单修复
  Ready。
- cutover 增加最多 30 次、每次 1 秒的有界就绪检查，正常切换和回滚恢复共用；永久
  失败场景验证先完整恢复旧 Server/Worker/unit/Web 及 enable/active 状态再退出。
  独立评审 Ready。
- 在提交 `35fbfb0` 上再次执行全量 Go、`go vet`、前端生产构建、全部 Web smoke、
  部署静态/完整行为 smoke、privacy/system-map 和空白/干净工作区检查，均退出 0。

## Gate 记录

| Gate | 状态 | 证据 |
|---|---|---|
| G1 准入 | PASS | 生产中断任务与用户确认范围 |
| G2 可行性/风险 | PASS | 设计评审与人工重试策略 |
| G3 测试 | PASS | 全量 Go/前端、静态分析、Web、部署、隐私与 system-map 门禁均通过 |
| G4 评审 | PASS | 首轮 5 个 P1、3 个 P2 全部修复；队列、控制面、隐私/部署三组独立复核均 Ready |
| G5 部署健康 | PENDING | 首次尝试已拒绝并恢复旧版；回流修复与全量门禁通过，等待第二次生产切换 |
| G6 上线验证 | PENDING | 真实创建、KBase 重启、Worker 中断和重试 |

## 待沉淀

- SQLite 迁移和旧 JSON 回滚兼容模式；
- Web 控制面与本机/服务器 Worker 的共同管理模型；
- 发布时对长任务的人工控制与可恢复边界。
