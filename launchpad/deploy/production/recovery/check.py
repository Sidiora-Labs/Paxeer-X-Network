"""Fresh native unittest results, no reuse of prior evidence."""
import json
import pathlib
import sys
import time
import unittest
root=pathlib.Path(__file__).resolve().parent
sys.path.insert(0,str(root))
started=time.time()
suite=unittest.TestSuite([unittest.defaultTestLoader.discover(str(root),pattern="test_*.py")])
result=unittest.TextTestRunner(verbosity=2).run(suite)
summary={"version":1,"tests":result.testsRun,"failures":len(result.failures),"errors":len(result.errors),"skipped":len(result.skipped),"started_unix":started,"ended_unix":time.time(),"proof":"local-contract-and-real-postgresql-logical-snapshot","physical_pitr_qualified":False,"cross_region_qualified":False}
print(json.dumps(summary,sort_keys=True))
sys.exit(0 if result.wasSuccessful() and result.testsRun>=15 and not result.skipped else 1)
