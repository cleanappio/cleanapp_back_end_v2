"""Verify the actual advisory lock used by deployment and weekly backup."""
import fcntl
import importlib.util
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest

HELPER = Path(__file__).resolve().parents[1] / "aws_image_deploy.py"
spec = importlib.util.spec_from_file_location("activation_lock_helper", HELPER)
MODULE = importlib.util.module_from_spec(spec)
spec.loader.exec_module(MODULE)


class ActivationLockTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name).resolve()
        self.path = self.directory / ".deployment-and-backup.lock"

    def test_new_lock_is_private_owned_regular_and_inode_is_stable(self):
        with MODULE.deployment_lock(self.directory):
            first = self.path.stat()
            self.assertEqual(stat.S_IMODE(first.st_mode), 0o600)
            self.assertEqual(first.st_uid, os.geteuid())
            self.assertEqual(first.st_nlink, 1)
        with MODULE.deployment_lock(self.directory):
            self.assertEqual(self.path.stat().st_ino, first.st_ino)

    def test_concurrent_process_is_blocked_by_actual_flock(self):
        script = """import fcntl,os,sys
fd=os.open(sys.argv[1],os.O_RDWR)
try:
 fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
except BlockingIOError:
 sys.exit(0)
else:
 sys.exit(2)
"""
        with MODULE.deployment_lock(self.directory):
            result = subprocess.run([sys.executable, "-c", script, str(self.path)], capture_output=True)
            self.assertEqual(result.returncode, 0)

    def test_exception_releases_the_lock(self):
        with self.assertRaisesRegex(RuntimeError, "fixture"):
            with MODULE.deployment_lock(self.directory):
                raise RuntimeError("fixture")
        descriptor = os.open(self.path, os.O_RDWR)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            fcntl.flock(descriptor, fcntl.LOCK_UN)
        finally:
            os.close(descriptor)

    def test_symlink_parent_is_rejected(self):
        alias = self.directory / "alias"
        real = self.directory / "real"
        real.mkdir()
        alias.symlink_to(real, target_is_directory=True)
        with self.assertRaises((OSError, ValueError)):
            with MODULE.deployment_lock(alias):
                self.fail("symlink parent accepted")

    def test_symlink_lock_is_rejected_without_touching_target(self):
        target = self.directory / "other"
        target.write_text("preserve")
        target.chmod(0o600)
        self.path.symlink_to(target)
        with self.assertRaises((OSError, ValueError)):
            with MODULE.deployment_lock(self.directory):
                self.fail("symlink lock accepted")
        self.assertEqual(target.read_text(), "preserve")

    def test_publicly_writable_parent_is_rejected(self):
        self.directory.chmod(0o777)
        try:
            with self.assertRaisesRegex(ValueError, "parent"):
                with MODULE.deployment_lock(self.directory):
                    self.fail("unsafe parent accepted")
        finally:
            self.directory.chmod(0o700)

    def test_unsafe_existing_lock_mode_is_not_silently_changed(self):
        self.path.touch(mode=0o644)
        self.path.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "private"):
            with MODULE.deployment_lock(self.directory):
                self.fail("public lock accepted")
        self.assertEqual(stat.S_IMODE(self.path.stat().st_mode), 0o644)


if __name__ == "__main__":
    unittest.main()
