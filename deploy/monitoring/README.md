# Hopbox Monitoring Stack

Prometheus + Grafana for scraping a host-running `hopboxd`.

## Prerequisites

1. Enable the admin server in `config.toml`:
   ```toml
   [admin]
   enabled  = true
   port     = 8080
   username = "admin"
   password = "change-me"
   ```
2. Restart `hopboxd`.

## Bring the stack up

```sh
cd deploy/monitoring
docker compose up -d
```

- Prometheus: http://localhost:9090
- Grafana:    http://localhost:3000  (admin / admin)

Prometheus scrapes `host.docker.internal:8080/metrics`, which reaches the
host-running hopboxd via the `host-gateway` extra host.

## Add the Prometheus datasource in Grafana

1. Log in.
2. Connections → Data sources → Add → Prometheus.
3. URL: `http://prometheus:9090`
4. Save & test.

## Import dashboards

See `../grafana/README.md`.
