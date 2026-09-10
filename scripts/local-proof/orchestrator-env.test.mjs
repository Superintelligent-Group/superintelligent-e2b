import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../../', import.meta.url));
const moduleDir = join(root, 'iac/modules/job-orchestrator');
function run(command, args, cwd, input) {
  const result = spawnSync(command, args, { cwd, input, encoding: 'utf8', timeout: 60000,
    env: { ...process.env, TF_IN_AUTOMATION: '1', CHECKPOINT_DISABLE: '1', NOMAD_ADDR: 'http://127.0.0.1:1' } });
  assert.ifError(result.error);
  assert.equal(result.status, 0, `${command}: ${result.stderr}\n${result.stdout}`);
  return result.stdout;
}

test('real Terraform template and supported Nomad parser preserve logical environment strings', () => {
  const dir = mkdtempSync(join(tmpdir(), 'sup917-env-'));
  try {
    const nomad = process.env.NOMAD_BINARY || 'nomad';
    const terraform = process.env.TERRAFORM_BINARY || 'terraform';
    assert.match(run(nomad, ['version'], dir), /Nomad v2\.0\.5\b/);
    // Use the production locals, including filtering and change-detection render.
    // Omit resources so console requires neither provider installation nor a server.
    const source = readFileSync(join(moduleDir, 'main.tf'), 'utf8');
    const locals = source.split('resource "random_id" "orchestrator_job"')[0];
    assert.ok(locals.includes('orchestrator_job_check = templatefile'));
    writeFileSync(join(dir, 'main.tf'), locals);
    writeFileSync(join(dir, 'variables.tf'), readFileSync(join(moduleDir, 'variables.tf')));
    mkdirSync(join(dir, 'jobs'));
    writeFileSync(join(dir, 'jobs/orchestrator.hcl'), readFileSync(join(moduleDir, 'jobs/orchestrator.hcl')));
    const protectedConfig = JSON.parse(readFileSync(join(root,
      'scripts/local-proof/fixtures/orchestrator-protected-env.json'), 'utf8'));
    const values = {
      NETWORK_USAGE_PROTECTED_DELIVERY: JSON.stringify(protectedConfig),
      QUOTES: 'say "hello"', PATH_TEXT: 'C:\\data\\new\\file', MULTILINE: 'first\nsecond\r\nthird\tend',
      UNICODE: 'naïve 日本語 🌲', TEMPLATE_TEXT: '${not_a_variable} %{ if true }x%{ endif }',
      ESCAPED_TEMPLATE: '$${already} %%{already} $$$${many}',
      CALLER_NODE: '${node.unique.name}', PADDED: '  preserved inside  value  ',
      EMPTY: '', BLANK: ' \n\t ', OMIT: null,
    };
    const vars = { node_pool: 'test-only', port: 5008, proxy_port: 5007, memory_mb: 4096,
      environment: 'dev', artifact_source: 'https://example.invalid/orchestrator?etag=test-only',
      orchestrator_checksum: 'test-only-checksum', job_env_vars: values };
    const render = () => {
      writeFileSync(join(dir, 'terraform.tfvars.json'), JSON.stringify(vars));
      return JSON.parse(run(terraform, ['console', '-no-color'], dir,
        'nonsensitive(jsonencode({render = local.orchestrator_job_check, keeper = sha256("${local.orchestrator_job_check}-${var.orchestrator_checksum}")}))\n'));
    };
    const rendered = JSON.parse(render());
    const file = join(dir, 'job.nomad.hcl');
    writeFileSync(file, rendered.render);
    const parsed = JSON.parse(run(nomad, ['job', 'run', '-output', file], dir));
    const job = parsed.Job;
    const task = job.TaskGroups[0].Tasks.find(t => t.Name === 'start');
    for (const [key, value] of Object.entries(values)) {
      if (value === null || value.trim() === '') assert.equal(task.Env[key], undefined, key);
      else assert.equal(task.Env[key], value.trim(), key);
    }
    assert.deepEqual(JSON.parse(task.Env.NETWORK_USAGE_PROTECTED_DELIVERY), protectedConfig);
    assert.equal(task.Env.NODE_ID, '${node.unique.name}');
    assert.equal(task.Env.NODE_IP, '${attr.unique.network.ip-address}');
    assert.equal(task.Env.NODE_LABELS, '${meta.node_labels}');
    assert.equal(job.TaskGroups[0].Constraints[0].LTarget, '${meta.node_type}');
    assert.equal(task.Artifacts[0].GetterSource, vars.artifact_source);
    const same = JSON.parse(render());
    assert.equal(same.keeper, rendered.keeper);
    vars.orchestrator_checksum = 'changed-artifact';
    assert.notEqual(JSON.parse(render()).keeper, rendered.keeper);
    vars.orchestrator_checksum = 'test-only-checksum';
    vars.job_env_vars.QUOTES = 'changed logical value';
    assert.notEqual(JSON.parse(render()).keeper, rendered.keeper);
  } finally {
    assert.equal(dirname(resolve(dir)), resolve(tmpdir()), 'cleanup must remain inside temporary root');
    assert.ok(basename(resolve(dir)).startsWith('sup917-env-'), 'cleanup must target this test directory');
    rmSync(dir, { recursive: true, force: true });
  }
});
