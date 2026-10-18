import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class NativeSafetyVoiceWidgets(unittest.TestCase):
    def test_real_dom_widget_interactions(self):
        web = Path(__file__).resolve().parents[3]
        with tempfile.TemporaryDirectory(prefix="kindle-L38-dom-") as directory:
            directory = Path(directory)
            report = directory / "vitest.json"
            config = directory / "vitest.config.mts"
            config.write_text(
                "import original from " + json.dumps(str(web / "vitest.config.ts")) + ";\n"
                "import {mergeConfig} from " + json.dumps(str(web / "node_modules/vitest/dist/config.js")) + ";\n"
                "export default mergeConfig(original,{cacheDir:" + json.dumps(str(directory / "cache")) +
                ",test:{cache:false,fileParallelism:false,maxWorkers:1,minWorkers:1}});\n"
            )
            result = subprocess.run(
                [str(web / "node_modules/.bin/vitest"), "run",
                 "src/widgets/community-safety/widgets.test.tsx", "--configLoader", "runner",
                 "--config", str(config), "--reporter=json", "--outputFile=" + str(report)],
                cwd=web, capture_output=True, text=True, timeout=300,
                env=dict(os.environ, NEXT_TELEMETRY_DISABLED="1"),
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertTrue(report.is_file(), "Native Vitest report missing")
            data = json.loads(report.read_text())
            self.assertIs(data.get("success"), True)
            count = data.get("numTotalTests")
            self.assertIs(type(count), int)
            self.assertEqual(count, 9)
            self.assertEqual(data.get("numPassedTests"), count)
            for key in ("numFailedTests", "numPendingTests", "numTodoTests"):
                self.assertEqual(data.get(key, 0), 0, key)
            suites = data.get("testResults")
            self.assertIsInstance(suites, list)
            self.assertGreater(len(suites), 0)
            assertions = []
            for suite in suites:
                self.assertEqual(suite.get("status"), "passed")
                assertions.extend(suite.get("assertionResults", []))
            self.assertEqual(len(assertions), count)
            for assertion in assertions:
                self.assertEqual(assertion.get("status"), "passed")
                self.assertTrue(assertion.get("fullName"))
            print(json.dumps({"native_vitest_passes": count,
                              "native_test_names": [item["fullName"] for item in assertions]}))


if __name__ == "__main__":
    unittest.main(verbosity=2)
