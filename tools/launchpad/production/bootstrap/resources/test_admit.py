import unittest
from admit import GIB, evaluate


class AdmissionTests(unittest.TestCase):
    def observed(self):
        return {'effective_cpu': 64, 'load_average': [2, 1, 1],
                'memory_available_bytes': 200 * GIB, 'host_memory_total_bytes': 256 * GIB,
                'disk_free_bytes': 600 * GIB, 'disk_total_bytes': 900 * GIB,
                'pids_remaining': 20000, 'nofile_soft': 1000000, 'nproc_soft': 1000000}

    def test_missing_executor_inventory_denies(self):
        result = evaluate(self.observed(), 'logical', 50, 0)
        self.assertFalse(result['admission_pass'])
        self.assertEqual(result['admitted_workers'], 0)

    def test_coordinator_reserved(self):
        result = evaluate(self.observed(), 'logical', 50, 50)
        self.assertEqual(result['admitted_workers'], 49)
        self.assertFalse(result['admission_pass'])

    def test_cpu_contention_reduces_capacity(self):
        observed = self.observed()
        observed['load_average'][0] = 63
        result = evaluate(observed, 'go-build', 50, 51)
        self.assertEqual(result['admitted_workers'], 0)

    def test_memory_and_pids_limit(self):
        observed = self.observed()
        observed['memory_available_bytes'] = 27 * GIB
        observed['pids_remaining'] = 260
        result = evaluate(observed, 'logical', 50, 51)
        self.assertEqual(result['admitted_workers'], 1)

    def test_no_disk_runway_denies(self):
        observed = self.observed()
        observed['disk_free_bytes'] = GIB
        self.assertEqual(evaluate(observed, 'logical', 50, 51)['admitted_workers'], 0)

    def test_build_recommendations_bounded(self):
        result = evaluate(self.observed(), 'go-build', 50, 51)
        self.assertLessEqual(result['limits']['max_concurrent_go_builds'], 8)
        self.assertEqual(result['limits']['go_gomaxprocs'], 2)
        self.assertEqual(result['limits']['max_concurrent_container_starts'], 2)
        self.assertFalse(result['admission_pass'])


if __name__ == '__main__':
    unittest.main()
