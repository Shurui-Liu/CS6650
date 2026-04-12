#!/usr/bin/env bash
# Usage: ./scripts/deploy.sh <ec2-public-ip> <ecr-repo-url> [ssh-key-path]
# Builds the Docker image, pushes to ECR, and restarts the container on EC2.
set -euo pipefail

EC2_IP="${1:?EC2 public IP required}"
ECR_REPO="${2:?ECR repo URL required}"
SSH_KEY="${3:-~/.ssh/id_rsa}"
REGION="${AWS_REGION:-us-east-1}"

echo "==> Authenticating with ECR..."
aws ecr get-login-password --region "$REGION" \
  | docker login --username AWS --password-stdin "$ECR_REPO"

echo "==> Building image (linux/amd64)..."
docker buildx build \
  --platform linux/amd64 \
  --tag "${ECR_REPO}:latest" \
  --push \
  .

echo "==> Deploying to EC2 ${EC2_IP}..."
ssh -i "$SSH_KEY" \
    -o StrictHostKeyChecking=no \
    "ec2-user@${EC2_IP}" \
    "bash -s" <<REMOTE
set -e
aws ecr get-login-password --region ${REGION} \
  | docker login --username AWS --password-stdin ${ECR_REPO}
docker pull ${ECR_REPO}:latest
docker stop album-store 2>/dev/null || true
docker rm   album-store 2>/dev/null || true
docker run -d --name album-store \
  --env-file /opt/album-store.env \
  -p 8080:8080 \
  --restart unless-stopped \
  ${ECR_REPO}:latest
echo "==> Container started"
docker ps --filter name=album-store
REMOTE

echo "==> Done. Service: http://${EC2_IP}:8080/health"
