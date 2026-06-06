# Attune Console

Stage B 自服务控制台 SPA。物理独立于主仓 pnpm workspace
（见 [`attune/docs/2026-05-15-console-tech-stack.md`](../docs/2026-05-15-console-tech-stack.md)）。

## Stack

Vite 6 · React 19 · TS 5.9 · TanStack Router · TanStack Query 5 · shadcn/ui
+ Radix · Tailwind 4 · react-hook-form + zod 4 · react-i18next ·
date-fns 3 · Biome 2.

## 跑

```bash
pnpm install
pnpm gen:proto     # 触发仓库根的 `make proto` → 重新生成 src/proto/**
pnpm dev           # :10092；/fb/v1 proxy 到本地 attune :8090
```

## API 契约同步

`.proto` 文件(`../proto/attune/v1/*.proto`)是单一真理源(#19, CLAUDE.md §11):

```bash
pnpm gen:proto                  # 等价于 cd .. && make proto
git diff src/proto/             # 必须无 diff，否则 CI proto-sync 拒绝
```

ts-proto 把 proto 转成 TS 类型并落到 `src/proto/attune/v1/*.ts`(只读)。各
feature 的 `src/features/<x>/api/*.ts` 把这些类型按消费场景重导出。

## 架构边界

`pnpm arch` 跑 dependency-cruiser,强制 bulletproof-react 风格的单向 import:
- `shared → features → app` 一向流
- 禁跨 feature 互引
- 无循环依赖

详见 `.dependency-cruiser.cjs` 与 `docs/proposals/2026-06-06-feature-organization.md`。
