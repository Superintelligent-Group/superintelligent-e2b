# SUP-906: null by default; this source change creates no bucket or retention lock.
# Activation requires separately reviewed identities, duration and live IAM proof.
variable "network_evidence" {
  type = object({
    account_id        = string
    bucket_name       = string
    evidence_prefix   = string
    producer_role_arn = string
    reader_role_arn   = string
    retention_days    = number
    expiration_days   = number
  })
  default = null
}
module "network_evidence" {
  for_each          = var.network_evidence == null ? {} : { configured = var.network_evidence }
  source            = "./modules/network-evidence"
  account_id        = each.value.account_id
  bucket_name       = each.value.bucket_name
  evidence_prefix   = each.value.evidence_prefix
  producer_role_arn = each.value.producer_role_arn
  reader_role_arn   = each.value.reader_role_arn
  retention_days    = each.value.retention_days
  expiration_days   = each.value.expiration_days
}
output "network_evidence_policy_sha256" {
  value = { for name, evidence in module.network_evidence : name => evidence.policy_sha256 }
}
