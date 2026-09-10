"""Test-only owned TAP setup; invoked by ip netns exec before first link-up."""
import errno
import json
import os
import pathlib
import subprocess
import sys
import tempfile


def disable_owned_tap(namespace, interface, parent_net, parent_mount, report_path):
    report = {"passed": False, "namespace": namespace, "interface": interface}
    mountpoint = None
    mounted = False

    def command(*args):
        return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT, timeout=5)

    def globals_now():
        return {name: pathlib.Path('/proc/sys/net/ipv6/conf', name, 'disable_ipv6').read_text().strip()
                for name in ('all', 'default', 'lo')}

    try:
        assert namespace == 'ns-912' and interface == 'tap0', 'unexpected fixture target'
        report['net'] = os.readlink('/proc/self/ns/net')
        report['mount'] = os.readlink('/proc/self/ns/mnt')
        report['named_inode'] = os.stat('/run/netns/' + namespace).st_ino
        assert report['net'] == 'net:[%d]' % report['named_inode'], 'wrong network namespace'
        assert report['net'] != parent_net, 'parent network namespace'
        assert report['mount'] != parent_mount, 'parent mount namespace'
        link = json.loads(command('ip', '-d', '-j', 'link', 'show', 'dev', interface))[0]
        report['before_link'] = link
        assert link['ifname'] == interface and 'UP' not in link['flags'], 'TAP must still be down'
        assert link['linkinfo']['info_kind'] == 'tun' and link['linkinfo']['info_data']['type'] == 'tap'
        report['globals_before'] = globals_now()
        target = pathlib.Path('/proc/sys/net/ipv6/conf', interface, 'disable_ipv6')
        report['before'] = target.read_text().strip()
        assert report['before'] in ('0', '1')
        try:
            target.write_text('1\n')
            report['ordinary_write'] = True
        except OSError as error:
            report['ordinary_errno'] = error.errno
            if error.errno != errno.EROFS:
                raise
            # ip netns exec gave this process a separate mount namespace. Never
            # remount /proc or write all/default; mount fresh proc only here.
            mountpoint = pathlib.Path(tempfile.mkdtemp(prefix='sup921-owned-proc-'))
            command('mount', '-t', 'proc', '-o', 'nosuid,nodev,noexec', 'proc', str(mountpoint))
            mounted = True
            target = mountpoint / 'sys/net/ipv6/conf' / interface / 'disable_ipv6'
            assert target.read_text().strip() == report['before']
            target.write_text('1\n')
            report['fresh_proc'] = str(mountpoint)
        report['readback'] = target.read_text().strip()
        assert report['readback'] == '1', 'IPv6 disable readback failed'
        report['globals_after'] = globals_now()
        assert report['globals_after'] == report['globals_before']
        assert 'UP' not in json.loads(command('ip', '-j', 'link', 'show', 'dev', interface))[0]['flags']
        report['passed'] = True
    finally:
        try:
            if mounted:
                command('umount', str(mountpoint))
                mounted = False
            if mountpoint is not None:
                mountpoint.rmdir()
            report['mount_cleanup_complete'] = True
        finally:
            report['passed'] = report['passed'] and report.get('mount_cleanup_complete', False)
            pathlib.Path(report_path).write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    disable_owned_tap(*sys.argv[1:])
