# AWS bootstrap for self-hosted E2B

This folder provides an AWS-first Terraform scaffold that mirrors the Google Cloud flow documented in [`self-host.md`](../../self-host.md). It sets up the network, artifact buckets, ECR repositories, an ALB front door, and a worker IAM role so you can drop in the control-plane and worker jobs that the Makefile builds.

## Layout
- `state/` – one-time bootstrap for the Terraform remote state bucket and DynamoDB lock table (runs locally without a backend configured).
- `versions.tf` – pins provider versions and declares the S3 backend (configure with `-backend-config`).
- `providers.tf` – AWS + optional Cloudflare providers.
- `variables.tf` – inputs aligned with `.env.template` (prefix, domain, bucket names, instance CIDRs, etc.).
- `main.tf` – VPC + subnets, NAT, S3 artifact buckets, ECR repos, ALB with HTTPS and HTTP→HTTPS redirect, DNS records (Route 53 or Cloudflare), and a worker IAM role with access to the buckets/ECR/CloudWatch.

## Usage
1) **Create remote state**
```sh
cd iac/provider-aws/state
terraform init
terraform apply -var="aws_region=us-east-1" -var="state_bucket_name=<globally-unique-bucket>" \
  -var="lock_table_name=e2b-terraform-locks" -var='tags={"project":"e2b"}'
```

2) **Configure backend**
Pass the backend config when initializing the main stack:
```sh
cd ..
terraform init \
  -backend-config="bucket=<globally-unique-bucket>" \
  -backend-config="key=terraform/orchestration/state" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=e2b-terraform-locks"
```

3) **Plan/apply**
Provide the CIDRs/availability zones and a certificate ARN from ACM. Bucket names default to `<prefix>-<env>-{templates|snapshots|builds|logs}` if you leave them blank. Supply AMI IDs, instance types, and scaling targets for control-plane and worker Auto Scaling Groups.
```sh
terraform plan \
  -var="aws_region=us-east-1" \
  -var="availability_zones=[\"us-east-1a\",\"us-east-1b\"]" \
  -var="public_subnet_cidrs=[\"10.10.0.0/24\",\"10.10.1.0/24\"]" \
  -var="private_subnet_cidrs=[\"10.10.10.0/24\",\"10.10.11.0/24\"]" \
  -var="control_plane_ami_id=ami-xxxxxxxx" \
  -var="worker_ami_id=ami-yyyyyyyy" \
  -var="domain_name=e2b.example.com" \
  -var="acm_certificate_arn=<arn:aws:acm:...>" \
  -var="create_route53_record=true" \
  -var="route53_zone_id=<ZXXXXXXXXXXX>" \
  -var='tags={"project":"e2b","env":"prod"}'
terraform apply
```

If you use Cloudflare instead of Route 53, set `cloudflare_api_token` + `cloudflare_zone_id` and leave `create_route53_record=false`. Both values are required; otherwise Cloudflare is skipped.

## Next steps
- Push the control-plane and worker images to the emitted ECR URLs (`ecr_repositories` output) using `make build-and-upload` with your AWS registry configured.
- Point Makefile jobs at the emitted S3 buckets (`artifact_buckets` output) for templates, snapshots, builds, and logs.
- Deploy Nomad/Consul jobs into the private subnets using the worker IAM role ARN (`worker_iam_role_arn`) and ALB/control-plane security groups (`api`, `control_plane`, `workers`) for inbound traffic. Use the ASG outputs to scale the control plane (`control_plane_autoscaling_group`) and workers (`worker_autoscaling_group`).
- Pass the ALB DNS name (`alb_dns_name`) or your custom domain into clients via `E2B_DOMAIN`.
- DNS on an existing zone: if your root zone is `superintelligent.group`, add an `A` alias record `e2b.superintelligent.group` in that hosted zone pointing to `alb_dns_name` (evaluate target health on). If using Cloudflare instead, add a `CNAME` named `e2b` to `alb_dns_name` with proxy off unless your certs and setup are Cloudflare-aware.
- ACM: the certificate referenced by `acm_certificate_arn` must cover `e2b.superintelligent.group` and live in the same AWS region as the ALB.
- Costs (rough, us-east-1 on-demand, 24/7): NAT ~$1.08/day (plus $0.045/GB data through NAT), ALB ~$0.73/day baseline (1 LCU; more with traffic), each c6a.large ~$1.63/day, 50 GiB gp3 root volume per node ~$0.13/day, S3 200 GiB Standard ~$0.15/day, ECR 20 GiB ~$0.07/day. Data transfer out, CloudWatch ingest, and DNS queries are extra—use the AWS Pricing Calculator with your exact counts/traffic.
- Cost scenarios (approx, us-east-1, 24/7, on-demand; excludes data transfer out and heavy logs):
  - Dev: 1× control plane + 1× worker (c6a.large), 2×50 GiB gp3, ALB, single NAT → ~$6/day.
  - Base: 2× control plane + 2× workers, 4×50 GiB gp3 → ~$9–10/day.
  - Busy: 3× control plane + 6× workers, 9×50 GiB gp3, ALB ~2 LCUs → ~$19–20/day.
  These assume modest S3/ECR usage (~200 GiB / 20–40 GiB) and low CloudWatch ingest. NAT data processing ($0.045/GB) and internet egress can dominate at scale—model in the AWS Pricing Calculator with expected traffic.

## Still to configure
- **Data stores and queues:** This scaffold does not yet provision Postgres, Redis/ElastiCache, or ClickHouse/analytics. You need to stand those up (or point to existing instances) and feed the connection strings into your control-plane jobs.
- **Secrets:** Secrets Manager/SSM Parameter Store wiring is intentionally absent; create secrets for database credentials, Cloudflare/Posthog/Supabase tokens, and inject them into the control-plane and worker user data or task definitions.
- **User data / service bootstrap:** The EC2 launch templates accept `control_plane_user_data` and `worker_user_data`, but the defaults are empty. Provide cloud-init/systemd scripts that pull artifacts, register with Nomad/Consul, and start the E2B services. User data is base64-encoded automatically.
- **Autoscaling policies:** The Auto Scaling Groups only pin `min`, `desired`, and `max` capacities. Add target-tracking or step policies based on CPU/queue depth to avoid manual resizing.
- **Observability:** CloudWatch log groups/metrics/alarms are not created here. Attach log shipping (e.g., fluent-bit), ALB access log targets, and alarms for high 5xx/latency and node health.
