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
  # Cost attribution is an explicit taxonomy, independent of resource names.
  # These tags also survive targeted recovery plans without provider defaults.
  resource_tags = {
    Project = var.project_tag
  }
}
