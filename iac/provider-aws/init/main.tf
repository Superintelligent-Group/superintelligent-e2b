data "aws_region" "current" {}

data "aws_elb_service_account" "current" {}

module "network" {
  source = "../modules/network"

  prefix                          = var.prefix
  vpc_availability_zones          = ["${var.region}a", "${var.region}b", "${var.region}c"]
  vpc_endpoint_ingress_subnet_ids = var.endpoint_ingress_subnet_ids
}

module "cloudflare" {
  source = "../modules/cloudflare"

  prefix = var.prefix
}
locals {
  # Keep provider-side cost attribution on every durable E2B artifact. The
  # prefix is the environment's canonical namespace (for example `e2b-`),
  # so this remains parameterized instead of baking the dev project name into
  # each resource.
  resource_tags = {
    Project = trimsuffix(var.prefix, "-")
  }
}
