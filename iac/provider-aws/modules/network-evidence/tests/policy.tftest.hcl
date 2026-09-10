mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
}
variables {
  account_id        = "123456789012"
  bucket_name       = "evidence-test"
  evidence_prefix   = "network-usage/v1/test"
  producer_role_arn = "arn:aws:iam::123456789012:role/client"
  reader_role_arn   = "arn:aws:iam::123456789012:role/reader"
  retention_days    = 30
  expiration_days   = 60
}
run "explicit_retention_plan" {
  command = plan
  assert {
    condition     = aws_s3_bucket.evidence.object_lock_enabled && !aws_s3_bucket.evidence.force_destroy && aws_s3_bucket_versioning.evidence.versioning_configuration[0].status == "Enabled"
    error_message = "Evidence storage must be versioned, locked and not force-destroyable."
  }
  assert {
    condition     = aws_s3_bucket_object_lock_configuration.evidence.rule[0].default_retention[0].days == 30 && aws_s3_bucket_object_lock_configuration.evidence.rule[0].default_retention[0].mode == "GOVERNANCE"
    error_message = "Retention must match explicit configuration."
  }
  assert {
    condition     = strcontains(output.policy_json, "$${aws:userid}") && strcontains(output.policy_json, "ec2:SourceInstanceARN") && strcontains(output.policy_json, "DenyReaderUnexpectedActions")
    error_message = "Rendered policy lost its runtime identity or reader boundary."
  }
}
run "reject_wrong_account" {
  command = plan
  variables { account_id = "999999999999" }
  expect_failures = [aws_s3_bucket.evidence]
}
run "reject_missing_retention" {
  command = plan
  variables { retention_days = 0 }
  expect_failures = [aws_s3_bucket.evidence]
}
run "reject_shared_reader_writer" {
  command = plan
  variables { reader_role_arn = "arn:aws:iam::123456789012:role/client" }
  expect_failures = [aws_s3_bucket.evidence]
}
