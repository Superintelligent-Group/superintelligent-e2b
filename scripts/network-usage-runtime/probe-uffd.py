"""Read-only UFFD capability probe for the pinned x86_64 acceptance fixture."""
import ctypes
import fcntl
import json
import os
import platform
import struct

if platform.machine() != "x86_64":
    raise SystemExit("this probe uses the x86_64 userfaultfd syscall number")

libc = ctypes.CDLL(None, use_errno=True)
results = []
# Pinned producer persist/mod.rs requires EVENT_REMOVE, MISSING_HUGETLBFS,
# and WP_ASYNC. Probe the complete set, not merely syscall availability.
for features in (0, (1 << 3) | (1 << 4) | (1 << 15)):
    fd = libc.syscall(323, os.O_CLOEXEC | os.O_NONBLOCK)
    result = {"requested_features": hex(features), "syscall_succeeded": fd >= 0}
    if fd < 0:
        result["syscall_errno"] = ctypes.get_errno()
    else:
        data = bytearray(struct.pack("QQQ", 0xAA, features, 0))
        try:
            fcntl.ioctl(fd, 0xC018AA3F, data, True)
            api, supported, ioctls = struct.unpack("QQQ", data)
            result.update(api=hex(api), supported_features=hex(supported),
                          wp_async=bool(supported & (1 << 15)), accepted=True)
        except OSError as error:
            result.update(ioctl_errno=error.errno, accepted=False)
        finally:
            os.close(fd)
    results.append(result)
print(json.dumps({"kernel": os.uname().release, "probes": results}, indent=2))
