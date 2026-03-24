# =============================================================================
# Route 53 DNS + ACM Certificate
# =============================================================================
# Replaces upstream's Cloudflare-based domain.tf for our Route 53 setup.
# Creates: wildcard ACM cert + DNS validation + CNAME to ALB
# =============================================================================

locals {
  domain_parts        = split(".", var.domain_name)
  domain_is_subdomain = length(local.domain_parts) > 2
  domain_root         = local.domain_is_subdomain ? join(".", slice(local.domain_parts, length(local.domain_parts) - 2, length(local.domain_parts))) : var.domain_name
}

data "aws_route53_zone" "domain" {
  name = local.domain_root
}

# Named "wildcard" to match the reference in alb.tf (certificate_arn)
resource "aws_acm_certificate" "wildcard" {
  domain_name               = "*.${var.domain_name}"
  subject_alternative_names = [var.domain_name]
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.wildcard.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      type   = dvo.resource_record_type
      record = dvo.resource_record_value
    }
  }

  zone_id = data.aws_route53_zone.domain.zone_id
  name    = each.value.name
  type    = each.value.type
  records = [each.value.record]
  ttl     = 300
}

resource "aws_acm_certificate_validation" "wildcard" {
  certificate_arn         = aws_acm_certificate.wildcard.arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

# Wildcard CNAME: *.e2b.superintelligent.group → ALB
resource "aws_route53_record" "e2b_wildcard" {
  zone_id = data.aws_route53_zone.domain.zone_id
  name    = "*.${var.domain_name}"
  type    = "CNAME"
  ttl     = 300
  records = [aws_lb.ingress.dns_name]
}

# Base domain: e2b.superintelligent.group → ALB
resource "aws_route53_record" "e2b_base" {
  zone_id = data.aws_route53_zone.domain.zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_lb.ingress.dns_name
    zone_id                = aws_lb.ingress.zone_id
    evaluate_target_health = true
  }
}
