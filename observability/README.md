# attune 服务 observability 配置

This is **attune 自己拥有的** observability declaration —
Prometheus scrape target + Grafana dashboards，都放在这个目录里，所以 attune
作为一个独立服务可以完整地"打包带 obs 配置"部署。

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

把 `targets.yaml` 指给 Prometheus 的 `file_sd_configs`，把 `dashboards/*.json`
导入 Grafana —— 两者都支持热加载，无需重启。
