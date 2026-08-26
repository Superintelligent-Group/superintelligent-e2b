#!/usr/bin/env node
/**
 * Read-only E2B nested-virtualization capacity probe.
 *
 * This intentionally does not scale an ASG or launch an instance. It verifies
 * the declarative control-plane evidence needed before the first live smoke:
 * supported non-metal family, ASG pointer alignment, and zero capacity.
 */
import { execFileSync } from 'node:child_process'

const region = process.env.AWS_REGION || process.env.AWS_DEFAULT_REGION || 'us-east-1'
const profile = process.env.AWS_PROFILE || undefined
const allowed = new Set(['c7i', 'm7i', 'r7i', 'c7i-flex', 'm7i-flex', 'c8i', 'm8i', 'r8i', 'c8i-flex', 'm8i-flex', 'r8i-flex', 'c8id', 'm8id', 'r8id', 'x8i', 'i7i'])
const pools = [
  { asg: process.env.E2B_BUILD_ASG || 'e2b-orch-build' },
  { asg: process.env.E2B_CLIENT_ASG || 'e2b-orch-client' },
]

function aws(args) {
  const env = profile ? { ...process.env, AWS_PROFILE: profile } : process.env
  return JSON.parse(execFileSync('aws', [...args, '--region', region, '--output', 'json'], { env, encoding: 'utf8' }))
}
function fail(message) {
  console.error(`capability probe failed: ${message}`)
  process.exitCode = 1
}
function family(type) {
  return type.split('.')[0]
}

const evidence = { region, readOnly: true, pools: [], checks: [] }
for (const pool of pools) {
  const group = aws(['autoscaling', 'describe-auto-scaling-groups', '--auto-scaling-group-names', pool.asg]).AutoScalingGroups?.[0]
  const launchTemplateId = group?.LaunchTemplate?.LaunchTemplateId
  const latest = launchTemplateId
    ? aws(['ec2', 'describe-launch-template-versions', '--launch-template-id', launchTemplateId, '--versions', '$Latest']).LaunchTemplateVersions?.[0]
    : undefined
  if (!group || !launchTemplateId || !latest) {
    fail(`missing live ASG or launch template for ${pool.asg}`)
    continue
  }
  const type = latest.LaunchTemplateData?.InstanceType
  const version = String(latest.VersionNumber)
  const pointer = String(group.LaunchTemplate?.Version || '')
  const record = {
    asg: pool.asg,
    launchTemplateId,
    latestVersion: version,
    asgVersion: pointer,
    instanceType: type,
    nestedVirtualizationState: 'enabled-in-terraform',
    desired: group.DesiredCapacity,
    minimum: group.MinSize,
    maximum: group.MaxSize,
    runningInstances: (group.Instances || []).length,
  }
  evidence.pools.push(record)
  if (!type || !allowed.has(family(type))) fail(`${pool.asg}: unsupported or missing instance family ${type || '<none>'}`)
  if (pointer !== version) fail(`${pool.asg}: ASG points at ${pointer || '<none>'}, latest template is ${version}`)
  if (group.DesiredCapacity !== 0 || group.MinSize !== 0 || (group.Instances || []).length !== 0) fail(`${pool.asg}: probe requires zero capacity`)
}
if (evidence.pools.length === pools.length && process.exitCode !== 1) {
  evidence.checks.push('supported-non-metal-family', 'asg-points-at-latest-template', 'zero-capacity-invariant')
}
console.log(JSON.stringify(evidence, null, 2))
