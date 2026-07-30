#!/usr/bin/env bash

set -euo pipefail

provider_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "${fixture_dir}"' EXIT

mkdir -p "${fixture_dir}/bin"

cat >"${fixture_dir}/bin/aws" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *"sts get-caller-identity"* ]]; then
  printf '%s\n' '014155356804'
  exit 0
fi
printf 'unexpected fake aws invocation: %s\n' "$*" >&2
exit 1
EOF
chmod +x "${fixture_dir}/bin/aws"

common_args=(
  --no-print-directory
  -C "${provider_dir}"
  ENV=dev
  SIG_AWS_ACCOUNT_TARGET=cq
  AWS_PROFILE=test-cq
  AWS_ACCOUNT_ID=014155356804
  AWS_REGION=us-east-1
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate
  TF=terraform
)

export PATH="${fixture_dir}/bin:${PATH}"

fail() {
  printf 'deployment authority test failed: %s\n' "$*" >&2
  exit 1
}

if make "${common_args[@]}" aws-account-guard TF_VAR_FILE=/tmp/unreviewed.tfvars >"${fixture_dir}/var-file.log" 2>&1; then
  fail 'caller-controlled TF_VAR_FILE was accepted for CQ'
fi
grep -q 'CQ does not accept TF_VAR_FILE overrides' "${fixture_dir}/var-file.log" ||
  fail 'TF_VAR_FILE rejection was not explicit'

if make "${common_args[@]}" aws-account-guard TF_PLAN_FILE=.tfplan.dev.sig >"${fixture_dir}/plan-file.log" 2>&1; then
  fail 'caller-controlled TF_PLAN_FILE was accepted'
fi
grep -q 'TF_PLAN_FILE is derived from account authority' "${fixture_dir}/plan-file.log" ||
  fail 'TF_PLAN_FILE rejection was not explicit'

dry_run_plan="$(make "${common_args[@]}" -n plan PREFIX=unreviewed-)"
[[ "${dry_run_plan}" == *"-var-file=./dev.cq.tfvars"* ]] ||
  fail 'CQ did not select the canonical dev.cq.tfvars'
[[ "${dry_run_plan}" != *"TF_VAR_prefix=unreviewed-"* ]] ||
  fail 'environment-derived TF_VAR_prefix overrode reviewed CQ tfvars'

dry_run_apply="$(make "${common_args[@]}" -n apply)"
[[ "${dry_run_apply}" == *".tfplan.dev.cq"* ]] ||
  fail 'apply did not use the CQ-bound plan path'

if make "${common_args[@]}" aws-account-guard TF_VAR_prefix=unreviewed- >"${fixture_dir}/override.log" 2>&1; then
  fail 'ambient TF_VAR_prefix was accepted for CQ'
fi
grep -q 'CQ rejects ambient Terraform variable overrides: TF_VAR_prefix' "${fixture_dir}/override.log" ||
  fail 'ambient TF_VAR rejection was not explicit'

make "${common_args[@]}" aws-account-guard

if grep -Fq '$(file <' "${provider_dir}/Makefile"; then
  fail 'Makefile still requires GNU Make 4 file-read syntax'
fi

printf 'AWS deployment authority checks passed.\n'
