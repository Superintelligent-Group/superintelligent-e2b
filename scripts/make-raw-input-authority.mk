# Parse-time authority for values that later select an AWS account, Terraform
# artifact, registry, or bucket. GNU Make command-line/environment variables
# are code-capable: a raw `$(shell ...)` executes as soon as a recursive value
# is expanded, before any recipe prerequisite can reject it.
#
# `$(value ...)` is the only safe way to inspect the raw text. Reject a dollar
# before any owning Makefile interpolates the value, then run the same check
# again immediately after optional .env inclusion.
ifndef AWS_MAKE_RAW_INPUT_AUTHORITY_INCLUDED
override AWS_MAKE_RAW_INPUT_AUTHORITY_INCLUDED := true
override aws_make_raw_dollar := $$

define aws_reject_make_expansion
$(if $(findstring $(aws_make_raw_dollar),$(value $(1))), \
	$(error AWS authority input $(1) contains GNU Make expansion syntax),)
endef

override AWS_MAKE_WRITER_INPUTS := \
	ENV \
	PROVIDER \
	SIG_AWS_ACCOUNT_TARGET \
	AWS_PROFILE \
	AWS_ACCOUNT_ID \
	AWS_REGION \
	PREFIX \
	AWS_BUCKET_PREFIX \
	REGISTRY_PREFIX \
	IMAGE_REGISTRY \
	CLICKHOUSE_MIGRATOR_IMAGE \
	GCP_REGION \
	GCP_PROJECT_ID

override AWS_MAKE_TERRAFORM_INPUTS := \
	$(AWS_MAKE_WRITER_INPUTS) \
	TF \
	TF_VAR_FILE \
	TF_PLAN_FILE \
	TERRAFORM_STATE_BUCKET \
	BUCKET_PREFIX

aws_guard_writer_inputs = $(foreach variable,$(AWS_MAKE_WRITER_INPUTS),$(call aws_reject_make_expansion,$(variable)))
aws_guard_terraform_inputs = $(foreach variable,$(AWS_MAKE_TERRAFORM_INPUTS),$(call aws_reject_make_expansion,$(variable)))

# Literal command-line values remain supported, but no command-line variable in
# an AWS-writer Make process may smuggle a Make function into parsing or the
# automatically exported recipe environment.
override AWS_MAKE_COMMAND_LINE_INPUTS := $(foreach variable,$(.VARIABLES),$(if $(findstring command line,$(origin $(variable))),$(variable)))
$(foreach variable,$(AWS_MAKE_COMMAND_LINE_INPUTS),$(call aws_reject_make_expansion,$(variable)))
endif
