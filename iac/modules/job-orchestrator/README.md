# Orchestrator environment values

`job_env_vars` takes logical strings. Do not pre-escape JSON, quotes or
backslashes for HCL. Existing normalization omits null/blank values and trims
surrounding whitespace. The jobspec template encodes each value exactly once,
then escapes HCL template introducers so Nomad's parser preserves their text.
For example, normal `jsonencode(config)` can supply
`NETWORK_USAGE_PROTECTED_DELIVERY`.

This is a jobspec parsing boundary, not a way to disable Nomad's later runtime
interpolation. A caller's `${node.unique.name}` remains that exact string in
the parsed job, just like the built-in NODE_ID. Nomad may subsequently resolve
recognized runtime variables when starting the task. No new Terraform/HCL
expression language is exposed through map values.

Both AWS and GCP forward their merged `orchestrator_env_vars` to this module;
their built-in orchestrator values are logical strings. No checked-in caller
requires pre-escaped HCL. Their API-only AUTH_PROVIDER_CONFIG workaround targets
a different job template and remains unchanged. Artifact URLs, checksum keepers,
empty filtering and built-in node attribute expressions remain unchanged.

Run `node --test scripts/local-proof/orchestrator-env.test.mjs` from the repository
root with Terraform and Nomad 2.0.5 (the AWS host-image pin) on PATH. Optional
TERRAFORM_BINARY/NOMAD_BINARY select local executables. The test renders the real
template and production normalization/change-detection locals in a temporary
provider-free Terraform module, then runs `nomad job run -output`; it never
submits a job. The companion Go TestNomadProtectedConfigFixture validates the
same synthetic fixture with the production strict configuration decoder.
No fixture identity, retention or capacity value is a deployment choice.
