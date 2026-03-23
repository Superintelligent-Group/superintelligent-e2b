# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

E2B Infrastructure is the backend infrastructure powering E2B (e2b.dev), an open-source cloud platform for AI code interpreting. It provides sandboxed execution environments using Firecracker microVMs, deployed on GCP using Terraform and Nomad.

## Common Development Commands

### Setup & Environment
```bash
# Switch between environments (prod, staging, dev)
make switch-env ENV=staging

# Setup GCP authentication
make login-gcloud

# Initialize Terraform
make init

# Setup local development stack (PostgreSQL, Redis, ClickHouse, monitoring)
make local-infra
```

### Building & Testing
```bash
# Run all unit tests across packages
make test

# Run integration tests
make test-integration

# Build specific package
make build/api
make build/orchestrator

# Generate code (proto, SQL, OpenAPI)
make generate

# Format and lint code
make fmt
make lint

# Regenerate mocks
make generate-mocks

# Tidy go dependencies
make tidy
```

### Running Services Locally
```bash
# From packages/api/
make run-local          # Run API server on :3000
make dev                # Run with air (hot reload)

# From packages/orchestrator/
make run-local          # Run orchestrator
make run-debug          # Run with race detector
```

### Package-Specific Commands
```bash
# API: Generate OpenAPI code
cd packages/api && make generate

# Orchestrator: Generate proto + OpenAPI
cd packages/orchestrator && make generate

# DB: Run migrations
make migrate

# Run single test
cd packages/<package> && go test -v -run TestName ./path/to/package
```

### Deployment
```bash
# Build and upload all services to GCP
make build-and-upload

# Build specific service
make build-and-upload/api
make build-and-upload/orchestrator

# Plan Terraform changes
make plan                    # All changes
make plan-without-jobs       # Without Nomad jobs
make plan-only-jobs          # Only Nomad jobs

# Apply changes
make apply
```

## Architecture Overview

### Service Communication Flow
```
Client → Client-Proxy → API (REST) ⟷ PostgreSQL
                      ↓              ⟷ Redis
                   Orchestrator     ⟷ ClickHouse
                      ↓ (gRPC)
                   Firecracker VMs
                      ↓
                   Envd (in-VM daemon)
```

### Core Services

**API (`packages/api/`)** - REST API using Gin framework
- Entry point: `main.go`
- Core logic: `internal/handlers/store.go` (APIStore)
- Authentication: JWT via Supabase in `internal/auth/`
- OpenAPI code generation: `internal/api/*.gen.go`
- Port: 80

**Orchestrator (`packages/orchestrator/`)** - Firecracker microVM orchestration
- Entry point: `main.go`
- VM management: `internal/sandbox/`
- Firecracker integration: `internal/sandbox/fc/`
- Networking: `internal/sandbox/network/`
- Storage: `internal/sandbox/nbd/` (Network Block Device)
- Template caching: `internal/sandbox/template/`
- gRPC server: `internal/server/`
- Utilities: `cmd/clean-nfs-cache/`, `cmd/build-template/`

**Envd (`packages/envd/`)** - In-VM daemon using Connect RPC
- Runs inside each Firecracker VM
- Process management API: `/spec/process/process.proto`
- Filesystem API: `/spec/filesystem/filesystem.proto`
- Port: 49983

**Client Proxy (`packages/client-proxy/`)** - Edge routing layer
- Service discovery via Consul
- Request routing to orchestrators
- Redis-backed state management

**Shared (`packages/shared/`)** - Common utilities
- Proto definitions: `pkg/grpc/orchestrator/`, `pkg/grpc/envd/`
- Telemetry: `pkg/telemetry/` (OpenTelemetry)
- Logging: `pkg/logger/` (Zap + OTEL)
- Database: `pkg/db/` (ent ORM)
- Models: `pkg/models/`
- Storage: `pkg/storage/` (GCS/S3 clients)
- Feature flags: `pkg/featureflags/` (LaunchDarkly)

**Database (`packages/db/`)** - PostgreSQL layer
- Migrations: `migrations/*.sql` (goose)
- Queries: `queries/*.sql` (sqlc)
- Generated code: `internal/db/`

### Key Technologies

- **Go 1.25.4** with workspaces (`go.work`)
- **Firecracker** for microVM virtualization
- **PostgreSQL** for primary data (sqlc for queries)
- **ClickHouse** for analytics
- **Redis** for caching and state
- **Terraform + Nomad** for IaC and orchestration
- **OpenTelemetry** for observability (Grafana stack: Loki, Tempo, Mimir)
- **gRPC/Connect RPC** for service communication
- **Gin** (API), **chi** (Envd) for HTTP

### Code Generation

The codebase uses several code generators:

1. **Protocol Buffers** (`packages/orchestrator/generate.Dockerfile`)
   - Generates: `packages/shared/pkg/grpc/*/`
   - Run: `make generate/orchestrator`

2. **OpenAPI** (`oapi-codegen`)
   - Spec: `spec/openapi.yml`
   - Generates: API handlers, types, specs
   - Run: `make generate/api`

3. **SQL** (`sqlc`)
   - Queries: `packages/db/queries/*.sql`
   - Generates: Type-safe DB code
   - Run: `make generate/db`

4. **Mocks** (`mockery`)
   - Config: `.mockery.yaml`
   - Run: `make generate-mocks`

### Testing Patterns

- **Unit tests**: Use `testify/assert` and `testify/require`
- **Database tests**: Use `testcontainers-go` for real PostgreSQL
- **Integration tests**: `tests/integration/` with shared test utilities
- **Mocking**: Generated mocks in `mocks/` directories
- **Race detection**: Tests run with `-race` flag

Example test invocation:
```bash
# Single package
go test -race -v ./internal/handlers

# Specific test
go test -race -v -run TestCreateSandbox ./internal/handlers
```

## Important Development Notes

### Working with Proto/gRPC
- Proto files: `spec/process/`, `spec/filesystem/`, internal proto in orchestrator
- Shared protos: `packages/shared/pkg/grpc/`
- After editing proto files, run `make generate/orchestrator` and `make generate/shared`

### Database Migrations
- Migrations: `packages/db/migrations/`
- Create: Add new `XXXXXX_name.sql` file
- Apply: `make migrate` (requires POSTGRES_CONNECTION_STRING)
- Code generation: `make generate/db` (regenerates sqlc code)

### Environment Variables
- Environment configs: `.env.{prod,staging,dev}`
- Template: `.env.template`
- Switch: `make switch-env ENV=staging`
- Secrets stored in GCP Secrets Manager (production)

### Infrastructure as Code
- Location: `iac/provider-gcp/`
- Nomad jobs: `iac/provider-gcp/nomad/jobs/`
- Network config: `iac/provider-gcp/network/`
- Deploy jobs only: `make plan-only-jobs` + `make apply`
- Deploy specific job: `make plan-only-jobs/orchestrator`

### Firecracker & VM Management
- Orchestrator requires **sudo** to run (Firecracker needs root)
- VM networking uses `iptables` and Linux `netlink`
- Storage uses NBD (Network Block Device)
- Templates cached in GCS bucket (configurable via TEMPLATE_BUCKET_NAME)
- Kernel/Firecracker versions: `packages/fc-versions/`

### Observability
- All services export OpenTelemetry traces/metrics/logs
- Local stack includes Grafana + Loki + Tempo + Mimir
- Telemetry setup: `packages/shared/pkg/telemetry/`
- Logger: `packages/shared/pkg/logger/` (Zap with OTEL)
- Profiling: API exposes pprof on `/debug/pprof/` (see `packages/api/Makefile` profiler target)

### CI/CD Workflows
- `.github/workflows/pr-tests.yml` - Run on PRs
- `.github/workflows/deploy-infra.yml` - Deploy infrastructure
- `.github/workflows/build-and-upload-job.yml` - Build containers
- `.github/workflows/integration_tests.yml` - Integration test suite

## Architecture Patterns

1. **Service Isolation**: Each service runs in containers with defined gRPC/HTTP interfaces
2. **Shared Libraries**: Cross-cutting concerns (logging, telemetry, DB) in `packages/shared`
3. **Event-Driven**: ClickHouse + Redis pub/sub for async operations
4. **Caching Strategy**: Redis for templates, auth tokens, performance optimization
5. **Feature Flags**: LaunchDarkly for gradual rollouts
6. **Graceful Shutdown**: Services handle SIGTERM with context cancellation
7. **Health Checks**: gRPC health protocol + HTTP health endpoints

## Self-Hosting

Self-hosting is fully supported on GCP (AWS in progress). See `self-host.md` for complete setup guide.

Key steps:
1. Create GCP project and configure quotas
2. Create `.env.{prod,staging,dev}` from `.env.template`
3. Run `make switch-env ENV=<env>`
4. Run `make login-gcloud && make init`
5. Run `make build-and-upload && make copy-public-builds`
6. Configure secrets in GCP Secrets Manager
7. Run `make plan && make apply`

## Debugging

### Remote Development (VSCode)
- See `DEV.md` for remote SSH setup via GCP
- Supports Go debugger attachment to remote instances

### SSH to Orchestrator
```bash
make setup-ssh
make connect-orchestrator
```

### Nomad UI
- Access: `https://nomad.<your-domain>`
- Token: GCP Secrets Manager

### Logs
- Local: Docker logs in `make local-infra`
- Production: Grafana Loki or Nomad UI

---

## 🚧 AWS Deployment - In Progress

### Current Status
We are setting up E2B infrastructure on AWS. The Terraform code is complete but AWS account setup is in progress.

### What Has Been Built (Terraform Code Ready)
All AWS infrastructure Terraform is in `iac/provider-aws/`:

| File | Purpose |
|------|---------|
| `main.tf` | Core infrastructure (VPC, ALB, ASG, S3, ECR, IAM) |
| `data.tf` | RDS PostgreSQL + ElastiCache Redis |
| `secrets.tf` | AWS Secrets Manager configuration |
| `observability.tf` | CloudWatch logs, alarms, dashboard |
| `autoscaling.tf` | Auto scaling policies |
| `userdata.tf` | EC2 bootstrap script generation |
| `scripts/control-plane-userdata.sh` | Control plane bootstrap |
| `scripts/worker-userdata.sh` | Worker node bootstrap (Firecracker setup) |
| `nomad/` | Nomad job definitions adapted for AWS |

### User's AWS Setup Progress
**Current Step: Step 1 - Create IAM Admin User**

- [ ] **Step 1**: Create IAM admin user (DO NOT use root account for deployment)
- [ ] **Step 2**: Install AWS CLI
- [ ] **Step 3**: Configure AWS CLI with access keys
- [ ] **Step 4**: Choose region and domain
- [ ] **Step 5**: Create ACM certificate for SSL
- [ ] **Step 6**: Find Ubuntu AMI ID for the region
- [ ] **Step 7**: Create SSH key pair (optional)
- [ ] **Step 8**: Create Terraform state backend (S3 + DynamoDB)
- [ ] **Step 9**: Deploy infrastructure with Terraform
- [ ] **Step 10**: Build and push Docker images to ECR
- [ ] **Step 11**: Deploy Nomad jobs

### Quick Resume Guide

When resuming, check these items:

1. **If AWS CLI not configured yet:**
   ```bash
   aws sts get-caller-identity
   ```
   If this fails, need to complete Steps 1-3.

2. **If CLI works but no infrastructure:**
   ```bash
   cd iac/provider-aws
   terraform init  # Will fail if state backend not created
   ```
   If fails, need Step 8 first.

3. **If state backend exists:**
   ```bash
   cd iac/provider-aws
   terraform plan
   ```

### AWS Setup - Detailed Steps

#### Step 1: Create IAM Admin User
1. Go to: https://console.aws.amazon.com/iam/
2. Click **Users** → **Create user**
3. Name: `e2b-admin`, check "Provide console access"
4. Attach **AdministratorAccess** policy
5. Create **Access Keys** for CLI (Security credentials tab)
6. **SAVE THE ACCESS KEY AND SECRET!** (won't be shown again)

#### Step 2-3: Install & Configure AWS CLI
```powershell
# Windows
winget install Amazon.AWSCLI

# Configure
aws configure
# Enter: Access Key, Secret Key, Region (us-east-1), Output (json)

# Verify
aws sts get-caller-identity
```

#### Step 4: Domain Decision
Options:
- Buy domain in Route 53
- Use existing domain with Route 53 hosted zone
- Use Cloudflare (set `cloudflare_api_token` and `cloudflare_zone_id`)

#### Step 5: Create ACM Certificate
1. Go to: https://console.aws.amazon.com/acm/
2. **Make sure you're in your target region!**
3. Request public certificate for your domain
4. Use DNS validation
5. Save the Certificate ARN

#### Step 6: Get Ubuntu AMI ID
```bash
aws ec2 describe-images \
  --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
  --query 'sort_by(Images, &CreationDate)[-1].ImageId' \
  --output text \
  --region us-east-1
```

#### Step 7: Create SSH Key (Optional)
```bash
aws ec2 create-key-pair --key-name e2b-key --query 'KeyMaterial' --output text --region us-east-1 > e2b-key.pem
chmod 400 e2b-key.pem
```

#### Step 8: Create Terraform State Backend
```bash
cd iac/provider-aws/state
terraform init
terraform apply \
  -var="aws_region=us-east-1" \
  -var="state_bucket_name=e2b-terraform-state-UNIQUE-NAME" \
  -var="lock_table_name=e2b-terraform-locks"
```

#### Step 9: Deploy Infrastructure
```bash
cd iac/provider-aws

# Initialize with backend
terraform init \
  -backend-config="bucket=e2b-terraform-state-UNIQUE-NAME" \
  -backend-config="key=terraform/e2b/state" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=e2b-terraform-locks"

# Create terraform.tfvars with your config (see .env.aws.template)

# Deploy
terraform plan
terraform apply
```

### Key Configuration Values to Collect
Before deploying, gather these:

| Item | Value | Status |
|------|-------|--------|
| AWS Account ID | (12-digit number) | ❌ Not collected |
| AWS Region | `us-east-1` (recommended) | ❌ Not confirmed |
| Domain name | e.g., `e2b.example.com` | ❌ Not decided |
| ACM Certificate ARN | `arn:aws:acm:...` | ❌ Not created |
| Ubuntu AMI ID | `ami-xxxxxxxx` | ❌ Not looked up |
| Route 53 Zone ID | `Z...` (if using Route 53) | ❌ Not created |

### AWS vs GCP Mapping
| GCP | AWS | Status |
|-----|-----|--------|
| Cloud Storage | S3 | ✅ Terraform ready |
| Artifact Registry | ECR | ✅ Terraform ready |
| Cloud SQL | RDS PostgreSQL | ✅ Terraform ready |
| Memorystore | ElastiCache Redis | ✅ Terraform ready |
| Secret Manager | Secrets Manager | ✅ Terraform ready |
| GKE + Nomad | EC2 + Nomad | ✅ Terraform ready |
| Cloud Logging | CloudWatch | ✅ Terraform ready |

### Files Created for AWS
```
iac/provider-aws/
├── main.tf                    # Core infra (existing, enhanced)
├── variables.tf               # Core variables (existing)
├── data.tf                    # NEW: RDS + ElastiCache
├── data-variables.tf          # NEW: Data layer variables
├── secrets.tf                 # NEW: Secrets Manager
├── secrets-variables.tf       # NEW: Secrets variables
├── observability.tf           # NEW: CloudWatch
├── observability-variables.tf # NEW: Observability variables
├── autoscaling.tf             # NEW: Auto scaling policies
├── autoscaling-variables.tf   # NEW: Scaling variables
├── userdata.tf                # NEW: User data generation
├── .env.aws.template          # NEW: Environment template
├── SETUP-GUIDE.md             # NEW: Step-by-step setup
├── README.md                  # UPDATED: Comprehensive docs
├── scripts/
│   ├── control-plane-userdata.sh  # NEW: CP bootstrap
│   └── worker-userdata.sh         # NEW: Worker bootstrap
└── nomad/
    ├── main.tf                # NEW: Nomad job deployment
    ├── variables.tf           # NEW: Nomad variables
    └── jobs/
        ├── api.hcl            # NEW: API job
        ├── orchestrator.hcl   # NEW: Orchestrator job
        ├── edge.hcl           # NEW: Client proxy job
        ├── template-manager.hcl # NEW: Template manager
        └── redis.hcl          # NEW: Redis job
```

### Estimated AWS Costs (Monthly)
| Scenario | Config | Cost |
|----------|--------|------|
| Dev | 1 CP + 1 Worker, small RDS/Redis | ~$180-220 |
| Staging | 2 CP + 2 Workers, medium | ~$400-500 |
| Production | 3 CP + 5 Workers, multi-AZ | ~$1,000-1,500 |
