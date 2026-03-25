# Rebuild E2B Infrastructure From Scratch

Complete playbook for destroying and recreating the entire E2B self-hosted
cluster. Every step is documented — no tribal knowledge required.

## Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| Terraform | 1.5.7 | BSL-free version; `tfenv install 1.5.7` |
| Packer | >= 1.8.4 | AMI builder |
| Docker | Latest | For building orchestrator binary (Linux AMD64) |
| Go | >= 1.21 | For building envd, API, client-proxy |
| Node.js | >= 18 | For building base template |
| pnpm | >= 8 | For SDK dependencies |
| AWS CLI v2 | Latest | With configured profile |
| WSL (Windows only) | Ubuntu 22.04 | For verifying LF line endings |

## Environment Setup

```bash
# 1. Clone and enter repo
git clone <repo-url> && cd superintelligent-e2b

# 2. Create .env.dev from template
cp .env.aws.template .env.dev

# 3. Fill in .env.dev (minimum required):
#    AWS_PROFILE=your-profile
#    AWS_ACCOUNT_ID=319933937176
#    AWS_REGION=us-east-1
#    DOMAIN_NAME=e2b.superintelligent.group
#    PREFIX=e2b-
#    PROVIDER=aws
#    TERRAFORM_ENVIRONMENT=dev

# 4. Set active environment
make set-env ENV=dev
```

## Step-by-Step Rebuild

### Phase 1: AMI (one-time, ~10 min)

Only needed if the AMI doesn't exist or needs updating.

```bash
cd iac/provider-aws/nomad-cluster-disk-image
make init    # packer init
make build   # builds Ubuntu 22.04 + Docker + Nomad 1.6.2 + Consul 1.16.2
# Output: AMI ID like ami-07860f3040f61febf
cd ../../..
```

**What it installs**: Docker, Nomad, Consul, Vault, AWS CLI, ECR helper,
s3fs, Go, nvme-cli, qemu-utils, nfs-common, gruntwork bash-commons.

### Phase 2: Terraform Init (creates S3 state bucket + secrets, ~3 min)

```bash
make init
# This runs: terraform init + terraform apply -target=module.init
# Creates: S3 state bucket, Secrets Manager entries (with placeholders)
```

### Phase 3: Populate Secrets (manual, ~5 min)

These secrets are created by Terraform with placeholder values. You must
populate them before deploying Nomad jobs.

| Secret | How to populate |
|--------|----------------|
| `e2b-postgres-connection-string` | Auto-set by Postgres Nomad job on first boot |
| `e2b-supabase-jwt-secrets` | Your JWT signing secret (if using Supabase auth) |
| `e2b-cloudflare` | `{"TOKEN": "your-cf-token"}` (for DNS management) |
| `e2b-dev/nomad-acl-token` | Generated during Consul/Nomad bootstrap |
| `e2b-dev/consul-acl-token` | Generated during Consul/Nomad bootstrap |

Secrets that are **auto-generated** (no action needed):
- `e2b-api-secret`, `e2b-admin-token`, `e2b-sandbox-access-token-hash-seed`
- `e2b-clickhouse` (username + password)
- `e2b-launch-darkly-api-key` (placeholder OK)
- `e2b-grafana` (placeholder OK unless using Grafana)

### Phase 4: Build and Upload Binaries (~10 min)

```bash
# Build ALL binaries and push to S3/ECR
make build-and-upload

# This builds and uploads:
#   - API server → ECR (docker image)
#   - Client proxy → ECR (docker image)
#   - Orchestrator → S3 fc-env-pipeline/orchestrator
#   - Template manager → S3 fc-env-pipeline/template-manager (same binary)
#   - envd → S3 fc-env-pipeline/envd
#   - clean-nfs-cache → S3 fc-env-pipeline/clean-nfs-cache
#   - Dashboard API, Docker reverse proxy, Clickhouse migrator, Nomad APM
```

**Windows gotcha**: The orchestrator uses `docker build --platform linux/amd64`
(can't cross-compile due to userfaultfd Linux syscall dependency). Docker
Desktop or WSL Docker must be available.

### Phase 5: Copy Public Builds (kernels + Firecracker, ~5 min)

```bash
make copy-public-builds
# Downloads Firecracker binaries and kernels from e2b's public GCS bucket
# and re-uploads to your S3 buckets
```

### Phase 6: Terraform Plan + Apply (infra without Nomad jobs, ~10 min)

```bash
make plan-without-jobs
make apply
# Creates: VPC, subnets, ASGs, ALB, Lambda, S3 buckets, ECR repos, DNS, ACM cert
```

### Phase 7: Terraform Plan + Apply (with Nomad jobs, ~5 min)

```bash
make plan
make apply
# Submits all Nomad jobs: api, redis, ingress, client-proxy, orchestrator,
# template-manager, loki, otel-collector, etc.
```

### Phase 8: Seed Database (~2 min)

After the API node is up and Postgres is running:

```bash
# Option A: Interactive (from a machine that can reach Postgres)
cd packages/db
POSTGRES_CONNECTION_STRING="postgresql://..." make seed-db

# Option B: Via Nomad batch job (if Postgres is only reachable in-cluster)
# See docs/database-seed.md for the Nomad batch job approach
```

This creates: default user, team (`a0000000-0000-0000-0000-000000000001`),
access token, and API key. The API key is stored in `e2b-dev/e2b-api-key`.

### Phase 9: Build Base Template (~5 min)

Requires build nodes running:

```bash
# Scale up build nodes
aws autoscaling update-auto-scaling-group \
  --auto-scaling-group-name e2b-orch-build \
  --desired-capacity 1 --max-size 1

# Wait for template-manager to be running (check Nomad UI)
# Then build the base template
cd packages/shared
E2B_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id e2b-dev/e2b-api-key --query SecretString --output text) \
E2B_DOMAIN=e2b.superintelligent.group \
make build-base-template

# Scale build nodes back to 0
aws autoscaling update-auto-scaling-group \
  --auto-scaling-group-name e2b-orch-build \
  --desired-capacity 0 --min-size 0
```

### Phase 10: Verify (~1 min)

```bash
# Run E2E tests
cd tests
E2B_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id e2b-dev/e2b-api-key --query SecretString --output text) \
node e2b-e2e.mjs
```

## Important Notes

### Windows / CRLF

All `.sh`, `.tpl`, `.conf`, `.go`, `.mod`, `.sum` files MUST have LF line
endings. The `.gitattributes` file enforces this, but if you see `sed: unknown
option to 's'` errors in template builds, check for `\r` characters:

```bash
# In WSL:
strings bin/orchestrator | grep "::sysinit" | od -c | grep '\\r'
```

If found, re-checkout with LF and rebuild:
```bash
git rm --cached -r packages/orchestrator/pkg/template/build/core/rootfs/files/
git checkout -- packages/orchestrator/pkg/template/build/core/rootfs/files/
```

### Nomad Datacenter = Availability Zone

Nodes register with their AZ as datacenter (e.g., `us-east-1c`), NOT the
region. All Nomad jobs use `datacenters = ["us-east-1c"]` — if you deploy
to a different AZ, update the job definitions.

### EBS Volume for Postgres

The API node has a persistent 10GB gp3 EBS volume in `us-east-1c` for
Postgres data. The `start-api.sh` script auto-attaches, formats (first
boot only), and mounts it. If you change AZs, you'll need to create a
new EBS volume in the target AZ.

### Scale-to-Zero

- **Control server**: Always on (Nomad/Consul state)
- **API, Client, Build**: Scale to zero after 30 min idle
- **Wake**: Lambda function URL triggers scale-up
- **EventBridge**: Checks every 5 min for idle shutdown

### S3 Bucket Naming

All buckets follow: `{PREFIX}{AWS_ACCOUNT_ID}-{purpose}`
- `e2b-319933937176-fc-env-pipeline` — orchestrator, envd, template-manager, clean-nfs-cache
- `e2b-319933937176-fc-kernels` — Linux kernels for Firecracker
- `e2b-319933937176-fc-versions` — Firecracker binaries
- `e2b-319933937176-templates` — Built template snapshots
- `e2b-319933937176-templates-build-cache` — Build cache

### Estimated Time

| Phase | Time |
|-------|------|
| AMI build | 10 min (one-time) |
| Terraform init + secrets | 5 min |
| Binary builds | 10 min |
| Copy public builds | 5 min |
| Terraform apply (2 passes) | 15 min |
| DB seed + base template | 10 min |
| **Total** | **~55 min** |

### Estimated Monthly Cost (idle)

| Resource | Cost |
|----------|------|
| Control server (t3.medium on-demand) | ~$30 |
| EBS volume (10GB gp3) | ~$0.80 |
| ALB | ~$16 |
| Route53 hosted zone | ~$0.50 |
| Secrets Manager (22 secrets) | ~$8.80 |
| S3 storage | ~$1 |
| **Total idle** | **~$57/month** |

Spot instances (API, Client, Build) only run when active — see
[instance-pricing.md](./instance-pricing.md) for per-sandbox costs.
