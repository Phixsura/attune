# Attune Console

Stage B 自服务控制台 SPA。物理独立于主仓 pnpm workspace。

## Stack

Vite 6 · React 19 · TS 5.9 · TanStack Router · TanStack Query 5 · shadcn/ui
+ Radix · Tailwind 4 · react-hook-form + zod 4 · react-i18next ·
date-fns 3 · Biome 2.

## 跑

```bash
pnpm install
pnpm gen:api       # openapi.yaml → src/api/types.ts
pnpm dev           # :10092；/fb/v1 proxy 到本地 attune :8090
```

## API 契约同步

```bash
pnpm gen:api
git diff src/api/types.ts   # 必须无 diff，否则 CI 拒绝
```

`../internal/handlers/console/openapi.yaml` 是单一真理源。
