provider "aws" {
  region = var.aws_region
}

provider "cloudflare" {
  alias     = "managed"
  api_token = var.cloudflare_api_token
}
