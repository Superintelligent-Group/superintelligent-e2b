terraform {
  required_version = ">= 1.5.0, < 1.6.0"

  backend "s3" {}

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.57"
    }

    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.19"
    }

    random = {
      source  = "hashicorp/random"
      version = "~> 3.5"
    }
  }
}
