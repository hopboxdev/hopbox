# Hopbox Grafana Dashboards

Two dashboards ship with hopbox:

- `server-overview.json` — users/boxes/containers/sessions, active sessions over time, build-duration quantiles, Go runtime.
- `box-details.json`    — per-box CPU / memory / network / disk. Uses `$user` and `$box` template variables.

## Import

1. Grafana → Dashboards → New → Import.
2. Upload JSON file.
3. Select the `Prometheus` datasource added via the monitoring stack.
4. Save.

Repeat for each dashboard.
