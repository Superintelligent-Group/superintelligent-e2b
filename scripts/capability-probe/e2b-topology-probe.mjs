#!/usr/bin/env node
/**
 * Read-only E2B topology probe.
 *
 * This verifies that the public Nomad record points at the canonical ingress
 * ALB and that the control/build ASGs share the same VPC. It never changes DNS,
 * capacity, or Nomad state.
 */
import { execFileSync } from 'node:child_process'

const region = process.env.AWS_REGION || process.env.AWS_DEFAULT_REGION || 'us-east-1'
const profile = process.env.AWS_PROFILE || undefined
const hostname = process.env.E2B_NOMAD_HOSTNAME || 'nomad.e2b.superintelligent.group'
const ingressName = process.env.E2B_INGRESS_LB_NAME || 'e2b-ingress'
const controlAsgName = process.env.E2B_CONTROL_ASG || 'e2b-control-server'
const buildAsgName = process.env.E2B_BUILD_ASG || 'e2b-orch-build'
const zoneName = process.env.E2B_HOSTED_ZONE_NAME || 'e2b.superintelligent.group.'

function aws(args) {
  const env = profile ? { ...process.env, AWS_PROFILE: profile } : process.env
  return JSON.parse(execFileSync('aws', [...args, '--region', region, '--output', 'json'], { env, encoding: 'utf8' }))
}
function fail(message) {
  console.error(`topology probe failed: ${message}`)
  process.exitCode = 1
}
function first(value, label) {
  if (!value) fail(`missing ${label}`)
  return value
}

const evidence = {
  region,
  readOnly: true,
  hostname,
  expectedIngress: ingressName,
  controlAsg: controlAsgName,
  buildAsg: buildAsgName,
  checks: [],
}

const ingress = aws(['elbv2', 'describe-load-balancers', '--names', ingressName]).LoadBalancers?.[0]
const control = aws(['autoscaling', 'describe-auto-scaling-groups', '--auto-scaling-group-names', controlAsgName]).AutoScalingGroups?.[0]
const build = aws(['autoscaling', 'describe-auto-scaling-groups', '--auto-scaling-group-names', buildAsgName]).AutoScalingGroups?.[0]
const zone = aws(['route53', 'list-hosted-zones-by-name', '--dns-name', zoneName]).HostedZones?.find((candidate) => candidate.Name === zoneName)

first(ingress, `ingress load balancer ${ingressName}`)
first(control, `control ASG ${controlAsgName}`)
first(build, `build ASG ${buildAsgName}`)
first(zone, `hosted zone ${zoneName}`)

const canonicalVpc = ingress?.VpcId
const controlSubnet = control?.VPCZoneIdentifier?.split(',').filter(Boolean)[0]
const buildSubnet = build?.VPCZoneIdentifier?.split(',').filter(Boolean)[0]
const subnetIds = [controlSubnet, buildSubnet].filter(Boolean)
const subnets = subnetIds.length ? aws(['ec2', 'describe-subnets', '--subnet-ids', ...subnetIds]).Subnets ?? [] : []
const vpcBySubnet = new Map(subnets.map((subnet) => [subnet.SubnetId, subnet.VpcId]))
const records = aws(['route53', 'list-resource-record-sets', '--hosted-zone-id', zone.Id.replace('/hostedzone/', '')]).ResourceRecordSets ?? []
const nomadRecord = records.find((record) => record.Name === `${hostname}.` && record.Type === 'A')
const observedAlias = nomadRecord?.AliasTarget?.DNSName?.replace(/\.$/, '')

evidence.ingress = { arn: ingress?.LoadBalancerArn, dnsName: ingress?.DNSName, vpcId: canonicalVpc }
evidence.control = { vpcId: vpcBySubnet.get(controlSubnet), subnets: control?.VPCZoneIdentifier?.split(',').filter(Boolean) ?? [] }
evidence.build = { vpcId: vpcBySubnet.get(buildSubnet), subnets: build?.VPCZoneIdentifier?.split(',').filter(Boolean) ?? [] }
evidence.dns = { hostedZoneId: zone.Id.replace('/hostedzone/', ''), record: hostname, observedAlias, expectedAlias: ingress?.DNSName }

if (!nomadRecord) fail(`missing public A record for ${hostname}`)
if (observedAlias !== ingress?.DNSName) fail(`${hostname} points to ${observedAlias || '<none>'}, expected ${ingress?.DNSName}`)
if (vpcBySubnet.get(controlSubnet) !== canonicalVpc) fail(`control ASG VPC ${vpcBySubnet.get(controlSubnet) || '<none>'} differs from ingress VPC ${canonicalVpc}`)
if (vpcBySubnet.get(buildSubnet) !== canonicalVpc) fail(`build ASG VPC ${vpcBySubnet.get(buildSubnet) || '<none>'} differs from ingress VPC ${canonicalVpc}`)

if (process.exitCode !== 1) evidence.checks.push('public-nomad-alias-canonical', 'control-ingress-vpc-aligned', 'build-ingress-vpc-aligned')
console.log(JSON.stringify(evidence, null, 2))
