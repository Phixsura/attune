# attune 服务 observability 配置

This is **attune 自己拥有的** observability declaration —
scrape target + Grafana dashboards. casceneai 平台共享 stack
(infra/observability) 会在部署时把这里的文件 sync 过去，所以 attune
作为一个独立服务可以完整地"打包带 obs 配置"。

## 布局

```
attune/observability/
├── targets.yaml        # VM 用 file_sd_configs 读这个 (作为 attune.yaml)
├── dashboards/         # 每个 *.json 都会 sync 到 Grafana 的 attune/ 文件夹
│   └── attune-overview.json
└── README.md
```

## 自己增减 dashboard

直接放 `dashboards/<name>.json`。命名建议带 service 前缀避免与其他服务冲突。

## 自己加 scrape target

改 `targets.yaml`。Wave 3+ 多 attune 实例时这里增条目。

## 部署

部署到生产时 ops 运行：

```bash
infra/observability/sync-from-services.sh   # 把每个服务的 obs config 同步过去
```

VM + Grafana 都会 auto-reload（不重启容器）。
