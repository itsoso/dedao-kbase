---
name: product-pipeline
description: "dedao-gui 产品全生命周期总指挥:把一句用户需求,经『需求→PRD→规划→研发→测试→构建→验证→沉淀』串成一条可追溯、可失败、可恢复的流水线。当用户说『我想要 X』『把这个需求走一遍流程』『从需求到上线』『加一个功能 X』时用本 skill。它不重造实现/构建能力,而是用 Gate 编排:四问 + Discovery + 评审 + go test/vue-tsc 测试闸 + wails build。硬规则(build/test/style/commit/security)以 AGENTS.md 为准。"
---

# dedao-gui Product Pipeline — 从需求到验证的总指挥

> **方法论来源**:移植自成熟研发流程范式(双环 + 6 道 Gate + Dossier + 失败即停 + 反馈环纪律),已**重新绑定到 dedao-gui 实况**(Wails / Go 后端 / Vue3 前端;无 agent 团队、无服务器部署/自动回滚——"上线"=`wails build` 桌面分发)。**硬规则的单一真源是 [`AGENTS.md`](../../../AGENTS.md)**(build/test 命令、gofmt、命名、commit/PR、安全)——本 skill 不重述,只在 Gate 处引用。

> 何时**不**用本 skill:单文件小修 / 纯文档 / 机械改动 → 直接做。需求已明确、范围很小 → 压缩流程(见"降级"),但 Gate 不跳。

## 核心理念

1. **Gate 而非 Stage**:阶段之间有一道**能失败、能 STOP** 的闸。价值在闸,不在线。
2. **双环**:**定义环**(便宜/可逆/纯文档:需求→PRD→规划)与**交付环**(贵/有闸:研发→测试→构建→验证)分开——先把定义吵清楚再进昂贵交付。
3. **最便宜的 kill 前置**:准入(G1)+ 可行性(G2)在写代码前跑。
4. **Dossier 作脊柱**:每 feature 一份 `docs/dossiers/<date>-<slug>.md` 串起全链 + 每道 Gate 裁决 + 状态 → 可追溯、可恢复(任何 session 接手先读它,从断点续)。
5. **人在环**:G1 准入、G2 待拍板、G5 构建/分发、G6 验证 —— 显式 STOP 问用户,不偷偷自治。
6. **反馈环纪律**:长构建(`wails build`)异步;**测试绝不 `| tail`,保真退出码**(AGENTS.md 已立此规);最便宜的闸先跑;不提交生成产物(`frontend/dist`、`build/bin`、`config.json`)。

## 流水线总览

```
        ┌────────── 定义环(便宜 · 可逆 · 纯文档)──────────┐
用户需求 ─▶ S0 Intake ─▶ S1 Discovery ─▶[G1 准入]─▶ S2 PRD ─▶ S3 规划 ─▶[G2 可行性]
        建 Dossier   读 system-map+grep   四问/范围   docs/prd  docs/plans  (待拍板 STOP 问人)
                                                                              │
        ┌────────── 交付环(贵 · 有闸)──────────────────────────────────────┘
        ▼
S4 分解 ─▶ S5 实现 ─▶[G3 测试闸]─▶[G4 评审闸]─▶ S6 构建 ─▶[G5 构建健康]─▶ S7 验证 ─▶[G6 验证闸]─▶ S8 沉淀
 任务/契约  Go/Vue   go test+vue-tsc  代码+安全评审 wails build  app 能起+smoke  真跑路径  用户确认   更新 system-map+memory
                    +smoke(真红回 S5) (BLOCK 回 S5)  +平台脚本                          (FAIL 回 S5)
```

每道 Gate 失败 → 回指定上游阶段,**绝不带红/带 BLOCK 往下走**。

## 阶段 × Gate(做什么 / 复用 / 产出 / Gate)

### S0 · Intake
把用户需求**逐字**记进新建 Dossier(`docs/dossiers/<date>-<slug>.md`,模板见同目录 `dossier-template.md`);补四问 Q1「谁用、解决什么、现在怎么绕过」。需求模糊到判不了范围 → 先问 2-3 问。

### S1 · Discovery(现状勘察)
写 PRD 前把需求触及的子系统**现状**摸清。**先读 `docs/system-map/INDEX.md`** 秒懂全景,再 grep `backend/`(app/services/utils/request/downloader)与 `frontend/src/`(views/components/stores)。**连接 > 新建**:已有 service/store/组件能复用就别重写。硬约束在此暴露(Wails 绑定边界、`frontend/wailsjs/` 不可手改、KBASE 鉴权、版权/个人用途)。产出写进 Dossier「Discovery」节(带 file:line)。

### G1 · 准入 Gate — **人在环 STOP**
过准入卡:① 映射到 dedao 客户端一个真实用户可见能力(下载/课程/听书/电子书/知识城邦/书库 chat…)?② `smallest_end_to_end_slice` 是什么?③ 是否需要先写一页 spec(新用户可见行为 / 新 Wails 绑定 / 新 CLI 接口 / 新外部请求面)?**裁决 PASS / REFRAME(范围不符,改写需求回 S0)/ REJECT(映射不到产品,不做,记 Dossier)**。裁决给用户确认后再进 S2。

### S2 · PRD
合成 PRD(`docs/prd/<date>-<slug>.md`):四问 + ASCII 数据流(用户操作 → Vue 组件 → Wails 绑定方法 → backend service → 外部请求/下载/落盘);声明边界、不变量、验收、待拍板。已有设计文档(`docs/plans/`)**引用不重述**。

### S3 · 规划
落成**分阶段 + 四问 + ASCII + 验收 + 测试点**的计划(`docs/plans/<date>-<slug>.md`)。重排序:数据/底座先行、最便宜 felt-value 先出、长杆(新 Wails 绑定/新外部协议)先 spike de-risk。

### G2 · 可行性 Gate — **人在环 STOP(待拍板)**
进昂贵交付前压测规划:Wails/Go/Vue 实现可行性(诚实:做不到的别承诺)、版权/个人用途红线、安全(KBASE token、cookie 不入库)、范围排序。可用 `codex` challenge 或自查清单做对抗。硬阻断焊进规划;**待拍板分叉 STOP 问用户**。PASS 后进 S4。

### S4 · 需求分解
拆成具体任务(每任务链接回规划某步);定**前后端契约**(Wails 绑定方法签名 ↔ Vue 调用 shape —— 注意 `frontend/wailsjs/` 是生成的,改 Go 绑定后要 `wails generate module` 或 `wails dev` 重生成,别手改)。**并发检查**:`git fetch` + 看有没有别的分支/PR 抢先。从 `origin/main` 干净起分支。

### S5 · 实现
直接实现(无 agent 团队;大改可分 backend‖frontend 两线)。Go 用 `gofmt`,导出名只给 Wails/API 面;Vue 组件 PascalCase,Pinia store 在 `frontend/src/stores/`,跟邻近 TS 风格。改 Go 绑定 → 重生成 `wailsjs`,别手改生成产物。

### G3 · 测试 Gate
- **Go**:先跑窄测(改了哪个包先 `go test ./backend/xxx/...`),再 `go test ./...`。
- **前端**:`cd frontend && npm run build`(= `vue-tsc --noEmit` 类型闸 + Vite build);行为用 smoke:`node frontend/scripts/markdown-render-smoke.mjs` / `book-knowledge-ui-smoke.mjs`(或在 `frontend/scripts/` 加小脚本)。
- **铁律**:**绝不 `go test ... | tail`**(tail 永远 exit 0,吞掉失败 → 带红上线);直读结果行或 `set -o pipefail`。
- **裁决**:真红 → 回 S5;**带红绝不进 S6**。

### G4 · 评审 Gate
碰外部请求/鉴权(`cmd/kbase-server` 的 `/api/*` KBASE_AUTH_TOKEN)、cookie/token 处理、下载/落盘路径、版权敏感内容 → 必经一次评审(`codex review` 或自查)。安全清单见 AGENTS.md §Security。**BLOCK 回 S5。**

### S6 · 构建("上线"=桌面分发)
`wails build --clean`(或平台脚本 `./scripts/build-macos.sh` / `build-macos-arm.sh` / `build-windows.sh`)。**长构建异步跑**,不串行干等。不提交 `build/bin`、`frontend/dist`。产出物路径 + 版本记进 Dossier。

### G5 · 构建健康 Gate
构建产物**能启动**(本机起一下 `wails dev` 或运行 build 产物)+ smoke 脚本绿 + 关键路径手验(登录/下载/渲染)。失败 → 回 S5。

### S7 · 验证
用**真实使用路径**验证需求达成(实际点一遍:登录→进课程→下载→生成 PDF/Markdown/MP3,或新功能的真路径)。结果记 Dossier。

### G6 · 验证 Gate — **人在环**
需求对用户真成立 → **PASS(回路闭合)**;不成立 → 记缺口 → 回 S5。发布类必经用户确认。

### S8 · 沉淀
新坑沉淀回本 skill / memory;**改了代码结构必跑 system-map 生成器更新 `docs/_generated/system-map.json` 并提交**(否则 system-map 漂移闸红,见 `system-map` skill);动了某 facet 域则更新该叙事文档 + bump `last-reviewed`。Dossier 状态 → shipped。

## 失败即停一览

| Gate | 失败信号 | 动作 |
|---|---|---|
| G1 准入 | 映射不到产品能力 | REJECT/REFRAME,记 Dossier,STOP 问人 |
| G2 可行性 | Wails/版权/安全不可行 | 焊进规划 reframe;待拍板 STOP |
| G3 测试 | `go test`/`vue-tsc`/smoke 真红 | 回 S5;**绝不 `\| tail` 吞退出码** |
| G4 评审 | 安全/版权 BLOCK | 回 S5 整改 + 复审 |
| G5 构建健康 | 起不来 / smoke 红 | 回 S5 |
| G6 验证 | 真实路径不达成 | 记缺口 → 回 S5 |

## 降级与并行

- **降级**:小需求压缩——跳 S1 大 discovery(直接小范围 grep)、PRD+规划合一页、直接实现。但 **G3/G4/G5 不可跳**。
- **并行**:Discovery readers 并行;backend‖frontend 实现可并行;长构建异步触发不等。
- **可恢复**:中断后读 Dossier「当前阶段 + 状态」从断点续。

## 演进
本 skill 是演进系统。每跑完一条 feature,把新坑(新 Gate 失败模式 / 新复用机会 / 新反馈环纪律)沉淀回本文件。
