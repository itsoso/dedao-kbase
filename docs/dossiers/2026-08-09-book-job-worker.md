# 书籍任务独立 Worker 交付档案

## 当前状态

- 阶段：S3 规划
- 状态：定义环已通过，等待实施计划确认后进入 S4/S5
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
