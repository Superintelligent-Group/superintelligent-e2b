#!/usr/bin/env bash

set -euo pipefail

provider_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${provider_dir}/../.." && pwd)"
fixture_dir="$(mktemp -d)"
implicit_tfvars="${provider_dir}/deployment-authority-test.auto.tfvars"

cleanup() {
  rm -f -- "${implicit_tfvars}"
  rm -rf -- "${fixture_dir}"
}
trap cleanup EXIT

[[ ! -e "${implicit_tfvars}" ]] || {
  printf 'deployment authority test fixture already exists: %s\n' "${implicit_tfvars}" >&2
  exit 1
}

mkdir -p "${fixture_dir}/bin"

cat >"${fixture_dir}/bin/aws" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *"sts get-caller-identity"* ]]; then
  case "${AWS_PROFILE:-}" in
    test-cq)
      printf '%s\n' '014155356804'
      exit 0
      ;;
    test-sig)
      printf '%s\n' '319933937176'
      exit 0
      ;;
  esac
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

if make --no-print-directory -C "${provider_dir}" aws-account-guard \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=foo \
  AWS_PROFILE=attacker \
  AWS_ACCOUNT_ID=000000000000 \
  AWS_ACCOUNT_ID_foo=000000000000 \
  AWS_REGION=us-east-1 \
  AWS_REGION_foo=us-east-1 \
  TERRAFORM_STATE_BUCKET=attacker-state \
  TF_STATE_BUCKET_foo=attacker-state \
  TF_VAR_FILE=./dev.cq.tfvars \
  TF=terraform \
  >"${fixture_dir}/unsupported-target.log" 2>&1; then
  fail 'caller-defined account target namespace was accepted'
fi
grep -q 'Unsupported SIG_AWS_ACCOUNT_TARGET: foo (expected cq or sig)' \
  "${fixture_dir}/unsupported-target.log" ||
  fail 'unsupported account target rejection was not explicit'

if make "${common_args[@]}" aws-account-guard \
  ENV=staging \
  CANONICAL_CQ_ENV=staging \
  CANONICAL_CQ_TF_VAR_FILE=./staging.cq.tfvars \
  TF_VAR_FILE=./staging.cq.tfvars \
  >"${fixture_dir}/cq-environment.log" 2>&1; then
  fail 'caller-selected CQ environment and unreviewed tfvars were accepted'
fi
grep -q 'CQ target is bound to the reviewed dev environment; got ENV=staging' \
  "${fixture_dir}/cq-environment.log" ||
  fail 'CQ environment authority rejection was not explicit'

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

if make "${common_args[@]}" aws-account-guard \
  AWS_ACCOUNT_ID=000000000000 AWS_ACCOUNT_ID_cq=000000000000 EXPECTED_AWS_ACCOUNT_ID=000000000000 \
  >"${fixture_dir}/constant-account.log" 2>&1; then
  fail 'caller redefined the trusted CQ account constant'
fi
grep -q 'does not match target cq (014155356804)' "${fixture_dir}/constant-account.log" ||
  fail 'CQ account authority constants were not immutable'

if make "${common_args[@]}" aws-account-guard \
  TF_PLAN_FILE=.tfplan.attacker EXPECTED_TF_PLAN_FILE=.tfplan.attacker \
  >"${fixture_dir}/constant-plan.log" 2>&1; then
  fail 'caller redefined the trusted plan path'
fi
grep -q 'use .tfplan.dev.cq' "${fixture_dir}/constant-plan.log" ||
  fail 'target-bound plan authority was not immutable'

dry_run_plan="$(
  make "${common_args[@]}" -n plan \
    PREFIX=unreviewed- \
    TF_VAR_FILE_ARG=-var-file=/tmp/unreviewed.tfvars \
    cq_tf_vars=TF_VAR_prefix=unreviewed- \
    tf_vars=TF_VAR_prefix=unreviewed-
)"
[[ "${dry_run_plan}" == *"-var-file=./dev.cq.tfvars"* ]] ||
  fail 'CQ did not select the canonical dev.cq.tfvars'
[[ "${dry_run_plan}" != *"unreviewed"* ]] ||
  fail 'caller-defined recipe fragments overrode reviewed CQ configuration'

dry_run_apply="$(make "${common_args[@]}" -n apply)"
[[ "${dry_run_apply}" == *".tfplan.dev.cq"* ]] ||
  fail 'apply did not use the CQ-bound plan path'

dry_run_init="$(make "${common_args[@]}" -n init)"
[[ "${dry_run_init}" == *"terraform apply -var-file=./dev.cq.tfvars -target=module.init"* ]] ||
  fail 'init apply did not use the canonical CQ variables'

if make "${common_args[@]}" aws-account-guard TF_VAR_prefix=unreviewed- >"${fixture_dir}/override.log" 2>&1; then
  fail 'ambient TF_VAR_prefix was accepted for CQ'
fi
grep -q 'CQ rejects ambient Terraform variable overrides: TF_VAR_prefix' "${fixture_dir}/override.log" ||
  fail 'ambient TF_VAR rejection was not explicit'

for cli_var in TF_CLI_ARGS TF_CLI_ARGS_plan; do
  if make "${common_args[@]}" aws-account-guard "${cli_var}=-destroy" >"${fixture_dir}/cli-args.log" 2>&1; then
    fail "ambient ${cli_var} was accepted for CQ"
  fi
  grep -q "CQ rejects ambient Terraform CLI arguments: ${cli_var}" "${fixture_dir}/cli-args.log" ||
    fail "ambient ${cli_var} rejection was not explicit"
done

grep -q '^bucket_prefix = "e2b-014155356804-"[[:space:]]*$' "${provider_dir}/dev.cq.tfvars" ||
  fail 'canonical CQ tfvars omitted the required bucket_prefix'

: >"${implicit_tfvars}"
if make "${common_args[@]}" aws-account-guard >"${fixture_dir}/implicit-tfvars.log" 2>&1; then
  fail 'Terraform implicit variable file was accepted'
fi
grep -q 'Terraform implicit variable files are forbidden' "${fixture_dir}/implicit-tfvars.log" ||
  fail 'implicit Terraform variable-file rejection was not explicit'
rm -f -- "${implicit_tfvars}"

make "${common_args[@]}" aws-account-guard

# Exercise the shared prerequisite itself with a deliberately wrong account.
# Target wiring is checked structurally below, so no mutating recipe can run.
if make --no-print-directory -C "${repo_root}" -f scripts/aws-account-authority.mk aws-write-account-guard \
  PROVIDER=aws \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=000000000000 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/shared-guard.log" 2>&1; then
  fail 'shared AWS write guard accepted a mismatched account'
fi
grep -q 'does not match target cq (014155356804)' "${fixture_dir}/shared-guard.log" ||
  fail 'shared AWS write guard did not delegate to canonical authority'

# The package-owned canonical destination passes both the raw-origin gate and
# the expanded syntax/account validator.
make --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
  PROVIDER=aws \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform

# A caller cannot replace the package-owned ECR destination, disguise an ECR
# write as GCP, or redirect the shared authority include. Direct values are
# rejected by origin before their contents can be expanded by GNU Make.
if make --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
  PROVIDER=gcp \
  IMAGE_REGISTRY=319933937176.dkr.ecr.us-east-1.amazonaws.com/attacker \
  AWS_WRITE_DESTINATION=014155356804.dkr.ecr.us-east-1.amazonaws.com/claimed \
  AWS_ACCOUNT_AUTHORITY_ROOT=/tmp/attacker \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/disguised-ecr.log" 2>&1; then
  fail 'cross-account ECR destination bypassed AWS account authority'
fi
grep -q 'ECR destination source IMAGE_REGISTRY must be Makefile-owned; origin is command line' \
  "${fixture_dir}/disguised-ecr.log" ||
  fail 'caller-owned ECR destination rejection was not explicit'

if make --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
  PROVIDER=aws \
  IMAGE_REGISTRY= \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/missing-ecr.log" 2>&1; then
  fail 'empty ECR destination bypassed AWS account authority'
fi
grep -q 'ECR destination source IMAGE_REGISTRY must be Makefile-owned; origin is command line' \
  "${fixture_dir}/missing-ecr.log" ||
  fail 'empty ECR destination rejection was not explicit'

# The destination must be one complete repository reference. Matching one word
# cannot authorize additional Docker flags or a second cross-account tag.
if make --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
  PROVIDER=aws \
  "IMAGE_REGISTRY=014155356804.dkr.ecr.us-east-1.amazonaws.com/core/client-proxy --tag 319933937176.dkr.ecr.us-east-1.amazonaws.com/attacker" \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/multiple-ecr.log" 2>&1; then
  fail 'multiple ECR destination words bypassed AWS account authority'
fi
grep -q 'ECR destination source IMAGE_REGISTRY must be Makefile-owned; origin is command line' \
  "${fixture_dir}/multiple-ecr.log" ||
  fail 'multiple ECR destination rejection was not explicit'

injection_marker="${fixture_dir}/destination-injection-ran"
if make --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
  PROVIDER=aws \
  "IMAGE_REGISTRY=014155356804.dkr.ecr.us-east-1.amazonaws.com/core/client-proxy;touch${injection_marker}" \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/malformed-ecr.log" 2>&1; then
  fail 'malformed ECR destination bypassed AWS account authority'
fi
grep -q 'ECR destination source IMAGE_REGISTRY must be Makefile-owned; origin is command line' \
  "${fixture_dir}/malformed-ecr.log" ||
  fail 'malformed ECR destination rejection was not explicit'
[[ ! -e "${injection_marker}" ]] ||
  fail 'ECR destination text was executed as shell source'

# GNU Make function syntax is more dangerous than shell metacharacters: Make
# evaluates $(shell ...) while expanding a recursive variable. The origin gate
# must reject both command-line and environment-override values without ever
# expanding their raw contents.
make_function_marker="${fixture_dir}/make-function-injection-ran"
make_function_destination='014155356804.dkr.ecr.us-east-1.amazonaws.com/core/client-proxy$(shell touch '"${make_function_marker}"')'
if make --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
  PROVIDER=aws \
  "IMAGE_REGISTRY=${make_function_destination}" \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/make-function-ecr.log" 2>&1; then
  fail 'GNU Make function in a command-line ECR destination bypassed authority'
fi
grep -q 'ECR destination source IMAGE_REGISTRY must be Makefile-owned; origin is command line' \
  "${fixture_dir}/make-function-ecr.log" ||
  fail 'command-line Make-function rejection was not explicit'
[[ ! -e "${make_function_marker}" ]] ||
  fail 'command-line ECR destination executed a GNU Make function'

if env IMAGE_REGISTRY="${make_function_destination}" \
  make -e --no-print-directory -C "${repo_root}/packages/client-proxy" aws-write-account-guard \
    PROVIDER=aws \
    ENV=dev \
    SIG_AWS_ACCOUNT_TARGET=cq \
    AWS_PROFILE=test-cq \
    AWS_ACCOUNT_ID=014155356804 \
    AWS_REGION=us-east-1 \
    TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
    TF=terraform >"${fixture_dir}/environment-function-ecr.log" 2>&1; then
  fail 'GNU Make function in an environment ECR destination bypassed authority'
fi
grep -q 'ECR destination source IMAGE_REGISTRY must be Makefile-owned; origin is environment override' \
  "${fixture_dir}/environment-function-ecr.log" ||
  fail 'environment Make-function rejection was not explicit'
[[ ! -e "${make_function_marker}" ]] ||
  fail 'environment ECR destination executed a GNU Make function'

# S3 writers bind the exact recipe prefix. A caller cannot replace either the
# source variable or the claimed guard variable with the other account.
if make --no-print-directory -C "${repo_root}" aws-write-account-guard \
  PROVIDER=aws \
  AWS_BUCKET_PREFIX=e2b-319933937176- \
  AWS_WRITE_S3_PREFIX=e2b-014155356804- \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/cross-account-s3.log" 2>&1; then
  fail 'cross-account S3 bucket prefix bypassed AWS account authority'
fi
grep -q 'S3 bucket prefix source AWS_BUCKET_PREFIX must be Makefile-owned; origin is command line' \
  "${fixture_dir}/cross-account-s3.log" ||
  fail 'caller-owned S3 prefix rejection was not explicit'

if make --no-print-directory -C "${repo_root}" aws-write-account-guard \
  PROVIDER=aws \
  AWS_BUCKET_PREFIX= \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/missing-s3.log" 2>&1; then
  fail 'empty S3 bucket prefix bypassed AWS account authority'
fi
grep -q 'S3 bucket prefix source AWS_BUCKET_PREFIX must be Makefile-owned; origin is command line' \
  "${fixture_dir}/missing-s3.log" ||
  fail 'empty S3 prefix rejection was not explicit'

s3_make_function_marker="${fixture_dir}/s3-make-function-injection-ran"
s3_make_function_prefix='e2b-014155356804-$(shell touch '"${s3_make_function_marker}"')'
if make --no-print-directory -C "${repo_root}" aws-write-account-guard \
  PROVIDER=aws \
  "AWS_BUCKET_PREFIX=${s3_make_function_prefix}" \
  ENV=dev \
  SIG_AWS_ACCOUNT_TARGET=cq \
  AWS_PROFILE=test-cq \
  AWS_ACCOUNT_ID=014155356804 \
  AWS_REGION=us-east-1 \
  TERRAFORM_STATE_BUCKET=commonquant-e2b-tfstate \
  TF=terraform >"${fixture_dir}/make-function-s3.log" 2>&1; then
  fail 'GNU Make function in a command-line S3 prefix bypassed authority'
fi
grep -q 'S3 bucket prefix source AWS_BUCKET_PREFIX must be Makefile-owned; origin is command line' \
  "${fixture_dir}/make-function-s3.log" ||
  fail 'command-line S3 Make-function rejection was not explicit'
[[ ! -e "${s3_make_function_marker}" ]] ||
  fail 'command-line S3 bucket prefix executed a GNU Make function'

# Every guarded destination must name and unexport the source its writer later
# expands. Otherwise a future package could accidentally restore the pre-guard
# Make-function execution path.
source_contracts=(
  'Makefile|override AWS_WRITE_S3_PREFIX_SOURCE := AWS_BUCKET_PREFIX|unexport AWS_BUCKET_PREFIX'
  'packages/api/Makefile|override AWS_WRITE_DESTINATION_SOURCE := REGISTRY_PREFIX|unexport REGISTRY_PREFIX'
  'packages/clickhouse/Makefile|override AWS_WRITE_DESTINATION_SOURCE := CLICKHOUSE_MIGRATOR_IMAGE|unexport CLICKHOUSE_MIGRATOR_IMAGE'
  'packages/client-proxy/Makefile|override AWS_WRITE_DESTINATION_SOURCE := IMAGE_REGISTRY|unexport IMAGE_REGISTRY'
  'packages/dashboard-api/Makefile|override AWS_WRITE_DESTINATION_SOURCE := IMAGE_REGISTRY|unexport IMAGE_REGISTRY'
  'packages/docker-reverse-proxy/Makefile|override AWS_WRITE_DESTINATION_SOURCE := IMAGE_REGISTRY|unexport IMAGE_REGISTRY'
  'packages/envd/Makefile|override AWS_WRITE_S3_PREFIX_SOURCE := AWS_BUCKET_PREFIX|unexport AWS_BUCKET_PREFIX'
  'packages/nomad-nodepool-apm/Makefile|override AWS_WRITE_S3_PREFIX_SOURCE := AWS_BUCKET_PREFIX|unexport AWS_BUCKET_PREFIX'
  'packages/orchestrator/Makefile|override AWS_WRITE_S3_PREFIX_SOURCE := AWS_BUCKET_PREFIX|unexport AWS_BUCKET_PREFIX'
)
for contract in "${source_contracts[@]}"; do
  IFS='|' read -r file source_binding unexport_binding <<<"${contract}"
  grep -Fq "${source_binding}" "${repo_root}/${file}" ||
    fail "AWS writer lacks an immutable source binding: ${file}"
  grep -Fq "${unexport_binding}" "${repo_root}/${file}" ||
    fail "AWS writer source is still exported before its origin gate: ${file}"
done

guarded_targets=(
  'Makefile|copy-public-builds: aws-write-account-guard'
  'packages/client-proxy/Makefile|build-and-upload: aws-write-account-guard'
  'Makefile|build-and-upload/%: aws-write-account-guard'
  'Makefile|build-and-upload/orchestrator: aws-write-account-guard'
  'Makefile|build-and-upload/template-manager: aws-write-account-guard'
  'Makefile|build-and-upload/clean-nfs-cache: aws-write-account-guard'
  'Makefile|build-and-upload/clickhouse-migrator: aws-write-account-guard'
  'packages/api/Makefile|build-and-upload: aws-write-account-guard'
  'packages/dashboard-api/Makefile|build-and-upload: aws-write-account-guard'
  'packages/docker-reverse-proxy/Makefile|build-and-upload: aws-write-account-guard'
  'packages/clickhouse/Makefile|build-and-upload: aws-write-account-guard'
  'packages/envd/Makefile|upload: aws-write-account-guard'
  'packages/envd/Makefile|promote: aws-write-account-guard'
  'packages/orchestrator/Makefile|upload/clean-nfs-cache: aws-write-account-guard'
  'packages/orchestrator/Makefile|upload/orchestrator: aws-write-account-guard'
  'packages/orchestrator/Makefile|upload/template-manager: aws-write-account-guard'
  'packages/nomad-nodepool-apm/Makefile|upload: aws-write-account-guard'
  'iac/provider-aws/nomad-cluster-disk-image/Makefile|build: aws-write-account-guard'
)
for contract in "${guarded_targets[@]}"; do
  file="${contract%%|*}"
  declaration="${contract#*|}"
  grep -Fq "${declaration}" "${repo_root}/${file}" ||
    fail "AWS write target lacks canonical guard: ${file} ${declaration}"
done

make --no-print-directory -f "${repo_root}/scripts/aws-account-authority.mk" PROVIDER=gcp aws-write-account-guard

sig_args=(
  --no-print-directory
  -C "${provider_dir}"
  ENV=dev
  SIG_AWS_ACCOUNT_TARGET=sig
  AWS_PROFILE=test-sig
  AWS_ACCOUNT_ID=319933937176
  AWS_REGION=us-east-1
  TERRAFORM_STATE_BUCKET=superintelligent-group-terraform-state
  TF=terraform
  TF_VAR_FILE=./dev.sig.tfvars.reference
)

sig_args_without_var_file=("${sig_args[@]:0:${#sig_args[@]}-1}")
if make "${sig_args_without_var_file[@]}" aws-account-guard >"${fixture_dir}/sig-var-required.log" 2>&1; then
  fail 'SIG rollback accepted an implicit variable-file selection'
fi
grep -q 'SIG rollback requires explicit TF_VAR_FILE=./dev.sig.tfvars.reference' \
  "${fixture_dir}/sig-var-required.log" ||
  fail 'SIG explicit rollback-file rejection was not explicit'

if make "${sig_args[@]}" aws-account-guard TF_VAR_FILE=./dev.cq.tfvars >"${fixture_dir}/sig-var-file.log" 2>&1; then
  fail 'SIG rollback accepted a noncanonical variable file'
fi
grep -q 'SIG rollback requires the reviewed variable file ./dev.sig.tfvars.reference' \
  "${fixture_dir}/sig-var-file.log" ||
  fail 'SIG reviewed rollback-file rejection was not explicit'

if make "${sig_args[@]}" aws-account-guard AWS_REGION=us-west-2 >"${fixture_dir}/sig-region.log" 2>&1; then
  fail 'noncanonical SIG rollback region was accepted'
fi
grep -q 'does not match target sig (us-east-1)' "${fixture_dir}/sig-region.log" ||
  fail 'SIG rollback region rejection was not explicit'

if make "${sig_args[@]}" aws-account-guard TERRAFORM_STATE_BUCKET=unreviewed-state >"${fixture_dir}/sig-state.log" 2>&1; then
  fail 'noncanonical SIG rollback state bucket was accepted'
fi
grep -q 'does not match target sig (superintelligent-group-terraform-state)' "${fixture_dir}/sig-state.log" ||
  fail 'SIG rollback state-bucket rejection was not explicit'

grep -q '^bucket_prefix = "e2b-319933937176-"[[:space:]]*$' \
  "${provider_dir}/dev.sig.tfvars.reference" ||
  fail 'canonical SIG rollback tfvars omitted the account-bound bucket_prefix'

make "${sig_args[@]}" aws-account-guard

if grep -Fq '$(file <' "${provider_dir}/Makefile"; then
  fail 'Makefile still requires GNU Make 4 file-read syntax'
fi

printf 'AWS deployment authority checks passed.\n'
