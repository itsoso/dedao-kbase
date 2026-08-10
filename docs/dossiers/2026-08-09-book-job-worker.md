# 书籍任务独立 Worker 交付档案

## 当前状态

- 阶段：S5 实施
- 状态：核心实现与双进程发布契约已完成，等待完整 G3/G4 与生产部署
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

## Gate 记录

| Gate | 状态 | 证据 |
|---|---|---|
| G1 准入 | PASS | 生产中断任务与用户确认范围 |
| G2 可行性/风险 | PASS | 设计评审与人工重试策略 |
| G3 测试 | PENDING | 实现后填写完整门禁 |
| G4 评审 | PENDING | 并发、认证、迁移和回滚独立评审 |
| G5 部署健康 | PENDING | 双服务健康、revision、日志和回滚点 |
| G6 上线验证 | PENDING | 真实创建、KBase 重启、Worker 中断和重试 |

## 待沉淀

- SQLite 迁移和旧 JSON 回滚兼容模式；
- Web 控制面与本机/服务器 Worker 的共同管理模型；
- 发布时对长任务的人工控制与可恢复边界。
