# E2B Instance Type Selection & Pricing Guide

Last updated: 2026-03-24 (us-east-1 spot prices are volatile — re-check before changing)

## Key Constraint: Nested Virtualization

E2B client nodes run **Firecracker microVMs**, which require hardware-assisted nested
virtualization (KVM inside the EC2 hypervisor). On AWS, **only 8th-gen Intel instances
(c8i, m8i, r8i and their -flex variants) support this**. No Graviton, no AMD, no older
Intel generations.

This is verified via the AWS API:
```bash
aws ec2 describe-instance-types \
  --filters "Name=processor-info.supported-features,Values=nested-virtualization" \
  --query 'InstanceTypes[].InstanceType'
```

API and control-server nodes do NOT need nested virt (they run containers, not VMs).

## Pricing Table (us-east-1, Linux, 2026-03-24)

### Client Nodes (Firecracker — MUST support nested virtualization)

| Type | vCPU | RAM | On-Demand $/hr | Spot $/hr | Savings | Spot $/vCPU/hr |
|------|------|-----|----------------|-----------|---------|----------------|
| **c8i.2xlarge** | 8 | 16G | $0.2720 | $0.1125 | 59% | $0.0141 |
| c8i-flex.2xlarge | 8 | 16G | $0.2426 | $0.1208 | 50% | $0.0151 |
| m8i.2xlarge | 8 | 32G | $0.3264 | $0.1276 | 61% | $0.0159 |
| m8i-flex.2xlarge | 8 | 32G | $0.2976 | $0.1269 | 57% | $0.0159 |
| r8i.2xlarge | 8 | 64G | $0.4536 | $0.1920 | 58% | $0.0240 |
| r8i-flex.2xlarge | 8 | 64G | $0.4032 | $0.1585 | 61% | $0.0198 |

#### Smaller Client (dev/testing, fewer concurrent sandboxes)

| Type | vCPU | RAM | On-Demand $/hr | Spot $/hr | Savings | Spot $/vCPU/hr |
|------|------|-----|----------------|-----------|---------|----------------|
| c8i.xlarge | 4 | 8G | $0.1360 | $0.0551 | 59% | $0.0138 |
| **c8i-flex.xlarge** | 4 | 8G | $0.1213 | $0.0393 | 68% | $0.0098 |
| m8i.xlarge | 4 | 16G | $0.1632 | $0.0795 | 51% | $0.0199 |
| m8i-flex.xlarge | 4 | 16G | $0.1488 | $0.0548 | 63% | $0.0137 |

### API Nodes (no nested virt required)

| Type | vCPU | RAM | On-Demand $/hr | Spot $/hr | Savings |
|------|------|-----|----------------|-----------|---------|
| **t3a.large** | 2 | 8G | $0.0752 | $0.0223 | 70% |
| m6i.large | 2 | 8G | $0.0960 | $0.0304 | 68% |
| t3.large | 2 | 8G | $0.0832 | $0.0319 | 62% |

### Control Server (on-demand for reliability, always-on)

| Type | vCPU | RAM | On-Demand $/hr | Monthly |
|------|------|-----|----------------|---------|
| t3.medium | 2 | 4G | $0.0416 | ~$30 |

## Current Configuration

```
Control server: t3.medium on-demand (always-on)     ~$30/month
API node:       t3a.large spot fleet (scale-to-zero) ~$0.02/hr when active
Client node:    c8i.2xlarge spot fleet (scale-to-zero) ~$0.11/hr when active
Build node:     same as client (scale-to-zero)       ~$0.11/hr when active
```

With 30-minute idle timeout and typical dev usage (~8 hrs/day):
- Control server: $30/month (always on)
- API + Client active: ~$0.13/hr x 8 hrs x 22 days = ~$23/month
- **Total dev cost: ~$53/month**

## Spot Fleet Strategy

We use `capacity-optimized` allocation across multiple types for each pool:

- **Client**: `c8i.2xlarge, m8i.2xlarge, c8i-flex.2xlarge, m8i-flex.2xlarge`
  - All support nested virt, 8 vCPU, 16-32GB RAM
  - c8i cheapest on spot, m8i as fallback with more RAM

- **API**: `t3.large, t3a.large, m6i.large`
  - No nested virt needed, burstable OK for API workload
  - t3a cheapest on spot

Fallback: if spot fails, Lambda falls back to on-demand via `_scale_asg()`.

## How to Change Instance Types

1. Edit `iac/provider-aws/auto-scaling.tf` — `client_spot_instance_types` / `api_spot_instance_types`
2. Run `terraform apply` (updates Lambda env vars)
3. Next wake cycle will use new types

For the launch template base type (affects on-demand fallback):
1. Edit `.env.dev` — `CLIENT_MACHINE_TYPE`
2. Run `make plan && make apply`

## Instance Family Reference

| Family | Optimized For | Nested Virt | Notes |
|--------|--------------|-------------|-------|
| c8i | Compute | YES | Best $/vCPU for Firecracker |
| c8i-flex | Compute (flex) | YES | Slightly cheaper OD, variable spot |
| m8i | General | YES | 2x RAM vs c8i, good for memory-heavy sandboxes |
| m8i-flex | General (flex) | YES | Cheaper OD than m8i |
| r8i | Memory | YES | 4x RAM vs c8i, expensive — skip unless needed |
| c7i/c6i | Compute (older) | NO | Cannot run Firecracker |
| m7i/m6i | General (older) | NO | Cannot run Firecracker |
| t3/t3a | Burstable | NO | Fine for API/control, not for sandboxes |
| Graviton (c7g etc) | ARM | NO | E2B doesn't support ARM yet |
