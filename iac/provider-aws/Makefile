ENV ?= $(if $(wildcard ../../.last_used_env),$(strip $(file <../../.last_used_env)),not-set)
ENV_FILE := ../../.env.${ENV}

-include ${ENV_FILE}

TF ?= terraform
SIG_AWS_ACCOUNT_TARGET ?=

# Account selection is authority, not ambient convenience. The CQ deployment has
# a tracked, reviewed configuration; selecting cq must therefore use that file.
AWS_ACCOUNT_ID_cq := 014155356804
AWS_ACCOUNT_ID_sig := 319933937176
EXPECTED_AWS_ACCOUNT_ID := $(AWS_ACCOUNT_ID_$(SIG_AWS_ACCOUNT_TARGET))
AWS_REGION_cq := us-east-1
TF_STATE_BUCKET_cq := commonquant-e2b-tfstate
EXPECTED_AWS_REGION := $(AWS_REGION_$(SIG_AWS_ACCOUNT_TARGET))
EXPECTED_TF_STATE_BUCKET := $(TF_STATE_BUCKET_$(SIG_AWS_ACCOUNT_TARGET))
TF_VAR_FILE ?= $(if $(filter cq,$(SIG_AWS_ACCOUNT_TARGET)),./${ENV}.cq.tfvars,./.terraform.${ENV}.tfvars)
TF_PLAN_FILE ?= .tfplan.${ENV}.${SIG_AWS_ACCOUNT_TARGET}
TF_VAR_FILE_ARG := $(if $(wildcard $(TF_VAR_FILE)),-var-file=$(TF_VAR_FILE))
TERRAFORM_STATE_BUCKET ?= $(AWS_ACCOUNT_ID)-terraform-state
TEMPLATE_BUCKET_LOCATION ?= $(AWS_REGION)

BUCKET_PREFIX ?= $(PREFIX)$(AWS_ACCOUNT_ID)-

SKIP_FMT ?= false
define tf_fmt
$(if $(filter true,$(SKIP_FMT)),,$(TF) fmt -recursive)
endef

# Keep environment-to-Terraform name mappings explicit and shell-independent.
define tfvar
$(if $(value $(strip $(1))), TF_VAR_$(strip $(2))=$($(strip $(1))))
endef

tf_vars := AWS_PROFILE=$(AWS_PROFILE) AWS_REGION=$(AWS_REGION) \
	TF_VAR_environment=$(TERRAFORM_ENVIRONMENT) \
	$(call tfvar,PREFIX,prefix) \
	$(call tfvar,DOMAIN_NAME,domain_name) \
	$(call tfvar,BUCKET_PREFIX,bucket_prefix) \
	$(call tfvar,CLIENT_CLUSTER_SIZE,client_cluster_size) \
	$(call tfvar,CLIENT_SERVER_MACHINE_TYPE,client_server_machine_type) \
	$(call tfvar,CLIENT_SERVER_NESTED_VIRTUALIZATION,client_server_nested_virtualization) \
	$(call tfvar,CLICKHOUSE_CLUSTER_SIZE,clickhouse_cluster_size) \
	$(call tfvar,REDIS_INSTANCE_TYPE,redis_instance_type) \
	$(call tfvar,REDIS_MANAGED,redis_managed) \
	$(call tfvar,API_CLUSTER_SIZE,api_cluster_size) \
	$(call tfvar,API_INTERNAL_GRPC_PORT,api_internal_grpc_port) \
	$(call tfvar,API_SERVER_MACHINE_TYPE,api_server_machine_type) \
	$(call tfvar,CLIENT_PROXY_COUNT,client_proxy_count) \
	$(call tfvar,INGRESS_COUNT,ingress_count) \
	$(call tfvar,BUILD_CLUSTER_SIZE,build_cluster_size) \
	$(call tfvar,BUILD_SERVER_MACHINE_TYPE,build_server_machine_type) \
	$(call tfvar,BUILD_SERVER_NESTED_VIRTUALIZATION,build_server_nested_virtualization) \
	$(call tfvar,AWS_ACCOUNT_ID,aws_account_id) \
	$(call tfvar,AWS_REGION,aws_region) \
	$(call tfvar,ENABLE_OTEL_ROUTER_LOGS,enable_otel_router_logs) \
	$(call tfvar,OTEL_ROUTER_HTTP_PORT,otel_router_http_port) \
	$(call tfvar,ENABLE_OTEL_ROUTER_METRICS,enable_otel_router_metrics) \
	$(call tfvar,OTEL_ROUTER_GRPC_PORT,otel_router_grpc_port) \
	$(call tfvar,DB_MAX_OPEN_CONNECTIONS,db_max_open_connections) \
	$(call tfvar,DB_MIN_IDLE_CONNECTIONS,db_min_idle_connections) \
	$(call tfvar,AUTH_DB_MAX_OPEN_CONNECTIONS,auth_db_max_open_connections) \
	$(call tfvar,AUTH_DB_MIN_IDLE_CONNECTIONS,auth_db_min_idle_connections) \
	$(call tfvar,ALLOW_SANDBOX_INTERNAL_CIDRS,allow_sandbox_internal_cidrs)

.PHONY: aws-account-guard
aws-account-guard:
	@if [ -z "$(SIG_AWS_ACCOUNT_TARGET)" ]; then \
		printf "SIG_AWS_ACCOUNT_TARGET is required (cq or sig).\n" >&2; \
		exit 1; \
	fi
	@if [ -z "$(EXPECTED_AWS_ACCOUNT_ID)" ]; then \
		printf "Unsupported SIG_AWS_ACCOUNT_TARGET: %s (expected cq or sig).\n" "$(SIG_AWS_ACCOUNT_TARGET)" >&2; \
		exit 1; \
	fi
	@if [ -z "$(AWS_PROFILE)" ]; then \
		printf "AWS_PROFILE is required; ambient AWS credentials are not deployment authority.\n" >&2; \
		exit 1; \
	fi
	@if [ "$(AWS_ACCOUNT_ID)" != "$(EXPECTED_AWS_ACCOUNT_ID)" ]; then \
		printf "Configured AWS_ACCOUNT_ID %s does not match target %s (%s).\n" "$(AWS_ACCOUNT_ID)" "$(SIG_AWS_ACCOUNT_TARGET)" "$(EXPECTED_AWS_ACCOUNT_ID)" >&2; \
		exit 1; \
	fi
	@if [ -n "$(EXPECTED_AWS_REGION)" ] && [ "$(AWS_REGION)" != "$(EXPECTED_AWS_REGION)" ]; then \
		printf "Configured AWS region %s does not match target %s (%s).\n" "$(AWS_REGION)" "$(SIG_AWS_ACCOUNT_TARGET)" "$(EXPECTED_AWS_REGION)" >&2; \
		exit 1; \
	fi
	@if [ -n "$(EXPECTED_TF_STATE_BUCKET)" ] && [ "$(TERRAFORM_STATE_BUCKET)" != "$(EXPECTED_TF_STATE_BUCKET)" ]; then \
		printf "Terraform state bucket %s does not match target %s (%s).\n" "$(TERRAFORM_STATE_BUCKET)" "$(SIG_AWS_ACCOUNT_TARGET)" "$(EXPECTED_TF_STATE_BUCKET)" >&2; \
		exit 1; \
	fi
	@actual="$$(AWS_PROFILE="$(AWS_PROFILE)" AWS_REGION="$(AWS_REGION)" aws sts get-caller-identity --query Account --output text 2>/dev/null)" || { \
		printf "Unable to resolve AWS caller identity for profile %s.\n" "$(AWS_PROFILE)" >&2; \
		exit 1; \
	}; \
	if [ "$$actual" != "$(EXPECTED_AWS_ACCOUNT_ID)" ]; then \
		printf "AWS caller account %s does not match target %s (%s).\n" "$$actual" "$(SIG_AWS_ACCOUNT_TARGET)" "$(EXPECTED_AWS_ACCOUNT_ID)" >&2; \
		exit 1; \
	fi
	@if [ ! -f "$(TF_VAR_FILE)" ]; then \
		printf "Canonical Terraform variable file is missing: %s\n" "$(TF_VAR_FILE)" >&2; \
		printf "Set TF_VAR_FILE explicitly only for an intentional reviewed rollback configuration.\n" >&2; \
		exit 1; \
	fi

.PHONY: provider-login
provider-login: aws-account-guard
	aws ecr get-login-password --region $(AWS_REGION) --profile $(AWS_PROFILE) | docker login --username AWS --password-stdin $(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com

.PHONY: switch
switch: aws-account-guard
	$(tf_vars) $(TF) init -input=false -upgrade -reconfigure -backend-config="bucket=$(TERRAFORM_STATE_BUCKET)"

.PHONY: init
init: aws-account-guard
	# Create S3 bucket for Terraform state if it doesn't exist
	# us-east-1 requires omitting LocationConstraint; all other regions require it
	@if [ "$(TEMPLATE_BUCKET_LOCATION)" = "us-east-1" ]; then \
		aws s3api create-bucket --bucket $(TERRAFORM_STATE_BUCKET) --region $(TEMPLATE_BUCKET_LOCATION) --profile $(AWS_PROFILE) 2>/dev/null || true; \
	else \
		aws s3api create-bucket --bucket $(TERRAFORM_STATE_BUCKET) --region $(TEMPLATE_BUCKET_LOCATION) --profile $(AWS_PROFILE) --create-bucket-configuration LocationConstraint=$(TEMPLATE_BUCKET_LOCATION) 2>/dev/null || true; \
	fi
	$(tf_vars) $(TF) init -upgrade -reconfigure -backend-config="bucket=${TERRAFORM_STATE_BUCKET}"
	$(tf_vars) $(TF) apply -target=module.init -input=false -compact-warnings

.PHONY: plan
plan: aws-account-guard
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	@ $(call tf_fmt)
	@ $(tf_vars) $(TF) plan $(TF_VAR_FILE_ARG) -out=$(TF_PLAN_FILE) -input=false -compact-warnings -parallelism=20

plan_only_jobs_moved_targets = -target="random_password.volume_token_key" -target="module.nomad.random_password.volume_token_key"

.PHONY: plan-only-jobs
plan-only-jobs: aws-account-guard
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(call tf_fmt)
	@ $(tf_vars) $(TF) plan $(TF_VAR_FILE_ARG) -out=$(TF_PLAN_FILE) -input=false -compact-warnings -parallelism=20 -target=module.nomad $(plan_only_jobs_moved_targets)

.PHONY: plan-without-jobs
plan-without-jobs: aws-account-guard
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(eval TARGET := $(shell cat main.tf | grep "^module" | awk '{print $$2}' | tr ' ' '\n' | grep -v -e "nomad" | awk '{print "-target=module." $$0 ""}' | xargs))
	@ $(tf_vars) $(TF) plan $(TF_VAR_FILE_ARG) -out=$(TF_PLAN_FILE) -input=false -compact-warnings -parallelism=20 $(TARGET)

.PHONY: apply
apply: aws-account-guard
	@ printf "Applying Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	@ $(tf_vars) $(TF) apply -auto-approve -input=false -compact-warnings -parallelism=20 $(TF_PLAN_FILE)

.PHONY: state-migrate
state-migrate: aws-account-guard
	@ printf "Running Terraform state migrations for env: $(ENV)\n\n"
	@ $(tf_vars) $(TF) state mv 'module.nomad.random_password.volume_token_key' 'random_password.volume_token_key' || true

.PHONY: apply-init
apply-init: aws-account-guard
	@ $(tf_vars) $(TF) apply $(TF_VAR_FILE_ARG) -target=module.init -auto-approve -input=false -compact-warnings

.PHONY: move
move: aws-account-guard
	@ printf "Moving resource for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	@ $(tf_vars) $(TF) state mv "$(SOURCE)" "$(TARGET)" -no-color

.PHONY: destroy
destroy: aws-account-guard
	@ printf "Destroying Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	@ $(tf_vars) $(TF) destroy $(TF_VAR_FILE_ARG)
