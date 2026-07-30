# Shared fail-closed authority for every Make target that can mutate AWS.
#
# Include this file from root or nested Makefiles, then add
# `aws-write-account-guard` as a prerequisite of each provider-aware write
# target. GCP remains a no-op; AWS delegates to the canonical provider guard so
# Terraform, Packer, S3, and ECR all enforce the same account decision.
#
# Identity alone is not sufficient: a principal can have cross-account access.
# Callers that write to ECR bind AWS_WRITE_DESTINATION to the registry they
# actually push, while S3 callers bind AWS_WRITE_S3_PREFIX to the bucket prefix
# used by their recipe. The owning Makefile also names the source variable and
# unexports it. A prerequisite checks that source's origin without expanding its
# value, so command-line or environment input containing GNU Make functions
# cannot execute before destination validation.
#
# The expanded value reaches Bash only after that origin gate and is quoted as a
# single shell word. This preserves literal data for the syntax validator.
override AWS_ACCOUNT_AUTHORITY_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
override AWS_WRITE_ACCOUNT_REQUIRED = $(strip $(if $(filter aws,$(PROVIDER)),true, \
	$(if $(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION)),true,false)))
override AWS_WRITE_EXPECTED_ACCOUNT_cq := 014155356804
override AWS_WRITE_EXPECTED_ACCOUNT_sig := 319933937176
override AWS_WRITE_EXPECTED_ACCOUNT_ID = $(AWS_WRITE_EXPECTED_ACCOUNT_$(SIG_AWS_ACCOUNT_TARGET))
override AWS_WRITE_EXPECTED_ECR_PREFIX = $(AWS_WRITE_EXPECTED_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com/
override AWS_WRITE_EXPECTED_S3_PREFIX = e2b-$(AWS_WRITE_EXPECTED_ACCOUNT_ID)-
override aws_write_shell_quote = '$(subst ','"'"',$(1))'

export AWS_WRITE_EXPECTED_ECR_PREFIX SIG_AWS_ACCOUNT_TARGET

.PHONY: aws-write-input-origin-guard
aws-write-input-origin-guard:
	$(if $(filter true,$(AWS_WRITE_ECR_DESTINATION_REQUIRED)), \
		$(if $(strip $(AWS_WRITE_DESTINATION_SOURCE)),, \
			$(error ECR destination source is not declared for target $(SIG_AWS_ACCOUNT_TARGET))))
	$(if $(filter true,$(AWS_WRITE_ECR_DESTINATION_REQUIRED)), \
		$(if $(or $(findstring environment,$(origin $(AWS_WRITE_DESTINATION_SOURCE))), \
			$(findstring command line,$(origin $(AWS_WRITE_DESTINATION_SOURCE))), \
			$(filter undefined,$(origin $(AWS_WRITE_DESTINATION_SOURCE)))), \
			$(error ECR destination source $(AWS_WRITE_DESTINATION_SOURCE) must be Makefile-owned; origin is $(origin $(AWS_WRITE_DESTINATION_SOURCE)))))
	$(if $(filter true,$(AWS_WRITE_S3_DESTINATION_REQUIRED)), \
		$(if $(strip $(AWS_WRITE_S3_PREFIX_SOURCE)),, \
			$(error S3 bucket prefix source is not declared for target $(SIG_AWS_ACCOUNT_TARGET))))
	$(if $(filter true,$(AWS_WRITE_S3_DESTINATION_REQUIRED)), \
		$(if $(or $(findstring environment,$(origin $(AWS_WRITE_S3_PREFIX_SOURCE))), \
			$(findstring command line,$(origin $(AWS_WRITE_S3_PREFIX_SOURCE))), \
			$(filter undefined,$(origin $(AWS_WRITE_S3_PREFIX_SOURCE)))), \
			$(error S3 bucket prefix source $(AWS_WRITE_S3_PREFIX_SOURCE) must be Makefile-owned; origin is $(origin $(AWS_WRITE_S3_PREFIX_SOURCE)))))
	@:

.PHONY: aws-write-account-guard
aws-write-account-guard: aws-write-input-origin-guard
	$(if $(and $(AWS_WRITE_EXPECTED_ACCOUNT_ID),$(or $(and $(filter aws,$(PROVIDER)),$(filter true,$(AWS_WRITE_ECR_DESTINATION_REQUIRED))), \
		$(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION)))), \
		$(if $(strip $(AWS_WRITE_DESTINATION)), \
			$(if $(filter 1,$(words $(AWS_WRITE_DESTINATION))), \
				$(if $(filter $(AWS_WRITE_EXPECTED_ECR_PREFIX)%,$(AWS_WRITE_DESTINATION)),, \
					$(error ECR destination $(AWS_WRITE_DESTINATION) does not match target $(SIG_AWS_ACCOUNT_TARGET) ($(AWS_WRITE_EXPECTED_ECR_PREFIX)...))), \
				$(error ECR destination must be exactly one registry reference for target $(SIG_AWS_ACCOUNT_TARGET))), \
			$(error ECR destination is required for target $(SIG_AWS_ACCOUNT_TARGET))))
	$(if $(and $(filter aws,$(PROVIDER)),$(AWS_WRITE_EXPECTED_ACCOUNT_ID),$(filter true,$(AWS_WRITE_S3_DESTINATION_REQUIRED))), \
		$(if $(strip $(AWS_WRITE_S3_PREFIX)), \
			$(if $(filter 1,$(words $(AWS_WRITE_S3_PREFIX))), \
				$(if $(filter $(AWS_WRITE_EXPECTED_S3_PREFIX),$(AWS_WRITE_S3_PREFIX)),, \
					$(error S3 bucket prefix $(AWS_WRITE_S3_PREFIX) does not match target $(SIG_AWS_ACCOUNT_TARGET) ($(AWS_WRITE_EXPECTED_S3_PREFIX)))), \
				$(error S3 bucket prefix must be exactly one value for target $(SIG_AWS_ACCOUNT_TARGET))), \
			$(error S3 bucket prefix is required for target $(SIG_AWS_ACCOUNT_TARGET))))
	+@if [ -n "$(AWS_WRITE_EXPECTED_ACCOUNT_ID)" ] && \
		{ [ "$(AWS_WRITE_ECR_DESTINATION_REQUIRED)" = "true" ] || [ -n "$(findstring .dkr.ecr.,$(AWS_WRITE_DESTINATION))" ]; }; then \
		AWS_WRITE_DESTINATION=$(call aws_write_shell_quote,$(AWS_WRITE_DESTINATION)) \
		AWS_WRITE_EXPECTED_ECR_PREFIX=$(call aws_write_shell_quote,$(AWS_WRITE_EXPECTED_ECR_PREFIX)) \
		SIG_AWS_ACCOUNT_TARGET=$(call aws_write_shell_quote,$(SIG_AWS_ACCOUNT_TARGET)) \
			bash "$(AWS_ACCOUNT_AUTHORITY_ROOT)/scripts/validate-aws-ecr-destination.sh"; \
	fi
	+@if [ "$(AWS_WRITE_ACCOUNT_REQUIRED)" = "true" ]; then \
		make --no-print-directory -C $(AWS_ACCOUNT_AUTHORITY_ROOT)/iac/provider-aws aws-account-guard; \
	fi
