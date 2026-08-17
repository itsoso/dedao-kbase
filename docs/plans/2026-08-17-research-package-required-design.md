# Research Package 必填校验设计

## 背景

研究运行后端要求每次请求同时提供 `package_id` 和 `package_version`，用于固定知识范围、模型配置和工具策略。研究工作台目前却把这两个字段标记为“可选”，并允许空值请求到达服务端，最终只显示原始错误 `invalid_research_request`。

## 目标

- 让前端契约与后端必填约束一致。
- 缺少 Package 或版本时不发送创建请求，并给出可执行的中文提示。
- 保留从 Agent 控制台进入研究工作台时通过 URL 自动填充 Package 和版本的行为。
- 对后端返回的研究创建错误提供中文说明，不改变服务端的 fail-closed 权限与范围校验。

## 方案

采用最小前端修复：

1. 将 Agent Package 和版本字段标记为必填，去掉“可选”占位文案。
2. 在提交函数中先校验两个字段；任一缺失时更新页面消息并直接返回，不调用创建接口。
3. 将 `invalid_research_request` 与 `research_package_not_eligible` 映射为中文、可执行的研究创建提示。
4. 增加指向 Agent 管理页的选择入口，避免用户必须记忆 Package ID。
5. 不新增默认 Package、不自动猜测知识范围，也不放宽后端校验。

## 数据流

`Agent 控制台深链接或用户填写` → `前端必填校验` → `POST /api/research/runs` → `后端继续验证 Package、模式、来源与策略`。

## 错误处理

- Package/版本缺失：前端中文提示，不发请求。
- Package 不支持所选模式或来源：显示中文提示并引导返回 Agent 管理页选择。
- 其他错误：保留现有错误传播，避免静默失败。

## 验收标准

- 空 Package 或版本无法触发研究创建请求。
- 页面明确显示两个字段为必填，并提供 Agent 管理入口。
- 有效 Package 与版本仍能正常创建研究。
- URL 中已有 `package_id` 和 `version` 时仍能自动填入。
- 前端研究工作台 smoke、隐私检查和差异检查通过。
