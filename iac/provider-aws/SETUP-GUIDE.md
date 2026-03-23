# AWS Setup Guide - Step by Step

This guide walks you through setting up AWS from scratch for E2B deployment.

## Step 1: Secure Your Root Account (Do This First!)

Your root account has unlimited access. You should:

### 1.1 Enable MFA on Root Account
1. Log into AWS Console as root: https://console.aws.amazon.com/
2. Click your account name (top right) → **Security credentials**
3. Under "Multi-factor authentication (MFA)", click **Assign MFA device**
4. Choose "Authenticator app" and follow the prompts
5. Use Google Authenticator, Authy, or similar

### 1.2 Create an Admin IAM User (Use This Instead of Root)

**Never use root for daily work!** Create an admin user:

1. Go to **IAM** service: https://console.aws.amazon.com/iam/
2. Click **Users** → **Create user**
3. User name: `e2b-admin` (or your preferred name)
4. Check ✅ **Provide user access to the AWS Management Console**
5. Select **I want to create an IAM user**
6. Set a strong password
7. Click **Next**

### 1.3 Attach Admin Permissions
1. Select **Attach policies directly**
2. Search and check ✅ **AdministratorAccess**
3. Click **Next** → **Create user**
4. **IMPORTANT**: Download or save the sign-in URL, username, and password

### 1.4 Create Access Keys for CLI
1. Click on your new user `e2b-admin`
2. Go to **Security credentials** tab
3. Under "Access keys", click **Create access key**
4. Select **Command Line Interface (CLI)**
5. Check the confirmation box, click **Next**
6. Click **Create access key**
7. **IMPORTANT**: Download the CSV or copy both:
   - Access key ID: `AKIA...`
   - Secret access key: `wJalr...`

   **You won't see the secret again!**

---

## Step 2: Install AWS CLI

### Windows (PowerShell as Administrator)
```powershell
# Download and run installer
msiexec.exe /i https://awscli.amazonaws.com/AWSCLIV2.msi

# Or use winget
winget install Amazon.AWSCLI
```

### macOS
```bash
# Using Homebrew
brew install awscli

# Or download installer
curl "https://awscli.amazonaws.com/AWSCLIV2.pkg" -o "AWSCLIV2.pkg"
sudo installer -pkg AWSCLIV2.pkg -target /
```

### Linux
```bash
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

### Verify Installation
```bash
aws --version
# Should show: aws-cli/2.x.x ...
```

---

## Step 3: Configure AWS CLI

Run this command and enter your access keys:

```bash
aws configure
```

You'll be prompted for:
```
AWS Access Key ID [None]: AKIA... (paste your access key)
AWS Secret Access Key [None]: wJalr... (paste your secret key)
Default region name [None]: us-east-1
Default output format [None]: json
```

### Verify It Works
```bash
# Should show your account info
aws sts get-caller-identity
```

Expected output:
```json
{
    "UserId": "AIDA...",
    "Account": "123456789012",
    "Arn": "arn:aws:iam::123456789012:user/e2b-admin"
}
```

**Save your Account ID** (the 12-digit number) - you'll need it later.

---

## Step 4: Install Terraform

### Windows (PowerShell)
```powershell
# Using winget
winget install HashiCorp.Terraform

# Or using Chocolatey
choco install terraform
```

### macOS
```bash
brew install terraform
```

### Linux
```bash
# Add HashiCorp GPG key and repo
wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
sudo apt update && sudo apt install terraform
```

### Verify
```bash
terraform --version
# Should show: Terraform v1.x.x
```

---

## Step 5: Choose Your Region and Domain

### 5.1 Choose AWS Region
Pick a region close to your users. Common choices:
- `us-east-1` (N. Virginia) - Most services, cheapest
- `us-west-2` (Oregon) - Good for West Coast
- `eu-west-1` (Ireland) - Europe
- `ap-southeast-1` (Singapore) - Asia

### 5.2 Domain Setup Options

**Option A: Buy a new domain in Route 53**
1. Go to Route 53: https://console.aws.amazon.com/route53/
2. Click **Register domain**
3. Search and purchase (e.g., `mycompany-e2b.com`)

**Option B: Use existing domain with Route 53**
1. Go to Route 53 → **Hosted zones** → **Create hosted zone**
2. Enter your domain name
3. Copy the NS records shown
4. Update your domain registrar's nameservers to these values

**Option C: Use Cloudflare (if you already have it)**
- Keep your domain on Cloudflare
- We'll configure Terraform to use Cloudflare DNS

---

## Step 6: Create ACM Certificate (SSL/TLS)

Your domain needs HTTPS. Create a certificate:

1. Go to ACM: https://console.aws.amazon.com/acm/
2. **IMPORTANT**: Make sure you're in your chosen region (top right)
3. Click **Request certificate**
4. Select **Request a public certificate** → **Next**
5. Enter domain names:
   - `e2b.yourdomain.com` (or whatever subdomain)
   - `*.e2b.yourdomain.com` (wildcard for subdomains)
6. Validation method: **DNS validation** (recommended)
7. Click **Request**

### Validate the Certificate
1. Click on your certificate
2. Click **Create records in Route 53** (if using Route 53)
   - Or copy the CNAME records to your DNS provider
3. Wait for status to change to **Issued** (can take 5-30 minutes)
4. **Copy the Certificate ARN** - you'll need it later:
   ```
   arn:aws:acm:us-east-1:123456789012:certificate/abc123-def456-...
   ```

---

## Step 7: Find Ubuntu AMI IDs

You need Ubuntu 22.04 LTS AMI IDs for your region:

```bash
# Find latest Ubuntu 22.04 AMI for your region
aws ec2 describe-images \
  --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
  --query 'sort_by(Images, &CreationDate)[-1].ImageId' \
  --output text \
  --region us-east-1
```

This will output something like:
```
ami-0c7217cdde317cfec
```

**Save this AMI ID** - use it for both control plane and workers.

---

## Step 8: Create SSH Key Pair (Optional but Recommended)

For debugging access to EC2 instances:

```bash
# Create key pair
aws ec2 create-key-pair \
  --key-name e2b-key \
  --query 'KeyMaterial' \
  --output text \
  --region us-east-1 > e2b-key.pem

# Secure the key file
chmod 400 e2b-key.pem

# On Windows PowerShell:
# icacls e2b-key.pem /inheritance:r /grant:r "$($env:USERNAME):R"
```

Keep `e2b-key.pem` safe - you'll need it to SSH into instances.

---

## Step 9: Create Terraform State Backend

Now let's create the S3 bucket and DynamoDB table for Terraform state:

```bash
cd iac/provider-aws/state

terraform init

# Replace with YOUR unique bucket name (must be globally unique)
terraform apply \
  -var="aws_region=us-east-1" \
  -var="state_bucket_name=e2b-terraform-state-YOURNAME-123" \
  -var="lock_table_name=e2b-terraform-locks" \
  -var='tags={"project":"e2b","env":"prod"}'
```

Type `yes` when prompted.

---

## Step 10: Deploy E2B Infrastructure

### 10.1 Initialize Main Terraform

```bash
cd iac/provider-aws

terraform init \
  -backend-config="bucket=e2b-terraform-state-YOURNAME-123" \
  -backend-config="key=terraform/e2b/state" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=e2b-terraform-locks"
```

### 10.2 Create terraform.tfvars

Create a file `terraform.tfvars` with your configuration:

```hcl
# Core
aws_region  = "us-east-1"
prefix      = "e2b"
environment = "prod"

# Domain
domain_name         = "e2b.yourdomain.com"
acm_certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/..."

# DNS (choose one)
create_route53_record = true
route53_zone_id       = "Z1234567890ABC"  # Your hosted zone ID

# Or for Cloudflare:
# create_route53_record = false
# cloudflare_api_token  = "your-cloudflare-token"
# cloudflare_zone_id    = "your-zone-id"

# Networking
availability_zones   = ["us-east-1a", "us-east-1b"]
public_subnet_cidrs  = ["10.10.0.0/24", "10.10.1.0/24"]
private_subnet_cidrs = ["10.10.10.0/24", "10.10.11.0/24"]

# EC2 Instances
control_plane_ami_id = "ami-0c7217cdde317cfec"  # Your Ubuntu AMI
worker_ami_id        = "ami-0c7217cdde317cfec"  # Same AMI
ssh_key_name         = "e2b-key"                 # Your key pair name

# Instance sizes (start small, scale later)
control_plane_instance_type = "c6a.large"
control_plane_min_size      = 1
control_plane_desired_capacity = 1
control_plane_max_size      = 2

worker_instance_type     = "c6a.xlarge"
worker_min_size          = 1
worker_desired_capacity  = 1
worker_max_size          = 3

# Data layer (RDS + ElastiCache)
create_rds         = true
rds_instance_class = "db.t3.medium"
rds_multi_az       = false  # Set to true for production

create_elasticache       = true
redis_node_type          = "cache.t3.medium"
redis_num_cache_clusters = 1  # Set to 2+ for production

# Observability
create_observability = true
alarm_email          = "your-email@example.com"

# Tags
tags = {
  Project     = "e2b"
  Environment = "prod"
  ManagedBy   = "terraform"
}
```

### 10.3 Plan and Apply

```bash
# See what will be created
terraform plan

# Deploy (this takes 10-20 minutes)
terraform apply
```

Type `yes` when prompted.

---

## Step 11: Post-Deployment

After Terraform completes, you'll see outputs like:

```
alb_dns_name = "e2b-prod-api-123456.us-east-1.elb.amazonaws.com"
ecr_repositories = {
  orchestration = "123456789012.dkr.ecr.us-east-1.amazonaws.com/e2b"
}
rds_endpoint = "e2b-prod-postgres.abc123.us-east-1.rds.amazonaws.com:5432"
redis_endpoint = "e2b-prod-redis.abc123.cache.amazonaws.com"
```

### Next Steps:
1. Update your DNS to point to the ALB
2. Build and push Docker images to ECR
3. Deploy Nomad jobs
4. Configure Supabase/ClickHouse (external services)

---

## Quick Reference: AWS Console Links

- IAM: https://console.aws.amazon.com/iam/
- EC2: https://console.aws.amazon.com/ec2/
- VPC: https://console.aws.amazon.com/vpc/
- RDS: https://console.aws.amazon.com/rds/
- ElastiCache: https://console.aws.amazon.com/elasticache/
- S3: https://console.aws.amazon.com/s3/
- Secrets Manager: https://console.aws.amazon.com/secretsmanager/
- CloudWatch: https://console.aws.amazon.com/cloudwatch/
- Route 53: https://console.aws.amazon.com/route53/
- ACM: https://console.aws.amazon.com/acm/

---

## Troubleshooting

### "Access Denied" errors
- Verify AWS CLI is configured: `aws sts get-caller-identity`
- Check you're using the admin user, not root

### Certificate stuck in "Pending validation"
- Verify DNS records are created correctly
- Wait up to 30 minutes for propagation

### Terraform state errors
- Ensure S3 bucket exists and you have access
- Check DynamoDB table exists

### Need Help?
- AWS docs: https://docs.aws.amazon.com/
- Terraform AWS docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs
