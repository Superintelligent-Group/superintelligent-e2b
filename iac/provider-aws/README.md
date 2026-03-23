# AWS Self-Hosted E2B Infrastructure

This folder provides a complete AWS Terraform deployment for self-hosted E2B infrastructure. It mirrors the Google Cloud flow documented in [`self-host.md`](../../self-host.md) but uses native AWS services.

## Architecture Overview

```
                                    ┌─────────────────────────────────────────────────┐
                                    │                    AWS Cloud                     │
                                    │                                                  │
    Internet ──────────────────────►│  ┌─────────────┐                                │
                                    │  │     ALB     │                                │
                                    │  │   (HTTPS)   │                                │
                                    │  └──────┬──────┘                                │
                                    │         │                                        │
                                    │         ▼                                        │
                                    │  ┌─────────────────────────────────────────┐    │
                                    │  │         Control Plane (ASG)              │    │
                                    │  │  ┌─────────┐ ┌─────────┐ ┌─────────┐    │    │
                                    │  │  │  Nomad  │ │ Consul  │ │  API    │    │    │
                                    │  │  │ Server  │ │ Server  │ │ Service │    │    │
                                    │  │  └─────────┘ └─────────┘ └─────────┘    │    │
                                    │  └──────────────────┬──────────────────────┘    │
                                    │                     │                            │
                                    │         ┌───────────┴───────────┐                │
                                    │         ▼                       ▼                │
                                    │  ┌─────────────┐         ┌─────────────┐        │
                                    │  │     RDS     │         │ ElastiCache │        │
                                    │  │ PostgreSQL  │         │    Redis    │        │
                                    │  └─────────────┘         └─────────────┘        │
                                    │                                                  │
                                    │  ┌─────────────────────────────────────────┐    │
                                    │  │           Workers (ASG)                  │    │
                                    │  │  ┌─────────┐ ┌─────────┐ ┌───────────┐  │    │
                                    │  │  │ Nomad   │ │Firecrack│ │Orchestrat │  │    │
                                    │  │  │ Client  │ │er VMs   │ │or         │  │    │
                                    │  │  └─────────┘ └─────────┘ └───────────┘  │    │
                                    │  └─────────────────────────────────────────┘    │
                                    │                                                  │
                                    │  ┌─────────────────────────────────────────┐    │
                                    │  │              Storage (S3)                │    │
                                    │  │  templates │ snapshots │ builds │ logs  │    │
                                    │  └─────────────────────────────────────────┘    │
                                    │                                                  │
                                    │  ┌─────────┐  ┌─────────┐  ┌─────────────┐      │
                                    │  │   ECR   │  │ Secrets │  │ CloudWatch  │      │
                                    │  │         │  │ Manager │  │ (Logs/Alarms│      │
                                    │  └─────────┘  └─────────┘  └─────────────┘      │
                                    └─────────────────────────────────────────────────┘
```

## What's Included

### Core Infrastructure (`main.tf`)
- **VPC & Networking**: VPC with public/private subnets, NAT Gateway, security groups
- **Load Balancing**: Application Load Balancer with HTTPS, HTTP→HTTPS redirect
- **Auto Scaling**: Separate ASGs for control plane and worker nodes
- **Container Registry**: ECR repositories for E2B images
- **Storage**: S3 buckets for templates, snapshots, builds, and logs
- **DNS**: Route 53 or Cloudflare integration

### Data Layer (`data.tf`)
- **RDS PostgreSQL**: Managed PostgreSQL database with encryption, backups
- **ElastiCache Redis**: Managed Redis cluster with TLS, auth token
- Security groups and subnet groups

### Secrets Management (`secrets.tf`)
- **AWS Secrets Manager**: All sensitive configuration stored securely
- Auto-generated passwords and tokens
- IAM policies for secret access

### Observability (`observability.tf`)
- **CloudWatch Log Groups**: For all E2B services
- **CloudWatch Alarms**: 5xx errors, latency, unhealthy hosts, RDS/Redis metrics
- **CloudWatch Dashboard**: Overview of system health
- **ALB Access Logs**: S3-based logging

### Auto Scaling (`autoscaling.tf`)
- **Step Scaling**: CPU-based scale up/down policies
- **Target Tracking**: Alternative CPU and request count based scaling
- **Scheduled Scaling**: Optional time-based scaling for predictable workloads

### User Data Bootstrap Scripts (`scripts/`)
- **Control Plane**: Installs Nomad server, Consul server, Docker, CloudWatch agent
- **Workers**: Installs Firecracker, kernel tuning, Nomad client

### Nomad Jobs (`nomad/`)
- AWS-adapted versions of all E2B Nomad jobs
- API, Orchestrator, Client Proxy, Template Manager, Redis

## Prerequisites

1. **AWS Account** with appropriate permissions
2. **AWS CLI** configured with credentials
3. **Terraform** >= 1.5.0
4. **Domain name** with DNS management access
5. **ACM Certificate** for your domain (in the same region as ALB)

## Quick Start

### 1. Create Remote State Infrastructure

```bash
cd iac/provider-aws/state

terraform init
terraform apply \
  -var="aws_region=us-east-1" \
  -var="state_bucket_name=e2b-terraform-state-YOURNAME" \
  -var="lock_table_name=e2b-terraform-locks" \
  -var='tags={"project":"e2b","env":"prod"}'
```

### 2. Configure Environment

```bash
cd iac/provider-aws

# Copy the template
cp .env.aws.template .env.aws.prod

# Edit the configuration
# Fill in: AWS_REGION, DOMAIN_NAME, ACM_CERTIFICATE_ARN, AMI IDs, etc.
```

### 3. Initialize Terraform

```bash
terraform init \
  -backend-config="bucket=e2b-terraform-state-YOURNAME" \
  -backend-config="key=terraform/e2b/state" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=e2b-terraform-locks"
```

### 4. Deploy Infrastructure

```bash
# Plan the deployment
terraform plan \
  -var="aws_region=us-east-1" \
  -var="environment=prod" \
  -var="domain_name=e2b.example.com" \
  -var="acm_certificate_arn=arn:aws:acm:us-east-1:123456789:certificate/..." \
  -var="control_plane_ami_id=ami-xxxxxxxx" \
  -var="worker_ami_id=ami-yyyyyyyy" \
  -var="availability_zones=[\"us-east-1a\",\"us-east-1b\"]" \
  -var="public_subnet_cidrs=[\"10.10.0.0/24\",\"10.10.1.0/24\"]" \
  -var="private_subnet_cidrs=[\"10.10.10.0/24\",\"10.10.11.0/24\"]"

# Apply
terraform apply
```

### 5. Build and Push Images

```bash
# From project root
# Configure ECR authentication
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin ACCOUNT.dkr.ecr.us-east-1.amazonaws.com

# Build and push (update Makefile with ECR URLs first)
make build-and-upload
```

### 6. Deploy Nomad Jobs

```bash
cd iac/provider-aws/nomad

terraform init
terraform apply \
  -var="aws_region=us-east-1" \
  -var="environment=prod" \
  -var="api_docker_image=ACCOUNT.dkr.ecr.us-east-1.amazonaws.com/e2b:api-latest" \
  -var="template_bucket_name=e2b-prod-templates" \
  -var="build_bucket_name=e2b-prod-builds"
```

## Configuration Reference

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `aws_region` | AWS region | `us-east-1` |
| `domain_name` | Your domain | `e2b.example.com` |
| `acm_certificate_arn` | ACM cert ARN | `arn:aws:acm:...` |
| `control_plane_ami_id` | Ubuntu AMI for control plane | `ami-0c7217cdde317cfec` |
| `worker_ami_id` | Ubuntu AMI for workers (Nitro) | `ami-0c7217cdde317cfec` |
| `availability_zones` | AZs for subnets | `["us-east-1a","us-east-1b"]` |
| `public_subnet_cidrs` | Public subnet CIDRs | `["10.10.0.0/24","10.10.1.0/24"]` |
| `private_subnet_cidrs` | Private subnet CIDRs | `["10.10.10.0/24","10.10.11.0/24"]` |

### Optional Variables

See `variables.tf`, `data-variables.tf`, `secrets-variables.tf`, `observability-variables.tf`, and `autoscaling-variables.tf` for complete variable documentation.

## File Structure

```
iac/provider-aws/
├── main.tf                    # Core infrastructure (VPC, ALB, ASG, S3, ECR)
├── variables.tf               # Core variables
├── versions.tf                # Provider versions
├── providers.tf               # AWS + Cloudflare providers
├── data.tf                    # RDS PostgreSQL, ElastiCache Redis
├── data-variables.tf          # Data layer variables
├── secrets.tf                 # AWS Secrets Manager
├── secrets-variables.tf       # Secrets variables
├── observability.tf           # CloudWatch logs, alarms, dashboard
├── observability-variables.tf # Observability variables
├── autoscaling.tf             # Auto scaling policies
├── autoscaling-variables.tf   # Auto scaling variables
├── userdata.tf                # User data template generation
├── .env.aws.template          # Environment configuration template
├── README.md                  # This file
├── state/                     # Remote state bootstrap
│   ├── main.tf
│   └── variables.tf
├── scripts/                   # EC2 bootstrap scripts
│   ├── control-plane-userdata.sh
│   └── worker-userdata.sh
└── nomad/                     # Nomad job definitions
    ├── main.tf                # Job deployment
    ├── variables.tf           # Job variables
    └── jobs/
        ├── api.hcl
        ├── orchestrator.hcl
        ├── edge.hcl
        ├── template-manager.hcl
        └── redis.hcl
```

## Cost Estimates

Approximate monthly costs (us-east-1, on-demand pricing):

| Scenario | Configuration | Est. Monthly Cost |
|----------|--------------|-------------------|
| **Dev** | 1 CP + 1 Worker (c6a.large), small RDS/Redis | ~$180-220 |
| **Staging** | 2 CP + 2 Workers, medium RDS/Redis | ~$400-500 |
| **Production** | 3 CP + 5 Workers, multi-AZ RDS/Redis | ~$1,000-1,500 |

*Excludes data transfer, heavy S3 usage, and CloudWatch ingestion. Use AWS Pricing Calculator for accurate estimates.*

## Outputs

After `terraform apply`, you'll receive:

| Output | Description |
|--------|-------------|
| `vpc_id` | VPC ID |
| `alb_dns_name` | ALB DNS name for CNAME/alias |
| `ecr_repositories` | ECR repository URLs |
| `artifact_buckets` | S3 bucket names |
| `rds_endpoint` | PostgreSQL endpoint |
| `redis_endpoint` | Redis endpoint |
| `secrets_arns` | Secrets Manager ARNs |
| `dashboard_url` | CloudWatch dashboard URL |

## Troubleshooting

### Nomad/Consul not clustering
- Check EC2 instance tags for `consul-cluster` and `nomad-server`
- Verify security groups allow internal VPC traffic
- Check CloudWatch logs at `/e2b/{env}/nomad` and `/e2b/{env}/consul`

### Firecracker VMs not starting
- Verify worker AMI is Nitro-capable
- Check `kvm` and `nbd` kernel modules are loaded
- Review orchestrator logs in CloudWatch

### ALB health checks failing
- Verify control plane instances are healthy in ASG
- Check API service is running on correct port
- Review ALB access logs in S3

## Security Considerations

1. **Network Isolation**: All data layer resources in private subnets
2. **Encryption**: RDS and Redis encrypted at rest and in transit
3. **IMDSv2**: Enforced on all EC2 instances
4. **Secrets Manager**: All credentials stored securely
5. **Security Groups**: Least-privilege ingress rules
6. **IAM Roles**: Scoped permissions for each instance type

## Upgrading

1. Update Terraform variables as needed
2. Run `terraform plan` to review changes
3. Apply with `terraform apply`
4. For Nomad job updates, use the `nomad/` module

## External Dependencies

The following must be provisioned separately:

- **ClickHouse**: Analytics database (can use ClickHouse Cloud or self-hosted)
- **Supabase**: Authentication (configure JWT secrets)
- **PostHog**: Analytics (optional)
- **LaunchDarkly**: Feature flags (optional)

## Support

For issues with this AWS deployment:
1. Check CloudWatch logs for error messages
2. Verify all required secrets are populated
3. Review [GitHub Issues](https://github.com/anthropics/claude-code/issues)
