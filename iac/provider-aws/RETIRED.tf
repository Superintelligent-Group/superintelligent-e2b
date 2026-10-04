# SUP-1845 (2026-10-04): this root is RETIRED. Do not plan, apply or destroy it.
#
# Its isolated-VPC cluster (e2b-api / e2b-control-server / e2b-orch-* /
# e2b-ingress, vpc-0d940ed974957ab7c, RDS e2b-postgres) was destroyed by a
# reviewed targeted destroy. The 17 live objects this state also claimed (E2B
# DNS, the ACM validation CNAME, the wake/shutdown Lambdas, the idle-check rule,
# e2b-core/{api,db-migrator,admin-postgres}, fc-env-pipeline) are now owned by
# the superintelligent.group dev root (infrastructure/environments/dev). The
# Nomad provider here points at the LIVE Nomad, so any apply of this root would
# recreate DNS/Lambdas and touch live Nomad jobs.
#
# The state is archived at s3://commonquant-e2b-tfstate/terraform/archive/.
# This impossible version constraint makes every terraform command in this
# directory fail before it can read or write state.
terraform {
  required_version = "= 0.0.0"
}
