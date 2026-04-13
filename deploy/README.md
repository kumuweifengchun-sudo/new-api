# Docker Deployment With Caddy

This directory contains Docker deployment templates for:

- `master/`: the single control-plane node (`NODE_TYPE=master`)
- `slave/`: edge/API nodes (`NODE_TYPE=slave`)
- `gateway/`: optional Caddy load balancer in front of slave nodes

Notes:

- Use exactly one `master`.
- All nodes must share the same `SESSION_SECRET`.
- All nodes must share the same `CRYPTO_SECRET`.
- All nodes must point to the same `SQL_DSN`.
- All nodes must point to the same `REDIS_CONN_STRING`.
- Do not use SQLite for multi-node deployment.

Typical startup:

```bash
cd deploy/master
cp .env.example .env
docker compose up -d --build

cd ../slave
cp .env.example .env
docker compose up -d --build
```

Optional gateway:

```bash
cd deploy/gateway
cp .env.example .env
docker compose up -d
```
