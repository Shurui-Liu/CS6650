#!/usr/bin/env bash
# Usage: ./scripts/deploy.sh <ecr-repo-url> [ssh-key-path]
# Builds the Docker image, pushes to ECR, then rolling-restarts the API ASG
# by terminating instances one at a time (ASG replaces each automatically).
set -euo pipefail

ECR_REPO="${1:?ECR repo URL required (terraform output ecr_repo_url)}"
SSH_KEY="${2:-~/.ssh/id_rsa}"
REGION="${AWS_REGION:-us-east-1}"
API_ASG="album-store-api-asg"

echo "==> Authenticating with ECR..."
aws ecr get-login-password --region "$REGION" \
  | docker login --username AWS --password-stdin "$ECR_REPO"

echo "==> Building image (linux/amd64)..."
docker buildx build \
  --platform linux/amd64 \
  --tag "${ECR_REPO}:latest" \
  --push \
  .

echo "==> Rolling restart of ASG: ${API_ASG}..."
INSTANCE_IDS=$(aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names "$API_ASG" \
  --region "$REGION" \
  --query "AutoScalingGroups[0].Instances[*].InstanceId" \
  --output text)

for ID in $INSTANCE_IDS; do
  echo "  Terminating ${ID} (ASG will launch replacement)..."
  aws autoscaling terminate-instance-in-auto-scaling-group \
    --instance-id "$ID" \
    --no-should-decrement-desired-capacity \
    --region "$REGION"
  echo "  Waiting 90s for replacement to become healthy..."
  sleep 90
done

ALB_DNS=$(aws elbv2 describe-load-balancers \
  --names album-store-alb \
  --region "$REGION" \
  --query "LoadBalancers[0].DNSName" \
  --output text 2>/dev/null || echo "unknown")

echo "==> Deploy complete."
echo "    Base URL: http://${ALB_DNS}"
echo "    Health:   http://${ALB_DNS}/health"
