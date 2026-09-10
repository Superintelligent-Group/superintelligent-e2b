#!/usr/bin/env bash
set -euo pipefail

# Fresh ephemeral source/build tree. Inputs are mounted read-only; no host
# credentials, Docker socket or provider destination is passed in. KVM and TUN
# are explicit local test devices; networking remains in the container namespace.
mkdir /work
cd /work
export CARGO_TARGET_DIR=/work/target
tar -xf /input/upstream.tar
git apply --check --whitespace=error-all /input/measurement.patch
git apply --whitespace=error-all /input/measurement.patch
test "$(rustc --version | cut -d ' ' -f 2)" = 1.89.0
python3 - <<'PY'
import json, subprocess
files = json.load(open('/input/source.json'))['changed_rust_files']
# Skip child traversal: unrelated upstream snapshot.rs does not pass this pinned
# rustfmt. Every directly changed Rust file is checked explicitly.
subprocess.run(['rustfmt', '--check', '--edition', '2024', '--config', 'skip_children=true', *files], check=True)
PY
cargo test --locked -p vmm --lib measurement -- --test-threads=1 | tee /output/writer-tests.log
cargo test --locked -p vmm --lib logger::metrics -- --test-threads=1 | tee /output/legacy-metrics-tests.log
cargo test --locked -p firecracker --lib api_server::request::actions -- --test-threads=1 | tee /output/action-tests.log
cargo test --locked -p firecracker --lib api_server::request::metrics -- --test-threads=1 | tee /output/metrics-api-tests.log
cargo test --locked -p firecracker --lib api_server::parsed_request -- --test-threads=1 | tee /output/response-tests.log
cargo test --locked -p firecracker --bin firecracker terminal -- --test-threads=1 | tee /output/terminal-adapter-tests.log
cargo test --locked -p vmm --lib kvm_terminal_measurement -- --ignored --test-threads=1 | tee /output/kvm-terminal-tests.log
cargo test --locked -p vmm --lib kvm_runtime_finalize_measurement_action_isolated -- --ignored --test-threads=1 | tee /output/kvm-action-tests.log
cargo build --locked --release --target x86_64-unknown-linux-musl -p firecracker --bin firecracker
cp target/x86_64-unknown-linux-musl/release/firecracker /output/firecracker
/output/firecracker --version
python3 - <<'PY'
import json, pathlib, re
lanes = {}
ignored = {}
for name in ('writer-tests', 'legacy-metrics-tests', 'action-tests', 'metrics-api-tests', 'response-tests', 'terminal-adapter-tests', 'kvm-terminal-tests', 'kvm-action-tests'):
    text = pathlib.Path('/output', name + '.log').read_text()
    counts = re.findall(r'test result: ok\. (\d+) passed; 0 failed; (\d+) ignored;', text)
    if len(counts) != 1 or int(counts[0][0]) == 0:
        raise SystemExit('missing nonempty successful lane: ' + name)
    lanes[name] = int(counts[0][0])
    ignored[name] = int(counts[0][1])
if ignored['kvm-terminal-tests'] or lanes['kvm-terminal-tests'] < 3:
    raise SystemExit('actual KVM/TUN terminal regressions did not run')
if ignored['kvm-action-tests'] or lanes['kvm-action-tests'] != 1:
    raise SystemExit('isolated runtime action regression did not run')
pathlib.Path('/output/validation.json').write_text(json.dumps({
    'rust': '1.89.0', 'target': 'x86_64-unknown-linux-musl',
    'focused_tests': lanes, 'ignored_by_lane': ignored,
    'format': 'passed', 'release_build': 'passed',
    'kvm_tests': 'terminal VMM/TUN unit regressions passed; not a booted guest acceptance run',
    'complete_measurement_window': False,
}, indent=2) + '\n')
PY
