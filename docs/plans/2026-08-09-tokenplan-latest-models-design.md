# 百炼最新文本模型接入设计

## 目标

让 KBase 的书籍知识库和得到文章分析界面可以选择百炼 Token Plan 当前支持的主流文本模型，同时保持现有默认模型和调用协议不变。

## 官方来源

- 阿里云百炼 Token Plan 团队版模型清单：<https://help.aliyun.com/zh/model-studio/token-plan-team-overview>
- 核对日期：2026-08-09
- Qwen 3.8 当前可调用的官方模型 ID 是 `qwen3.8-max-preview`，界面明确标注“预览版”，不虚构 `qwen3.8-max` 正式版 ID。

## 方案比较

### 方案 A：KBase 维护精简文本模型白名单（采用）

在现有 `knowledgeAnalysisModels` 中维护面向分析和对话的最新文本模型。优点是改动小、上线快、不会把图片和视频模型误放进文本分析入口；缺点是百炼清单变化时需要更新代码。

### 方案 B：新增后端模型目录 API

后端维护统一目录，前端启动时动态加载。长期一致性更好，但本次需要新增接口、缓存和失败降级，超出“增加模型选项”的最小范围。

### 方案 C：复制百炼完整模型目录

把文本、图片、音频和视频模型全部加入选择器。模型类型与当前 `/chat/completions` 文本入口不匹配，会制造可选但不可用的项目，因此不采用。

## 模型范围

选择器按能力与新旧顺序展示：

1. `qwen3.8-max-preview` — Qwen 3.8 Max（预览版）
2. `qwen3.7-max` — Qwen 3.7 Max（默认）
3. `qwen3.7-plus` — Qwen 3.7 Plus
4. `deepseek-v4-pro` — DeepSeek V4 Pro
5. `deepseek-v4-flash` — DeepSeek V4 Flash
6. `kimi-k2.7-code` — Kimi K2.7 Code
7. `glm-5.2` — GLM 5.2
8. `MiniMax-M2.5` — MiniMax M2.5

不加入旧代模型和非文本模型。Qwen 3.7 Max 继续作为默认值，避免预览模型替换或下线时改变现有行为。

## 实现

- 前端扩展统一模型数组，得到文章与知识库分析两个入口自然共享。
- 模型提交值始终使用百炼官方 Model ID，显示名称只负责中文可读性。
- 后端补充新模型常见显示写法的规范化测试和最小映射，确保显示名称不会被错误地直接发给百炼。
- Qwen 3.8 预览版不继承 Qwen 3.7 专用的“关闭思考”策略，保持现有 fail-closed 行为。

## 验收

- Web smoke 能断言八个模型及其官方 ID 均存在，且默认仍是 `qwen3.7-max`。
- Go 测试覆盖 Qwen 3.8、GLM 5.2、DeepSeek V4、Kimi K2.7 和 MiniMax 的显示名称规范化。
- 运行前端 smoke、`go test ./...`、前端构建、隐私检查和 `git diff --check`。
