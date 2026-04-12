#!/bin/bash
set -e

# ── Docker ────────────────────────────────────────────────────────────────────
yum update -y
yum install -y docker pgbouncer
systemctl enable docker
systemctl start docker

# ── ECR login (instance role — no static keys) ────────────────────────────────
aws ecr get-login-password --region ${region} \
  | docker login --username AWS \
    --password-stdin ${ecr_repo}

# ── PgBouncer (Change 5) ──────────────────────────────────────────────────────
# pool_mode = transaction is required; session mode is incompatible with pgxpool.
cat > /etc/pgbouncer/pgbouncer.ini <<'PEOF'
[databases]
albumstore = host=${rds_host} port=5432 dbname=albumstore

[pgbouncer]
listen_port = 5432
listen_addr = 127.0.0.1
auth_type   = md5
auth_file   = /etc/pgbouncer/userlist.txt
pool_mode   = transaction
max_client_conn  = 200
default_pool_size = 20
min_pool_size    = 5
reserve_pool_size = 5
log_connections    = 0
log_disconnections = 0
PEOF

sed -i "s|\${rds_host}|${rds_host}|g" /etc/pgbouncer/pgbouncer.ini

echo '"albumuser" "${db_password}"' > /etc/pgbouncer/userlist.txt

systemctl enable pgbouncer
systemctl start pgbouncer

# ── Environment file ──────────────────────────────────────────────────────────
# No PORT — this is a pure worker process with no HTTP listener.
# No REDIS_ADDR — workers do not use the cache.
cat > /opt/album-store.env <<EOF
DATABASE_URL=${db_url}
DATABASE_READER_URL=postgres://albumuser:${db_password}@${rds_reader_addr}:5432/albumstore
SQS_QUEUE_URL=${sqs_url}
S3_BUCKET=${s3_bucket}
S3_BASE_URL=${s3_base_url}
AWS_REGION=${region}
WORKER_CONCURRENCY=20
EOF

# ── Pull and run ──────────────────────────────────────────────────────────────
docker pull ${ecr_repo}:latest || true
docker run -d --name album-store-worker \
  --env-file /opt/album-store.env \
  --restart unless-stopped \
  ${ecr_repo}:latest
