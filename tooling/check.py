#!/usr/bin/env python3
"""Linux local verification: inherited seccomp network denial, minimal environment."""
import ctypes
import ctypes.util
import errno
import os
import pathlib
import shutil
import socket

library = ctypes.util.find_library('seccomp')
if not library:
    raise SystemExit('Local check requires Linux libseccomp and Python 3; network isolation was not established.')
seccomp = ctypes.CDLL(library, use_errno=True)
seccomp.seccomp_init.argtypes = [ctypes.c_uint32]
seccomp.seccomp_init.restype = ctypes.c_void_p
seccomp.seccomp_syscall_resolve_name.argtypes = [ctypes.c_char_p]
seccomp.seccomp_rule_add.argtypes = [ctypes.c_void_p, ctypes.c_uint32, ctypes.c_int, ctypes.c_uint]
seccomp.seccomp_load.argtypes = [ctypes.c_void_p]
seccomp.seccomp_release.argtypes = [ctypes.c_void_p]
context = seccomp.seccomp_init(0x7fff0000)  # allow except explicit network syscalls
if not context:
    raise SystemExit('Cannot initialize network filter')
for name in [b'socket', b'connect', b'bind', b'listen', b'accept', b'accept4', b'sendto', b'sendmsg', b'recvfrom', b'recvmsg', b'io_uring_setup']:
    number = seccomp.seccomp_syscall_resolve_name(name)
    if number >= 0 and seccomp.seccomp_rule_add(context, 0x50000 | errno.EPERM, number, 0):
        raise SystemExit('Cannot configure network filter')
if seccomp.seccomp_load(context):
    raise SystemExit('Cannot enforce network filter')
seccomp.seccomp_release(context)
try:
    socket.socket(socket.AF_INET, socket.SOCK_STREAM)
except PermissionError:
    print('Network socket creation denied by inherited seccomp filter.', flush=True)
else:
    raise SystemExit('Network filter probe failed')
node = shutil.which('node')
if not node:
    raise SystemExit('Install Node.js 22.x')
root = pathlib.Path(__file__).resolve().parent.parent
os.chdir(root)
os.execve(node, [node, '--test', 'test/baseline.test.mjs'], {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'HOME': '/nonexistent', 'LANG': 'C', 'TMPDIR': '/tmp'})
