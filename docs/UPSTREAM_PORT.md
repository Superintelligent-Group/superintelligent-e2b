# Upstream port workflow

This fork is maintained as a small, reviewable patch stack on top of
`e2b-dev/infra` rather than as a long-lived merge of the upstream tree.  The
upstream repository owns the E2B runtime; SIG owns the account-bound
deployment, governance, cost, and local-proof seams around it.

## Current base

At the time this document was written:

```text
upstream: https://github.com/e2b-dev/infra.git
base:    upstream/main
```

Always fetch first and record the exact base commit in the port PR.  Do not
assume that a branch name is immutable.

```bash
git fetch --prune upstream
git switch -c port/upstream-YYYYMMDD upstream/main
git rev-parse HEAD              # record this as UPSTREAM_BASE
```

## Building the SIG stack

Derive the stack from the SIG fork instead of copying a full tree.  The
`--cherry-pick` filter omits changes that upstream has since adopted, which is
important because an upstream commit may have a different SHA in the SIG
fork.

```bash
SIG_SOURCE=origin/port/upstream-core-YYYYMMDD
git log --cherry-pick --right-only --no-merges --format='%H %s' \
  upstream/main..."$SIG_SOURCE"
```

Review that list into ordered topic groups, then apply one focused group at a
time.  Keep the commits separate; do not create a single merge commit.

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
that supersedes it in the port PR.  A clean cherry-pick is not proof of
semantic compatibility; the affected package tests and the SIG local proof
remain required.

## Acceptance checklist

Before publishing a port:

```bash
git diff --check
go test ./packages/shared/pkg/sandbox-catalog/...
go test ./packages/api/internal/template-manager/...
go test ./packages/api/internal/orchestrator/placement/...
git status --short --branch
```

Then run the SIG local proof from the SIG repository against the exact landed
commit.  The resulting receipt must name both the upstream base and the SIG
patch commits.  Only after that evidence exists should the stack be promoted
or used for a live template build.

This workflow makes upstream refreshes compositional: a new upstream base
changes the substrate, while the SIG delta remains an explicit, reviewable
series that can be dropped, reordered by dependency, or re-ported by intent.
