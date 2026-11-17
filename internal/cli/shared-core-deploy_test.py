"""Synthetic archive tests; never installs or executes a host runtime."""

import hashlib
import io
import os
import pathlib
import runpy
import struct
import tarfile
import tempfile
import unittest

DEPLOY = runpy.run_path(str(pathlib.Path(__file__).with_name("shared-core-deploy.py")))


def executable(machine):
    header = bytearray(128)
    header[:7] = b"\x7fELF\x02\x01\x01"
    struct.pack_into("<HHI", header, 16, 2, machine, 1)
    return bytes(header)


def archive(entries):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w:xz") as package:
        for name, data, kind in entries:
            entry = tarfile.TarInfo(name)
            entry.size = len(data)
            entry.type = kind
            package.addfile(entry, io.BytesIO(data))
    return output.getvalue()


class NodeExtraction(unittest.TestCase):
    @unittest.skipUnless(os.geteuid() == 0, "root-owned executable verification")
    def test_xz_selection_and_executable_revalidation(self):
        for arch, machine in (("x64", 62), ("arm64", 183)):
            name = "node-v1.2.3-linux-" + arch
            data = executable(machine)
            digest = hashlib.sha256(data).hexdigest()
            packed = archive([(name + "/bin/node", data, tarfile.REGTYPE)])
            with tempfile.TemporaryDirectory() as directory:
                stage = pathlib.Path(directory)
                DEPLOY["extract_node"](packed, stage, name, digest, machine)
                node = stage / "bin/node"
                self.assertEqual(node.read_bytes(), data)
                DEPLOY["verify_node"](node, digest, machine)
                with self.assertRaises(ValueError):
                    DEPLOY["verify_node"](node, digest, 1)
                node.write_bytes(data + b"changed")
                with self.assertRaises(ValueError):
                    DEPLOY["verify_node"](node, digest, machine)

    def test_wrong_member_duplicate_link_digest_and_elf(self):
        name = "node-v1.2.3-linux-arm64"
        data = executable(183)
        digest = hashlib.sha256(data).hexdigest()
        member = (name + "/bin/node", data, tarfile.REGTYPE)
        for entries, expected, machine in (
            ([("node-v1.2.3-linux-x64/bin/node", data, tarfile.REGTYPE)], digest, 183),
            ([member, member], digest, 183),
            ([(name + "/bin/node", data, tarfile.SYMTYPE)], digest, 183),
            ([member], "f" * 64, 183),
            ([member], digest, 62),
        ):
            with tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(ValueError):
                    DEPLOY["extract_node"](archive(entries), pathlib.Path(directory),
                                           name, expected, machine)

    def test_invalid_archive_and_identity_stop_before_host_effects(self):
        install = DEPLOY["install_runtime"]
        for name, arch in (("../escape", "arm64"),
                           ("node-v1.2.3-linux-x64", "arm64"),
                           ("node-v1.2.3-linux-arm64", "unknown")):
            with self.assertRaises(ValueError):
                install(b"bad", "a" * 64, "b" * 64, name, arch)
        with self.assertRaisesRegex(ValueError, "archive digest"):
            install(b"bad", "a" * 64, "b" * 64,
                    "node-v1.2.3-linux-arm64", "arm64")


if __name__ == "__main__":
    unittest.main()
