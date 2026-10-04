import os
import pathlib
import pwd
import stat
import subprocess
import sys
import tempfile
import unittest


HELPER = pathlib.Path(__file__).resolve().parents[2] / "scripts/ci/restore-build-ownership.py"


class BuildOwnership(unittest.TestCase):
    def setUp(self):
        if os.geteuid() != 0:
            self.fail("real ownership cases require root; no skipped or simulated privilege")
        self.owner = pwd.getpwnam("nobody")
        self.temporary = tempfile.TemporaryDirectory(prefix="layerx-build-owner-")
        self.addCleanup(self.temporary.cleanup)
        self.base = pathlib.Path(self.temporary.name)
        self.base.chmod(0o755)
        self.repo = self.base / "repo"
        self.repo.mkdir(mode=0o755)
        os.chown(self.repo, self.owner.pw_uid, self.owner.pw_gid)
        self.build = self.repo / "build"
        self.build.mkdir(mode=0o750)
        self.build.chmod(0o2750)
        self.nested = self.build / "nested"
        self.nested.mkdir(mode=0o700)
        self.output = self.nested / "native-object"
        self.output.write_bytes(b"retained real filesystem bytes")
        self.output.chmod(0o640)
        self.owner_arg = f"{self.owner.pw_uid}:{self.owner.pw_gid}"

    def run_helper(self, *arguments):
        return subprocess.run(
            [sys.executable, str(HELPER), "--repo", str(self.repo), "--owner", self.owner_arg, *arguments],
            capture_output=True, text=True, check=False,
        )

    def unprivileged_write(self, path):
        child = os.fork()
        if child == 0:
            try:
                os.setgroups([])
                os.setgid(self.owner.pw_gid)
                os.setuid(self.owner.pw_uid)
                fd = os.open(path, os.O_WRONLY | os.O_APPEND)
                os.write(fd, b" owner-writable")
                os.close(fd)
                os._exit(0)
            except OSError:
                os._exit(1)
        _, status = os.waitpid(child, 0)
        return os.waitstatus_to_exitcode(status)

    def test_real_owner_restoration_preserves_modes_and_enables_runner(self):
        paths = [self.build, self.nested, self.output]
        before = {path: stat.S_IMODE(path.stat().st_mode) for path in paths}
        self.assertEqual(self.unprivileged_write(self.output), 1)
        result = self.run_helper("--output", str(self.build))
        self.assertEqual(result.returncode, 0, result.stderr)
        for path in paths:
            self.assertEqual((path.stat().st_uid, path.stat().st_gid), (self.owner.pw_uid, self.owner.pw_gid))
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), before[path])
        self.assertEqual(self.unprivileged_write(self.output), 0)

    def test_real_internal_build_hardlinks_remain_linked(self):
        linked = self.build / "native-hardlink"
        os.link(self.output, linked)
        result = self.run_helper("--output", str(self.build), "--output", str(self.nested))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(linked.stat().st_ino, self.output.stat().st_ino)
        self.assertEqual(linked.stat().st_uid, self.owner.pw_uid)
        self.assertEqual(self.unprivileged_write(self.output), 0)

    def test_outside_and_repository_root_are_refused_without_changes(self):
        outside = self.base / "outside"
        outside.mkdir()
        for path in [outside, self.repo, self.repo / "build/../../outside"]:
            result = self.run_helper("--output", str(path))
            self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.build.stat().st_uid, 0)
        self.assertEqual(outside.stat().st_uid, 0)

    def test_symlink_output_and_parent_are_refused(self):
        outside = self.base / "outside"
        outside.mkdir()
        (outside / "nested").mkdir()
        link = self.repo / "link"
        link.symlink_to(outside, target_is_directory=True)
        for path in [link, link / "nested"]:
            result = self.run_helper("--output", str(path))
            self.assertNotEqual(result.returncode, 0)
        self.assertEqual(outside.stat().st_uid, 0)

    def test_nested_symlink_and_environment_contents_are_untouched(self):
        outside = self.base / "outside"
        outside.write_bytes(b"outside data")
        (self.build / "outside-link").symlink_to(outside)
        environment = self.build / ".env"
        environment.touch(mode=0o600)
        result = self.run_helper("--output", str(self.build))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outside.stat().st_uid, 0)
        self.assertEqual(environment.stat().st_uid, 0)

    def test_owner_mismatch_and_hardlink_are_refused_before_mutation(self):
        mismatch = subprocess.run(
            [sys.executable, str(HELPER), "--repo", str(self.repo), "--owner", "0:0", "--output", str(self.build)],
            capture_output=True, text=True, check=False,
        )
        self.assertNotEqual(mismatch.returncode, 0)
        outside = self.base / "outside"
        outside.write_bytes(b"linked object")
        os.link(outside, self.build / "hardlink")
        result = self.run_helper("--output", str(self.build))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.build.stat().st_uid, 0)
        self.assertEqual(outside.stat().st_uid, 0)

    def test_validate_and_owner_capture_do_not_mutate(self):
        captured = subprocess.run(
            [sys.executable, str(HELPER), "--repo", str(self.repo), "--capture-owner"],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(captured.returncode, 0, captured.stderr)
        self.assertEqual(captured.stdout.strip(), self.owner_arg)
        result = self.run_helper("--output", str(self.build), "--output", str(self.repo / "missing"), "--validate-only")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.build.stat().st_uid, 0)


if __name__ == "__main__":
    unittest.main()
