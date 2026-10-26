# Upstream port workflow

Maintain this fork as reviewable SIG capability groups on top of
[`e2b-dev/runtime`](https://github.com/e2b-dev/runtime). Upstream owns the
general E2B runtime. SIG owns its account-bound deployment, governed template
selection, worker integration, cost evidence, and release authority. A clean
upstream checkout is a substrate, not a SIG-ready runtime.

## Current base

At the time this document was written:

```text
upstream: https://github.com/e2b-dev/runtime.git
base:    f551e44addb4acce285aa290fba39c260dd522fe (2026-10-26 snapshot)
fork:    9c4a3e499e92c9d722de07e831535f7ea9b3063d (2026-10-26 snapshot)
```

Always fetch first and record the exact base commit in the port PR.  Do not
assume that a branch name is immutable.

```bash
git fetch --prune upstream
git worktree add -b port/upstream-YYYYMMDD .worktrees/upstream-YYYYMMDD upstream/main
git rev-parse HEAD              # record this as UPSTREAM_BASE
```

## Building the SIG stack

Derive the stack from the SIG fork instead of copying a full tree.  The
`--cherry-pick` filter omits changes that upstream has since adopted, which is
important because an upstream commit may have a different SHA in the SIG
fork.

```bash
SIG_SOURCE=origin/main
git log --cherry-pick --right-only --no-merges --format='%H %s' \
  upstream/main..."$SIG_SOURCE"
```

Review that list into ordered capability groups. It is a candidate inventory,
not an automatic cherry-pick list: the fork contains several dependent series
and changes that current upstream may have superseded by behavior rather than
patch identity. Apply one reviewed group at a time and keep its commits separate.

```bash
git cherry-pick <topic-commit-1> <topic-commit-2>
git diff --check
```

The useful groups are:

1. **Upstream runtime fixes** — only changes that are absent from the current
   upstream base and pass the affected Go/package tests.
2. **SIG runtime adapters** — account selection, CQ placement, wake/build
   lifecycle, and environment-specific configuration.  These must not alter
   upstream service semantics silently.
3. **IaC and deployment authority** — Terraform, launch templates, Nomad
   topology, and account-bound guards.  Keep these in their own commits so a
   runtime port can be reviewed without applying infrastructure.
4. **Local proof and release wiring** — local attestation, receipt publication,
   and promotion tooling.  These are SIG-owned and must remain independent of
   GitHub Actions.

## Cross-repository contracts to preserve

| Boundary | Required behavior before a SIG port can replace the fork | Evidence to collect |
| --- | --- | --- |
| E2B API and SIG edge/worker | Keep the API origin distinct from the sandbox forwarding domain. Team API keys authenticate API requests; per-sandbox envd traffic follows the configured wildcard domain. The SIG edge and sandbox worker must receive the same domain as E2B's `DOMAIN_NAME`. | Terraform task definitions, edge execute test, SDK port-forward test, live DNS/route probe. |
| SIG template catalog and E2B template manager | Resolve only a verified stable immutable self-hosted template release; do not let a caller select a raw template or inject environment variables around the governed profile. Preserve build status, terminal outcome, and actionable COPY/placement errors. | Catalog and template-manager tests, exact release ID/build ID, live template-build canary. |
| E2B allocation and SIG execution session | Treat `allocationIdentity` as optional server-owned context. Missing or historical context stays unavailable; no caller metadata or local receipt can invent an allocation or a SIG session mapping. | API response tests, create/resume/fork/connect tests, consumer mapping check. |
| E2B lifecycle and SIG spend | Admission, wake, creation, refresh, pause/close, and spend settlement must retain their idempotency and failure compensation. Device/network observations are evidence with stated gaps, not provider-billable usage. | Zero-to-one wake and redelivery smoke, bounded metering tests, ledger reconciliation. |
| E2B host topology and SIG release | Nomad/Consul identity, DNS owner, build-node capacity, restore memory, and worker NBD/Firecracker prerequisites must match the deployed topology. The SIG sandbox worker image is built and published by a separate CQ-scoped source/proof-bound route; an E2B port cannot imply its release. | Terraform plan, bootstrap/health probes, restore/build canary, separate sandbox image digest and SIG deployment receipts. |
| Database and account authority | Migration history must be reconciled against the actual restored schema and ledger; an operator-supplied baseline alone cannot assert migrations applied. Cost tags and cloud account identity follow one reviewed taxonomy. | Restored DB inventory and replay test, account-scoped Terraform plan, tag readback. |

The current fork's network measurement and protected-custody work is default-off
and retains explicit completeness gaps. Port it as a separate capability group;
do not promote a local sample or signed source proof into a billing or live
retention claim.

## Conflict policy

When a cherry-pick conflicts, first classify the file:

- **Upstream-owned runtime file:** port the SIG behavior to the current
  upstream API and preserve upstream's surrounding implementation.  Never
  resolve by blindly choosing `ours` or `theirs`.
- **SIG-owned IaC/proof file:** keep the SIG change and re-run its focused
  contract tests against the new base.
- **Generated/vendor file:** regenerate from the current upstream source or
  drop the patch; do not hand-merge generated output.

If a patch is already present upstream, skip it and record the upstream commit
or current behavior that supersedes it in the port PR. A clean cherry-pick is
not proof of semantic compatibility; the affected package tests and SIG local
proof remain required. Preserve source and deployment authority boundaries:
fork `main`, upstream `main`, the SIG sandbox worker's hot/dev branch, and SIG
`dev` are distinct heads with distinct proof and rollout gates.

## Acceptance checklist

Before publishing a port:

```bash
git diff --check
(cd packages/shared && GOWORK=off go test ./pkg/sandbox-catalog/...)
(cd packages/api && GOWORK=off go test ./internal/template-manager/... ./internal/orchestrator/placement/...)
git status --short --branch
```

Then run the relevant SIG and sandbox local proof lanes against their own exact
source heads and verify the cross-repository contract above. The port receipt
must name the upstream base and SIG patch commits. A successful source test
does not establish deployed routing, a live template build, an ECS worker
rollout, or human acceptance; retain those as separate gates.

This workflow makes upstream refreshes compositional: a new upstream base
changes the substrate, while the SIG delta remains an explicit, reviewable
series that can be dropped, reordered by dependency, or re-ported by intent.
