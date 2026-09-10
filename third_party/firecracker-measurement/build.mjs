#!/usr/bin/env node
// Build a reviewed additive patch against exact upstream source. No upload or deployment.
import { createHash } from 'node:crypto'
import { closeSync, copyFileSync, mkdirSync, openSync, readFileSync, writeFileSync } from 'node:fs'
import { execFileSync, spawnSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const builderBytes = readFileSync(fileURLToPath(import.meta.url))
const [upstream, destination, ...extra] = process.argv.slice(2)
if (!upstream || !destination || extra.length) throw new Error('usage: node third_party/firecracker-measurement/build.mjs <upstream-git-repo> <new-output-directory>')
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const manifestBytes = readFileSync(path.join(here, 'source.json'))
const manifest = JSON.parse(manifestBytes)
const patch = readFileSync(path.join(here, 'measurement.patch'))
if (sha256(patch) !== manifest.patch_sha256) throw new Error('reviewed patch hash mismatch')
const git = (...args) => execFileSync('git', ['-C', path.resolve(upstream), ...args], { maxBuffer: 64 * 1024 * 1024 })
if (git('rev-parse', `${manifest.upstream_commit}^{tree}`).toString().trim() !== manifest.upstream_tree) throw new Error('upstream source tree mismatch')
if (sha256(git('show', `${manifest.upstream_commit}:Cargo.lock`)) !== manifest.cargo_lock_sha256) throw new Error('upstream dependency lock mismatch')
if (!/^public\.ecr\.aws\/firecracker\/fcuvm@sha256:[a-f0-9]{64}$/.test(manifest.builder_image)) throw new Error('builder must be digest pinned')
if (manifest.target !== 'x86_64-unknown-linux-musl') throw new Error('unsupported build target')
const output = path.resolve(destination)
// Refuse reuse: an old success receipt must never survive a failed rerun.
mkdirSync(output)
const input = path.join(output, 'input')
mkdirSync(input)
const artifacts = path.join(output, 'artifacts')
mkdirSync(artifacts)
writeFileSync(path.join(input, 'source.json'), manifestBytes)
writeFileSync(path.join(input, 'measurement.patch'), patch)
copyFileSync(path.join(here, 'build.sh'), path.join(input, 'build.sh'))
const archive = path.join(input, 'upstream.tar')
const archiveFd = openSync(archive, 'wx')
try { execFileSync('git', ['-C', path.resolve(upstream), '-c', 'core.autocrlf=false', 'archive', '--format=tar', manifest.upstream_commit], { stdio: ['ignore', archiveFd, 'inherit'] }) }
finally { closeSync(archiveFd) }
const log = openSync(path.join(output, 'build.log'), 'wx')
let result
try {
  result = spawnSync('docker', ['run', '--rm', '--entrypoint', 'bash',
    '--device', '/dev/kvm', '--device', '/dev/net/tun', '--cap-add', 'NET_ADMIN',
    '--mount', `type=bind,source=${input},target=/input,readonly`,
    '--mount', `type=bind,source=${artifacts},target=/output`,
    manifest.builder_image, '/input/build.sh'], { stdio: ['ignore', log, log] })
} finally { closeSync(log) }
if (result.error || result.status !== 0) throw new Error(`build failed; inspect ${path.join(output, 'build.log')}`)
const binary = path.join(artifacts, 'firecracker')
const evidence = {
  ...manifest,
  schema: 'sig.firecracker-measurement-build.v1',
  source_schema: manifest.schema,
  completed_at: new Date().toISOString(),
  builder_script_sha256: sha256(builderBytes),
  manifest_sha256: sha256(manifestBytes),
  upstream_archive_sha256: sha256(readFileSync(archive)),
  build_script_sha256: sha256(readFileSync(path.join(input, 'build.sh'))),
  binary_sha256: sha256(readFileSync(binary)),
  build_log_sha256: sha256(readFileSync(path.join(output, 'build.log'))),
  validation: JSON.parse(readFileSync(path.join(artifacts, 'validation.json'))),
  live_vm_acceptance: false,
  deployed: false,
}
writeFileSync(path.join(output, 'receipt.json'), `${JSON.stringify(evidence, null, 2)}\n`)
console.log(JSON.stringify(evidence, null, 2))
