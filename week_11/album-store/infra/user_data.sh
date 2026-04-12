#!/bin/bash
set -e

# Install Docker
yum update -y
yum install -y docker
systemctl enable docker
systemctl start docker

# ECR login (uses instance role — no static keys needed)
aws ecr get-login-password --region ${region} \
  | docker login --username AWS \
    --password-stdin ${ecr_repo}

# Write env file
cat > /opt/album-store.env <<EOF
DATABASE_URL=${db_url}
SQS_QUEUE_URL=${sqs_url}
S3_BUCKET=${s3_bucket}
S3_BASE_URL=${s3_base_url}
AWS_REGION=${region}
PORT=8080
WORKER_CONCURRENCY=20
EOF

# Pull and run (first deploy; subsequent deploys use deploy.sh)
docker pull ${ecr_repo}:latest || true
docker run -d --name album-store \
  --env-file /opt/album-store.env \
  -p 8080:8080 \
  --restart unless-stopped \
  ${ecr_repo}:latest
