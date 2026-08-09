# 书籍任务独立 Worker 与可恢复队列设计

## 背景

当前电子书下载和知识库入库任务虽然持久化在 `jobs.json`，实际执行仍由
`kbase-server` 进程内的 goroutine 完成。KBase 发布或服务重启会中断正在运行的
任务；启动恢复逻辑只能把 `queued/running` 统一标记为失败。任务错误又会被统一
转换为 `job execution failed`，用户无法区分登录失效、下载失败、知识包生成失败
和服务升级中断。

线上巡检确认，一批同一时刻失败的任务并非内容或登录问题，而是在服务重启时
被中断。这个设计把任务执行从 Web 控制面拆出，并在不自动重放未知副作用的前提
下恢复排队任务、保留失败历史和人工控制权。

## 已确认的产品行为

- 采用统一控制面和独立 `book-job-worker`。
- 尚未开始的 `queued` 任务在重启后自动继续。
- 已进入 `running` 的任务若 Worker 中断，不自动重放，转为 `interrupted`。
- “重新执行”创建新任务，通过 `retry_of` 关联原任务，原历史保持不变。
- 同一原任务已有 `queued/running` 重试时，拒绝重复创建。
- Worker 进入 `/sources/agents` 总览，显示状态、版本、当前任务、最后心跳和最近
  安全错误。
- Worker 与 KBase 使用同一发布版本和既有共享 Worker Token。
- 首版只允许诊断和受控重启，不执行任意系统命令。

## 方案选择

采用 SQLite 持久队列。KBase 和 Worker 通过事务共享任务、租约、状态与重试历史。
SQLite 已符合当前单机部署边界，不增加外部队列服务，同时比 `jobs.json` 全文件
重写和跨进程文件锁更适合并发领取、崩溃恢复与后续任务增长。

不采用以下方案：

- `jobs.json` 加文件锁：迁移较小，但全文件重写、跨进程锁和崩溃恢复更脆弱。
- Redis 或云队列：可靠但增加新的基础设施、认证和运维依赖，超出当前规模。
- 继续在 KBase 进程内执行：无法消除 Web 服务发布对下载任务的影响。

## 架构

### 控制面

`kbase-server` 只负责：

- 校验得到会话、书籍身份和访问权限；
- 创建和查询任务；
- 创建人工重试任务；
- 展示任务与 Worker 状态；
- 向 Worker 下发受限诊断或重启命令。

创建任务后只写入持久队列，不再启动进程内 goroutine。现有接口继续保留，并增加：

- `POST /api/jobs/<job_id>/retry`

重试接口重新校验书籍身份和访问权限，成功后返回新任务。若原任务不允许重试，
或已存在活动重试，则返回冲突，不产生新记录。

### 独立 Worker

新增 `cmd/book-job-worker`。Worker 与 KBase 使用相同的得到配置、下载根目录和知识
库根目录，但不持有 KBase 管理员 Token。Worker 循环执行：

1. 上报 Agent 心跳和版本；
2. 用事务领取最早的 `queued` 任务；
3. 写入 Worker 身份、租约和当前阶段；
4. 下载或生成知识包，并定期续租；
5. 写入安全结果或结构化失败；
6. 领取下一任务。

首版允许多个 Worker 进程连接同一队列，但同一任务只能被一个租约持有。生产默认
只运行一个 Worker。

### Agent 管理接入

Worker 复用现有 Source Agent 心跳与命令协议，使用 `worker_type=book_job` 注册到
`/sources/agents`。总览按能力显示当前任务阶段、队列状态和最近安全错误。

受控重启命令只触发优雅退出：Worker 先把当前运行任务标记为 `interrupted`，释放
资源后退出，再由 systemd 拉起。它不接受任意命令、脚本路径或 shell 参数。Worker
随 KBase 同一版本发布，升级继续走现有受限升级和回滚边界。

## 数据模型与状态机

任务状态为：

```text
queued -> running -> succeeded
                  -> failed
                  -> interrupted
```

SQLite 记录至少包含：

- 原有任务身份、类型、书籍 ID、EnID、下载格式和时间戳；
- `retry_of`：直接关联被重试的任务；
- `stage`：`queued`、`downloading`、`building_knowledge`、`completed`；
- `worker_id`、`lease_expires_at`：内部领取和租约字段；
- `failure_code`、`failure_message`：安全、可操作的失败信息；
- 安全结果字段和有限状态事件。

领取任务必须在单个事务内完成。Worker 只可更新自己持有且租约有效的任务。正常
关闭时，当前任务转为 `interrupted`；进程崩溃时，由存活 Worker 或控制面在租约
过期后完成同样转换。过期的 `running` 任务不会回到 `queued`。

重试创建全新 `queued` 任务并保留原参数。活动重试唯一性由数据库事务约束，不
依赖浏览器按钮禁用。

## 错误处理与隐私

失败按执行阶段映射为有限错误码和中文提示，例如：

- `authentication_required`：得到登录已失效，请重新登录；
- `download_failed`：电子书下载失败，可以重新执行；
- `knowledge_build_failed`：下载完成，但知识包生成失败；
- `worker_interrupted`：Worker 升级或异常退出，任务已中断；
- `source_changed`：任务参数或书籍权限已经变化；
- `unknown_failure`：任务执行失败，请查看诊断并重试。

API、前端、心跳和任务记录不保存 Cookie、Token、下载正文或机器专用绝对路径。
底层错误不得直接进入 API。Worker 日志只记录任务 ID、阶段、安全错误码和必要的
脱敏诊断。

## Web 交互

电子书卡片和任务中心显示明确阶段：排队、下载中、生成知识包、完成、失败、
中断。`failed/interrupted` 状态显示安全原因和“重新执行”按钮。展开详情可查看原
任务、新重试任务及时间线，但不展示服务器路径。

`/sources/agents` 增加“书籍任务 Worker”卡片，展示：

- 在线、离线、降级或处理中；
- 当前发布版本；
- 当前任务和阶段；
- 最后心跳；
- 最近安全错误；
- 诊断和受控重启操作。

## 迁移与回滚

首次启动时，把现有 `jobs.json` 按任务 ID 幂等导入 SQLite，不删除原文件。迁移
识别旧记录中的中断日志，并把对应失败任务转换为 `interrupted` 和
`worker_interrupted`。无法分类的旧错误保留为安全的 `unknown_failure`。

切换后 SQLite 是唯一写入源，避免双写漂移。发布前备份任务数据库、`jobs.json`、
下载目录元数据和知识库状态。

随 Worker 提供兼容导出命令。发布回滚时先停止 Worker，再把 SQLite 任务原子导回
旧版 `jobs.json`，最后恢复旧 KBase。导出把 `interrupted` 映射为旧版可识别的
`failed`，同时保留安全错误和任务历史。回滚不删除 SQLite 或下载内容。

## 测试策略

按 TDD 分层覆盖：

- 存储层：建库、幂等迁移、事务领取、租约续期、租约归属、并发唯一领取、状态
  迁移、活动重试唯一性和旧 JSON 导出；
- HTTP 层：认证、权限复核、允许/拒绝重试、重复重试冲突、响应脱敏；
- Worker 层：排队领取、阶段更新、成功、分类失败、优雅退出、租约过期恢复、心跳
  和受控重启；
- 前端 smoke：中文状态、失败原因、重试按钮、历史链和 Worker 管理卡片；
- 发布验证：KBase 重启不影响运行任务，Worker 重启使运行任务进入中断，排队任务
  继续，回滚导出可被旧版读取。

完整门禁包括相关窄测、并发测试、`go test ./...`、`go vet ./...`、前端 smoke、
前端构建、system-map 生成与漂移检查、隐私检查和 `git diff --check`。

## 成功标准

1. KBase 发布重启不再中断独立 Worker 正在执行的任务。
2. Worker 重启后，排队任务自动继续，运行任务明确进入“升级中断”。
3. 人工重试创建新任务并保留审计链，重复点击不会重复执行。
4. 同一任务在多 Worker 并发领取时最多执行一次。
5. 用户能区分登录、下载、知识生成和 Worker 中断错误。
6. `/sources/agents` 能观察和受控重启书籍任务 Worker。
7. 旧任务完整迁移，回滚能恢复旧版可读取的任务文件。
8. API、日志、Git 和文档不泄露 Cookie、Token、下载内容或机器专用路径。
