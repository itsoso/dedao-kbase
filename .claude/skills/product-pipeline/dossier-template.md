---
slug: <YYYY-MM-DD>-<short-slug>
status: intake   # intake | defined | building | testing | verifying | shipped | rejected
current_stage: S0
last-reviewed: <YYYY-MM-DD>
---

# Dossier: <一句话标题>

> 每 feature 一份。任何 session 接手**先读本文件「当前阶段 + 状态」从断点续**。Gate 裁决必须写进来(诚实:REJECT/BLOCK 也写)。

## 用户原话需求(逐字)
> <把用户原话粘进来,不改写>

## 四问(feature-plan)
- **Q1 用户价值**:谁用、解决什么、现在怎么绕过?
- **Q2 边界(What NOT)**:明确不做什么 / 留到下一期 / 会影响哪些现有功能需回归?
- **Q3 最简实现**:最少改几个文件?复用哪个现有 service/store/组件?数据流一句话?反馈环多长?
- **Q4 风险**:外部请求/鉴权?版权/个人用途?新 Wails 绑定(要重生成 wailsjs)?

## ASCII 数据流
```
用户操作 → Vue 组件(哪个) → Wails 绑定方法(backend.App.Xxx) → backend service(哪个) → 外部请求/下载/落盘
```

## 阶段产出物(链接)
| 阶段 | 产出 | 链接 |
|---|---|---|
| S1 Discovery | 现状图 | (本节下方,带 file:line) |
| S2 PRD | docs/prd/... | |
| S3 规划 | docs/plans/... | |
| S5 实现 | 分支 / commit / PR | |
| S6 构建 | wails build 产物 / 版本 | |

## Gate 裁决记录
| Gate | 裁决 | 依据 / 日期 |
|---|---|---|
| G1 准入 | PASS/REFRAME/REJECT | |
| G2 可行性 | PASS / 待拍板项 | |
| G3 测试 | 绿 / 真红→S5 | go test + vue-tsc + smoke 结果行 |
| G4 评审 | GO / BLOCK→S5 | |
| G5 构建健康 | 起得来+smoke 绿 | |
| G6 验证 | PASS(闭合)/ 缺口→S5 | 用户确认 |

## 待拍板决策(STOP 问人)
- <列需要用户拍板的分叉>

## Discovery 现状图(带 file:line)
- <已有什么可复用 / 缺什么 / 硬约束>

## 沉淀(S8)
- 新坑 / system-map 是否更新 / memory 是否写
