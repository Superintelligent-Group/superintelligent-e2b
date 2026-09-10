#!/bin/bash
set -euo pipefail
cd /out
echo 'f4fbb71a581c2f4cd204900ceaf280b71b031c58479250cb0430e0b29774ef5c  /assets/ubuntu-24.04.squashfs' | sha256sum -c -
test ! -e rootfs.ext4
unsquashfs -no-progress -d tree /assets/ubuntu-24.04.squashfs >/out/unsquashfs.log
cat >tree/sup912-init <<'EOF'
#!/bin/sh
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
ip link set lo up
ip addr replace 169.254.0.21/30 dev eth0
ip link set eth0 up
echo SUP912_GUEST_READY >/dev/ttyS0
while :; do sleep 1; done
EOF
chmod 755 tree/sup912-init
truncate -s 512M rootfs.ext4
mkfs.ext4 -q -F -O ^metadata_csum_seed,^orphan_file -d tree rootfs.ext4
sha256sum /assets/ubuntu-24.04.squashfs /assets/vmlinux-5.10.245 tree/sup912-init rootfs.ext4 >derived.sha256
