#!/usr/bin/env node
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, readFileSync, writeFileSync, rmSync, mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'

const cwd = process.cwd()
const run = (file, args, options = {}) => execFileSync(file, args, { cwd, encoding: 'utf8', stdio: options.stdio ?? 'pipe', timeout: options.timeout ?? 120_000 })
const mode = process.argv[2] ?? 'verify'
if (!['verify', 'publish'].includes(mode)) throw new Error('usage: node scripts/local-proof/local-proof.mjs [verify|publish]')
const git = (...args) => run('git', args).trim()
const commit = git('rev-parse', 'HEAD')
const tree = git('rev-parse', 'HEAD^{tree}')
const repo = git('rev-parse', '--show-toplevel')
const proofDir = path.join(repo, '.sig', 'proofs')
mkdirSync(proofDir, { recursive: true })
const receipt = path.join(proofDir, `${commit}.json`)
const signature = `${receipt}.sig`
const policyFiles = [
  '.github/workflows/pr-tests.yml', '.github/workflows/pr-tests-arm64.yml',
  '.github/workflows/integration_tests.yml', 'Makefile', 'iac/provider-aws/Makefile',
  'scripts/aws-account-authority.mk', 'scripts/make-raw-input-authority.mk',
  'scripts/validate-aws-ecr-destination.sh', 'iac/provider-aws/test-deployment-authority.sh',
  'scripts/local-proof/local-proof.mjs',
].filter((file) => existsSync(path.join(repo, file)))
const policyHash = createHash('sha256').update(policyFiles.sort().map((file) => `${file}\0${readFileSync(path.join(repo, file))}`).join('')).digest('hex')
const lanes = []
const runLane = (id, file, args, timeout = 120_000) => {
  try { run(file, args, { timeout }); lanes.push({ id, status: 'success' }) }
  catch (error) { lanes.push({ id, status: 'failure' }); throw new Error(`${id} failed: ${error.stderr || error.message}`) }
}
runLane('diff-check', 'git', ['diff', '--check'])
runLane('authority', process.platform === 'win32' ? 'bash.exe' : 'bash', ['iac/provider-aws/test-deployment-authority.sh'], 120_000)
runLane('terraform-fmt', 'terraform', ['fmt', '-check', '-recursive', 'iac/provider-aws'])
const result = { version: 'e2b.local-proof.v1', commit, tree, policy_hash: policyHash, lanes }
writeFileSync(receipt, `${JSON.stringify(result)}\n`, 'utf8')
const signingKey = process.env.SIG_PROOF_SIGNING_KEY || path.join(process.env.HOME || process.env.USERPROFILE || '', '.ssh', 'id_ed25519')
const allowedSigners = process.env.SIG_PROOF_ALLOWED_SIGNERS || path.join(repo, 'config', 'local-proof-allowed-signers')
if (!existsSync(signingKey)) throw new Error(`local proof signer missing: ${signingKey}`)
if (!existsSync(allowedSigners)) throw new Error(`local proof allowlist missing: ${allowedSigners}`)
run('ssh-keygen', ['-Y', 'sign', '-f', signingKey, '-n', 'sig-local-proof', receipt], { timeout: 30_000 })
if (mode === 'verify') { console.log(`local proof verified: ${commit}`); process.exit(0) }
const origin = git('config', '--get', 'remote.origin.url')
const match = origin.match(/github\.com[:/]([^/]+\/[^/.]+)(?:\.git)?$/)
if (!match) throw new Error('cannot derive GitHub repository from origin')
run('gh', ['api', `repos/${match[1]}/statuses/${commit}`, '-f', 'state=success', '-f', 'context=SIG Local Proof', '-f', 'description=Locally attested E2B proof; hosted Actions are non-authoritative', '-f', `target_url=https://github.com/${match[1]}/commit/${commit}`], { timeout: 30_000 })
console.log(`published SIG Local Proof: ${commit}`)