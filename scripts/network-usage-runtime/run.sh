#!/bin/bash
set -euo pipefail
# Run in a disposable, network-none Linux container with KVM/TUN and the
# capabilities listed in specs/network-usage-runtime.md. All input mounts are RO.
python3 /scripts/probe-uffd.py > /output/uffd-capability.json
# Satisfy the network package's host-route discovery in this network-none
# container. This dummy link has no peer and provides no external connectivity.
ip link add sup912-dummy type dummy
ip addr add 192.0.2.2/24 dev sup912-dummy
ip link set sup912-dummy up
ip route add default via 192.0.2.1 dev sup912-dummy
test ! -e /output/run
mkdir -p /output/run/kernel /output/run/fc
cp /assets/vmlinux-5.10.245 /output/run/kernel/vmlinux.bin
cp /binary/firecracker /output/run/fc/firecracker
chmod 755 /output/run/fc/firecracker
cp /derived/rootfs.ext4 /output/run/rootfs.ext4
sha256sum /build/fc.test /output/run/fc/firecracker /output/run/kernel/vmlinux.bin /output/run/rootfs.ext4 > /output/inputs.sha256
export SUP912_KVM_ACCEPTANCE=1 SUP912_ACCEPTANCE_DIR=/output/run
timeout --signal=TERM --kill-after=15s 180s /build/fc.test -test.v -test.run "${SUP912_TEST_PATTERN:-^TestMeasurementRuntime}" -test.timeout=165s 2>&1 | tee /output/test.log
