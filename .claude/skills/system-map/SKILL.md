---
name: system-map
description: "系统透明化层:维护一张『永远当前、agent 一遍读懂』的 dedao-gui 全景 —— 能力/架构/多端(Go 后端×Vue 前端×CLI)×业务流。当用户说『这系统是什么/有哪些能力/架构是什么/系统现状/onboard 这个项目/系统地图』,或任何 agent 开工前要秒懂现状时,先读 docs/system-map/INDEX.md。本 skill 定义该地图的结构、防漂移机制、与 product-pipeline 的读写闭环。"
---

# System Map — dedao-gui 系统透明化层(agent-first 永远当前)

> **一句话**:让任意 agent **读一个入口(`docs/system-map/INDEX.md`)就知道「这系统有什么、在哪、怎么扩」**,且不会过时。
> **铁律(命根子)**:一个事实只允许两种状态 —— ① **从代码生成进无人手改的文件**(`docs/_generated/system-map.json`),或 ② **带 `last-reviewed` 日期的纯叙事**(显式不含 live 数字)。**致命的第三态——手打 live 数字进叙事——结构性禁止**(它必然漂移:你今天写"38 个 service",明天加一个就错)。

## 产物

```
docs/system-map/
├── INDEX.md          ← agent 先读。read-order + facet 指针 + 三层分治 + 防漂移
└── product-map.md    ← 多端(Go 后端 × Vue 前端 × cmd/ CLI)× 业务流(下载/课程/听书/电子书/知识库)叙事 + last-reviewed
docs/_generated/
└── system-map.json   ← 代码派生:计数 + roster。无人手改;由生成器产出
scripts/dump_system_map.*  ← 生成器(扫 backend/ Go 包+Wails 绑定方法、frontend/src/ views/components/stores,确定性 sorted 输出)
```

多数 facet **不在 system-map 里重写**,INDEX 指向已有权威文档(`README.md`、`AGENTS.md`、`docs/plans/`)。本 skill 的价值 = 入口 + 防漂移 + 维护协议。

## 三层分治

| 层 | 内容 | 真源 | 漂移 |
|---|---|---|---|
| **A 叙事** | 能力/为什么/业务流/端 roster | INDEX + product-map(`last-reviewed`) | 靠新鲜度门 + S8 回写 |
| **B 代码生成** | 一切计数 + roster(Go 包/Wails 绑定方法/Vue views/components/stores 数量与清单) | `docs/_generated/system-map.json` | **零** |
| **C 在途** | 当前在做的 feature | `docs/dossiers/` | 零 |

## 防漂移机制(落地建议)

1. **生成器** `scripts/dump_system_map`(Go 或 node 皆可):扫 `backend/`(`go/types` 或 `go list ./...` + 数 `backend.App` 导出方法 = Wails 绑定面)与 `frontend/src/`(数 `views/*.vue`、`components/**/*.vue`、`stores/*.ts`),输出**确定性 JSON**(全 sorted、无时间戳)。改代码后跑它重新生成。
2. **漂移闸**:加一个 **Go 测试** `system_map_drift_test.go`(或 `frontend/scripts/system-map-drift.mjs`):重算 vs committed `docs/_generated/system-map.json`,不符即 `t.Fatal`。**它随 `go test ./...` 跑(AGENTS.md 已把它当发布前闸)→ 地图与代码不符 → 测试红 → product-pipeline G3 拦住,物理上无法带漂移上线。**(dedao-gui 暂无 CI,所以把闸挂在 `go test ./...` 上,而不是 GitHub Actions。)
3. **叙事新鲜度**:每个叙事文档 front-matter `last-reviewed: YYYY-MM-DD`;读者据此判断「叙事可信度,计数永远信 `_generated`」。
4. **在途**:`docs/dossiers/` 由 product-pipeline 写,天然当前。

## 与 product-pipeline 闭环(不靠自觉)

- **S1 Discovery 读地图**:实现新功能前读 INDEX 秒懂现状。
- **S8 沉淀写回**:① B 层——改代码必跑 `dump_system_map` 提交(否则 G3 的漂移测试红);② A 层——动了某 facet 域则更新该文档 + bump `last-reviewed`。
- **关键**:enforcement 在 **G3**(硬闸,`go test` 红即拦),authorship 在 **S8**(最易跳的末步,别只靠它)。

## Agent 读法
INDEX 顶部 READ ORDER:① 能力/目标 → INDEX 表;② 功能在哪/怎么连 → product-map;③ 地图可不可信 → `_generated/system-map.json`(计数真源);④ 怎么扩 → product-pipeline skill;⑤ 当前在做 → dossiers。

## 加一类新「会漂的结构」时
把它从 A 叙事挪进 B 代码生成:① `dump_system_map` 加一个扫描字段;② 跑生成器更新 JSON;③ 漂移测试的等值比对自动覆盖;④ 叙事里删掉手打的该数字,改引用 `_generated`。

## 边界
- 不重写 README/AGENTS —— 只 INDEX 化 + 加防漂移。
- 只钉「代码可生成的计数」进闸;叙事用 `last-reviewed`,不钉闸(否则逼出假 bump)。
- 这是本地桌面工具,system-map 可**极简**(首版只 INDEX + 端 roster 表即可,生成器/漂移测试按需再加)。

## 演进
每 feature 上线(S8)更新地图;每发现一类新「会漂的结构」按上面 4 步挪进 B 层。地图越用越准。
