#!/usr/bin/env bash
# SessionStart hook —— 把项目治理纪律注入每个 session/subagent(stdout 即注入 context)。
cat <<'EOF'
[harness bootstrap · dedao-gui 纪律 · 违反=bug]
- 开工前读 docs/system-map/INDEX.md(若已建)秒懂全景;硬规则权威=AGENTS.md;走完整需求流程用 product-pipeline skill。
- Go 用 gofmt;改 Go 绑定后**重生成 frontend/wailsjs/**(wails dev / wails generate module),**别手改生成产物**。
- 不提交生成/本地产物:frontend/dist、build/bin、config.json、.DS_Store;不提交 token/cookie。
- 改配置默认值/路径/docs/prompts/export/提交前,用 privacy-guard skill;提交前跑 bash scripts/privacy-smoke.sh + git diff --check。
- 版权红线:仅个人学习用,得到内容勿传播。
- 测试绝不 `| tail`(吞退出码);Gate(go test ./... + cd frontend && npm run build)不跳。
- 系统计数只准代码派生(docs/_generated/system-map.json),绝不手打 live 数字进叙事。
- 先 grep 复用现有 service/store/组件 > 新建;从干净 origin/main 起分支。
EOF
