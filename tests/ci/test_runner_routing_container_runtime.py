import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
CHECK = ROOT / "scripts/ci/runner-routing-check.sh"
ROUTED = (
    "${{ (github.event_name == 'pull_request' && "
    "github.event.pull_request.head.repo.full_name != github.repository) && "
    "'ubuntu-24.04' || vars.CI_LINUX_RUNNER || 'ubuntu-24.04' }}"
)


class RunnerRoutingContainerRuntime(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="runner-routing-")
        self.addCleanup(self.temporary.cleanup)
        self.repo = pathlib.Path(self.temporary.name)
        (self.repo / "scripts/ci").mkdir(parents=True)
        shutil.copy2(CHECK, self.repo / "scripts/ci/runner-routing-check.sh")
        shutil.copytree(ROOT / ".github/workflows", self.repo / ".github/workflows")

    def run_check(self):
        return subprocess.run(
            [str(self.repo / "scripts/ci/runner-routing-check.sh")],
            capture_output=True,
            text=True,
            check=False,
        )

    def write_workflow(self, name, jobs):
        (self.repo / ".github/workflows" / name).write_text(
            "name: probe\non: pull_request\njobs:\n" + jobs, encoding="utf-8"
        )

    def test_repository_workflows_pass(self):
        result = self.run_check()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")

    def test_routed_matrix_leg_reaching_the_mix_builder_is_refused(self):
        self.write_workflow(
            "probe.yml",
            "  lint:\n"
            f"    runs-on: {ROUTED}\n"
            "    strategy:\n"
            "      matrix:\n"
            "        leg: [backend, frontend]\n"
            "    steps:\n"
            "      - name: Lint\n"
            "        env:\n"
            "          LEG: ${{ matrix.leg }}\n"
            "        run: |\n"
            '          "scripts/explorer/lint-$LEG.sh"\n',
        )
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            ".github/workflows/probe.yml: job lint: routed job needs a container runtime the routed runner "
            "image lacks (step 'Lint', matrix {'leg': 'backend'}); keep it on a hosted runner and list it "
            "as docker-command\n",
        )

    def test_routed_job_calling_docker_is_refused(self):
        self.write_workflow(
            "probe.yml",
            "  images:\n"
            f"    runs-on: {ROUTED}\n"
            "    steps:\n"
            "      - name: Build\n"
            "        run: docker build .\n",
        )
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            ".github/workflows/probe.yml: job images: routed job needs a container runtime the routed runner "
            "image lacks (step 'Build', matrix {}); keep it on a hosted runner and list it as docker-command\n",
        )

    def test_routed_matrix_without_a_container_leg_passes(self):
        self.write_workflow(
            "probe.yml",
            "  lint:\n"
            f"    runs-on: {ROUTED}\n"
            "    strategy:\n"
            "      matrix:\n"
            "        leg: [frontend, services]\n"
            "    steps:\n"
            "      - name: Lint\n"
            "        env:\n"
            "          LEG: ${{ matrix.leg }}\n"
            '        run: "scripts/explorer/lint-$LEG.sh"\n',
        )
        result = self.run_check()
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
