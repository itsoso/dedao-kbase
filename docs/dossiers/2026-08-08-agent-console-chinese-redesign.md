# Agent 控制台中文化重设计 Dossier

## 状态

已通过定义与可行性 Gate；实现、测试、部署和线上验证待执行。

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

裁决：PENDING。

## G4 评审 Gate

裁决：PENDING。

## G5 部署健康 Gate

裁决：PENDING。

## G6 线上验证 Gate

裁决：PENDING。

任何测试、隐私、部署健康或线上功能验证失败时，均停止后续 Gate 并回到对应上游修复。
