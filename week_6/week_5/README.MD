# E-commerce Product API - README

A RESTful Product API deployed on AWS ECS with Terraform infrastructure as code.

---

## 📋 Table of Contents
- [Quick Start Deployment](#quick-start-deployment)
- [Prerequisites](#prerequisites)
- [Step-by-Step Deployment Guide](#step-by-step-deployment-guide)
- [Sending Requests to API](#sending-requests-to-api)
- [API Response Code Examples](#api-response-code-examples)
- [Project Structure](#project-structure)
- [Cleanup](#cleanup)

---

## Quick Start Deployment

**Total Time:** 15-20 minutes

1. Install prerequisites (AWS CLI, Terraform, Docker, Git)
2. Get AWS Academy credentials
3. Clone repo and configure AWS
4. Run `terraform apply`
5. Build and push Docker image
6. Test your API!

---

## Prerequisites

### Required Software

Install these before starting:

| Tool | Download Link | Verify Command |
|------|--------------|----------------|
| **AWS CLI** | https://aws.amazon.com/cli/ | `aws --version` |
| **Terraform** | https://www.terraform.io/downloads | `terraform --version` |
| **Docker Desktop** | https://www.docker.com/products/docker-desktop/ | `docker --version` |
| **Git** | https://git-scm.com/downloads | `git --version` |

### AWS Academy Learner Lab Access

You'll need access to AWS Academy Learner Lab for this course.

---

## Step-by-Step Deployment Guide

### Step 1: Get AWS Credentials

1. **Log into AWS Academy** and open your Learner Lab
2. Click **"Start Lab"** and wait for 🟢 (green indicator)
3. Click **"AWS Details"** → **"Show"** next to **"AWS CLI:"**
4. **Copy the credentials** (you'll need them in Step 3)

Example credentials format:
```ini
[default]
aws_access_key_id=ASIA...
aws_secret_access_key=...
aws_session_token=IQoJb3JpZ2luX2VjE...
```

⚠️ **Note:** Credentials expire when your lab session ends (~4 hours)

---

### Step 2: Clone Repository
```bash
# Clone the repository
git clone https://github.com/YOUR_USERNAME/CS6650_2b_demo.git

# Navigate to project
cd CS6650_2b_demo
```

---

### Step 3: Configure AWS Credentials

**Create/edit AWS credentials file:**

**Mac/Linux:**
```bash
mkdir -p ~/.aws
nano ~/.aws/credentials
```

**Windows PowerShell:**
```powershell
New-Item -ItemType Directory -Force -Path $env:USERPROFILE\.aws
notepad $env:USERPROFILE\.aws\credentials
```

**Paste your AWS Academy credentials:**
```ini
[default]
aws_access_key_id=ASIA...YOUR_KEY
aws_secret_access_key=...YOUR_SECRET
aws_session_token=IQoJb3...YOUR_TOKEN
```

**Create AWS config file:**

**Mac/Linux:**
```bash
nano ~/.aws/config
```

**Windows:**
```powershell
notepad $env:USERPROFILE\.aws\config
```

**Paste:**
```ini
[default]
region=us-west-2
output=json
```

**Verify credentials work:**
```bash
aws sts get-caller-identity
```

You should see your AWS account info ✅

---

### Step 4: Configure Terraform Variables
```bash
cd terraform
```

**Create `terraform.tfvars` file:**

**Mac/Linux:**
```bash
nano terraform.tfvars
```

**Windows:**
```powershell
notepad terraform.tfvars
```

**Paste this content:**
```hcl
# AWS Configuration
aws_region = "us-west-2"

# Project Settings
ecr_repository_name = "ecommerce-product-api"
service_name        = "product-api-service"

# Container Settings
container_port   = 8080
container_cpu    = 256
container_memory = 512

# ECS Settings
ecs_count = 1

# Logs
log_retention_days = 7
```

**Save and close.**

---

### Step 5: Deploy Infrastructure
```bash
# Initialize Terraform
terraform init

# Preview changes
terraform plan

# Deploy (takes 5-10 minutes)
terraform apply
```

**Type `yes` when prompted.**

This creates:
- ✅ ECR repository
- ✅ ECS Fargate cluster
- ✅ Application Load Balancer
- ✅ Security groups
- ✅ IAM roles
- ✅ CloudWatch logs

---

### Step 6: Build and Push Docker Image

**Set environment variables:**

**Mac/Linux:**
```bash
export ECR_REPO=$(terraform output -raw ecr_repository_url)
export AWS_REGION=$(terraform output -raw aws_region)
```

**Windows PowerShell:**
```powershell
$ECR_REPO = terraform output -raw ecr_repository_url
$AWS_REGION = terraform output -raw aws_region
```

**Login to ECR:**

**Mac/Linux:**
```bash
aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin $ECR_REPO
```

**Windows:**
```powershell
aws ecr get-login-password --region $AWS_REGION | docker login --username AWS --password-stdin $ECR_REPO
```

**Build and push image:**

**Mac/Linux:**
```bash
cd ../src
docker build -t product-api:latest .
docker tag product-api:latest $ECR_REPO:latest
docker push $ECR_REPO:latest
```

**Windows:**
```powershell
cd ../src
docker build -t product-api:latest .
docker tag product-api:latest ${ECR_REPO}:latest
docker push ${ECR_REPO}:latest
```

---

### Step 7: Deploy to ECS

**Mac/Linux:**
```bash
cd ../terraform
CLUSTER=$(terraform output -raw ecs_cluster_name)
SERVICE=$(terraform output -raw ecs_service_name)

aws ecs update-service \
  --cluster $CLUSTER \
  --service $SERVICE \
  --force-new-deployment \
  --region $AWS_REGION
```

**Windows:**
```powershell
cd ../terraform
$CLUSTER = terraform output -raw ecs_cluster_name
$SERVICE = terraform output -raw ecs_service_name

aws ecs update-service `
  --cluster $CLUSTER `
  --service $SERVICE `
  --force-new-deployment `
  --region $AWS_REGION
```

**Wait 2-3 minutes** for deployment to complete.

---

### Step 8: Get Your API Endpoint
```bash
terraform output load_balancer_url
```

**Example output:**
```
product-api-service-alb-1747584278.us-west-2.elb.amazonaws.com
```

**Save this URL** - this is your API endpoint! 🎉

---

## Sending Requests to API

### Save Your Load Balancer URL

**Mac/Linux:**
```bash
export LB_URL=$(cd terraform && terraform output -raw load_balancer_url)
```

**Windows:**
```powershell
cd terraform
$LB_URL = terraform output -raw load_balancer_url
cd ..
```

### Test Health Endpoint

**Mac/Linux:**
```bash
curl http://$LB_URL/health
```

**Windows:**
```powershell
curl.exe http://$LB_URL/health
```

**Expected:** `{"status":"healthy"}` ✅

### Create a Product (POST)

**Mac/Linux:**
```bash
curl -X POST http://$LB_URL/products/1/details \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": 1,
    "sku": "LAPTOP-001",
    "manufacturer": "TechCorp",
    "category_id": 5,
    "weight": 2000,
    "some_other_id": 999
  }'
```

**Windows:**
```powershell
curl.exe -X POST "http://$LB_URL/products/1/details" `
  -H "Content-Type: application/json" `
  -d '{\"product_id\":1,\"sku\":\"LAPTOP-001\",\"manufacturer\":\"TechCorp\",\"category_id\":5,\"weight\":2000,\"some_other_id\":999}'
```

**Expected:** No output (204 No Content = success)

### Get a Product (GET)

**Mac/Linux:**
```bash
curl http://$LB_URL/products/1
```

**Windows:**
```powershell
curl.exe http://$LB_URL/products/1
```

**Expected:**
```json
{
  "product_id": 1,
  "sku": "LAPTOP-001",
  "manufacturer": "TechCorp",
  "category_id": 5,
  "weight": 2000,
  "some_other_id": 999
}
```

---

## API Response Code Examples

Below are screenshots demonstrating all HTTP response codes returned by the API.

### 1. Health Check - 200 OK ✅

**Command:**
```bash
curl -i http://$LB_URL/health
```

**Screenshot:**
![Alt Text](./images_screenshots/Screenshot%202026-02-16%20200.png)
---

### 2. Create Product - 204 No Content ✅

**Command:**
```bash
curl -i -X POST http://$LB_URL/products/1/details \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": 1,
    "sku": "LAPTOP-001",
    "manufacturer": "TechCorp",
    "category_id": 5,
    "weight": 2000,
    "some_other_id": 999
  }'
```

**Screenshot:**

![Alt Text](./images_screenshots/Screenshot%202026-02-16%20204.png)

---

### 3. Get Product - 200 OK ✅

**Command:**
```bash
curl -i http://$LB_URL/products/1
```

![Alt Text](./images_screenshots/Screenshot%20200.png)

---

### 4. Product Not Found - 404 Not Found ❌

**Command:**
```bash
curl -i http://$LB_URL/products/999
```

**Screenshot:**

![Alt Text](./images_screenshots/Screenshot%20404.png)

**Expected Response:**
```json
{
  "error": "NOT_FOUND",
  "message": "Product not found",
  "details": "Product with ID 999 does not exist"
}
```

---

### 5. Invalid ID - 400 Bad Request ❌
**Screenshot:**

![Alt Text](./images_screenshots/Screenshot%20400.png)

**Expected Response:**
```json
{
  "error": "INVALID_INPUT",
  "message": "Invalid product data",
  "details": "sku must be between 1 and 100 characters"
}
```

---


---

### Response Code Summary

| Status Code | Scenario | Example |
|-------------|----------|---------|
| **200 OK** | Successful GET | Health check, retrieve product |
| **204 No Content** | Successful POST | Create/update product |
| **400 Bad Request** | Validation error | Empty SKU, negative values, ID mismatch, invalid JSON |
| **404 Not Found** | Resource not found | Product doesn't exist |

---

## Project Structure
```
CS6650_2b_demo/
├── src/                          # 📁 APPLICATION CODE
│   ├── main.go                   # ⭐ Go API implementation
│   │                             #    - Product endpoints (GET, POST)
│   │                             #    - In-memory HashMap storage
│   │                             #    - Input validation
│   │                             #    - Health check
│   │
│   ├── Dockerfile                # ⭐ Docker container definition
│   │                             #    - Multi-stage build
│   │                             #    - Exposes port 8080
│   │
│   ├── go.mod                    # Go dependencies
│   └── go.sum                    # Dependency checksums
│
├── terraform/                    # 📁 INFRASTRUCTURE CODE
│   ├── main.tf                   # ⭐ Main Terraform config
│   │                             #    - ECR repository
│   │                             #    - ECS cluster, task, service
│   │                             #    - Application Load Balancer
│   │                             #    - Security groups
│   │                             #    - IAM roles
│   │                             #    - CloudWatch logs
│   │
│   ├── variables.tf              # ⭐ Variable declarations
│   ├── terraform.tfvars          # ⚠️  Your values (git ignored)
│   ├── provider.tf               # AWS provider config
│   ├── outputs.tf                # Output definitions
│   │
│   ├── .terraform/               # ⚠️  Cache (git ignored)
│   ├── terraform.tfstate         # ⚠️  State (git ignored)
│   └── terraform.tfstate.backup  # ⚠️  Backup (git ignored)
│
├── .gitignore                    # ⭐ Git exclusions
├── locustfile.py                 # Load testing script
└── README.md                     # This file
```

---

## Files Excluded from Git (.gitignore)

**Important:** The following files are automatically excluded from version control:
```gitignore
# Terraform - Contains sensitive resource IDs
terraform/.terraform/
terraform/terraform.tfstate
terraform/terraform.tfstate.backup
terraform/*.tfvars
terraform/.terraform.lock.hcl

# Sensitive credentials
.env
*.pem
*.key
*.crt
credentials
secrets.json

# Build artifacts
src/main
*.exe

# IDE files
.vscode/
.idea/

# OS files
.DS_Store
Thumbs.db
```

| File/Directory | Reason |
|----------------|--------|
| `terraform.tfstate` | Contains AWS resource IDs and potentially sensitive data |
| `*.tfvars` | Contains your configuration values (AWS region, settings) |
| `.terraform/` | Large cache directory, auto-regenerated |
| `.env`, `*.key`, `*.pem` | Credentials and private keys |
| Build artifacts | Generated files, not source code |

**What IS included in git:**
- ✅ Source code (`src/main.go`)
- ✅ Dockerfile
- ✅ Terraform configuration files (`*.tf`)
- ✅ Documentation (README.md)
- ✅ `.gitignore` itself

---

## Cleanup

**Remove all AWS resources when done:**
```bash
cd terraform
terraform destroy
```

**Type `yes` when prompted.**

This deletes:
- ECS service and tasks
- Load balancer
- ECR repository and images
- Security groups
- IAM roles
- CloudWatch logs

**Cost:** $0 if cleaned up within a few hours (AWS Free Tier)

---

## Quick Reference

### View All Terraform Outputs
```bash
cd terraform
terraform output
```

### Check ECS Service Status
```bash
aws ecs describe-services \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --services $(terraform output -raw ecs_service_name) \
  --region us-west-2
```

### View Application Logs
```bash
aws logs tail $(terraform output -raw log_group_name) \
  --follow \
  --region us-west-2
```

### Re-deploy After Code Changes
```bash
# Rebuild image
cd src
docker build -t product-api:latest .
docker tag product-api:latest $ECR_REPO:latest
docker push $ECR_REPO:latest

# Force new deployment
cd ../terraform
aws ecs update-service \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --service $(terraform output -raw ecs_service_name) \
  --force-new-deployment \
  --region us-west-2
```

---

## Troubleshooting

### AWS Credentials Expired
**Solution:** Get fresh credentials from AWS Academy → Update `~/.aws/credentials`

### ECS Tasks Not Starting
**Solution:** Check logs with `aws logs tail <log-group-name> --follow`

### API Returns 503
**Solution:** Wait 2-3 minutes for ECS tasks to become healthy

### Docker Push Fails
**Solution:** Re-run ECR login command

---

## Authors

- **Shurui Liu** - CS6650 Building Scalable Distributed Systems
- **Institution**: Northeastern University
- **Date**: February 2026

---

## License

MIT License - Educational purposes only

