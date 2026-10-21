import os
from pathlib import Path
import tempfile
import unittest

from evidence_policy import validate_phase4
from replay_restore import Phase4Recovery, RecoveryError, load_configuration4, recovery_timestamps, persistence


class RecoveryBoundaries(unittest.TestCase):
    def test_missing_raw_execution_cannot_qualify(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                validate_phase4(directory,{}, {},1)

    def test_isolated_result_cannot_promote_global_gate(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                validate_phase4(directory,{'task':'4.49','scope':'final_shared','capacity_qualified':True}, {},1)

    def test_recovery_math_preserves_actual_raw_timestamp_order(self):
        result=recovery_timestamps(loss_utc_ns=1_000_000_000_000,loss_monotonic_ns=100_000_000_000,verified_utc_ns=1_002_000_000_000,verified_monotonic_ns=102_000_000_000,last_acknowledged_utc_ns=999_000_000_000,last_restored_utc_ns=998_000_000_000)
        self.assertEqual(result['rpo_ns'],'2000000000')
        self.assertEqual(result['rto_ns'],'2000000000')
        self.assertFalse(result['final_integrated_recovery_qualified'])
        self.assertEqual(result['final_gate'],'9.1')

    def test_clock_jump_or_missing_commit_time_cannot_be_recovery_success(self):
        with self.assertRaises(RecoveryError):
            recovery_timestamps(loss_utc_ns=1_000_000_000_000,loss_monotonic_ns=100_000_000_000,verified_utc_ns=1_003_000_000_000,verified_monotonic_ns=102_000_000_000,last_acknowledged_utc_ns=999_000_000_000,last_restored_utc_ns=998_000_000_000)
        with self.assertRaises(RecoveryError):
            recovery_timestamps(loss_utc_ns=1_000_000_000_000,loss_monotonic_ns=100_000_000_000,verified_utc_ns=1_002_000_000_000,verified_monotonic_ns=102_000_000_000,last_acknowledged_utc_ns=999_000_000_000,last_restored_utc_ns=0)

    def test_recovery_class_inventory_cannot_replace_profiles_chat_or_rewards_with_chain(self):
        with self.assertRaises(persistence.PersistenceError):
            persistence.recovery_snapshot_sql([{'class':'business','name':'raw','schema':'indexer','table':'indexer_raw_logs','keys':['chain_id','transaction_hash','log_index']}],commit_timestamps=True)

    def test_exact_row_checksums_detect_loss_and_reject_missing_commit_authority(self):
        tables=[{'class':group,'name':group,'schema':'owned','table':group,'keys':['id']} for group in sorted(persistence.RECOVERY_CLASSES)]
        expected={group:[{'identity':['durable-'+group],'raw_value':'{"id":"durable-'+group+'","amount_raw":"123456789012345678901234567890"}','committed_utc_ns':'1000000000000'}] for group in persistence.RECOVERY_CLASSES}
        actual={group:[{key:value for key,value in row.items() if key!='committed_utc_ns'} for row in rows] for group,rows in expected.items()}
        result=persistence.assert_restored_checkpoint(expected,actual,tables)
        self.assertEqual(result['acknowledged_rows_lost'],0)
        changed={**actual,'reward_ledger':[{'identity':['durable-reward_ledger'],'raw_value':'{"id":"durable-reward_ledger","amount_raw":"0"}'}]}
        with self.assertRaises(persistence.PersistenceError):
            persistence.assert_restored_checkpoint(expected,changed,tables)
        missing={group:[{key:value for key,value in row.items() if key!='committed_utc_ns'} for row in rows] for group,rows in expected.items()}
        with self.assertRaises(persistence.PersistenceError):
            persistence.assert_restored_checkpoint(missing,actual,tables)


class ActualPhase4Recovery(unittest.TestCase):
    def test_actual_candidate_replay_pitr_and_regressions(self):
        path=os.environ.get('PHASE4_CONFIG_FILE')
        digest=os.environ.get('PHASE4_CONFIG_SHA256')
        source=os.environ.get('QUALIFICATION_CANDIDATE_SHA256')
        output=os.environ.get('PHASE4_OUTPUT_DIR')
        self.assertTrue(path and digest and source and output,'actual qualified3.99/runtime/PITR/broker/generator authority required; no provider skip or substitution')
        configuration=load_configuration4(Path(path),digest,source)
        result=Phase4Recovery(configuration,path,Path(output)).run()
        self.assertTrue(result['phase4_isolated_proof'])
        self.assertFalse(result['actual_elapsed72h_proven'])
        self.assertFalse(result['final_integrated_gate_passed'])
        self.assertFalse(result['capacity_qualified'])
        self.assertFalse(result['disaster_recovery_qualified'])
        self.assertFalse(result['production_qualified'])


if __name__=='__main__':
    unittest.main(verbosity=2)
