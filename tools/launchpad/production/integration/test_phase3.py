import os
from pathlib import Path
import tempfile
import unittest

from adapters import BoundaryError, RecoveryPermit, RecoveryFaults
from evidence_policy import validate_phase3
from journeys import Phase3Journey, load_phase3_configuration, persistence


class Phase3Boundaries(unittest.TestCase):
    def test_absent_runtime_never_qualifies(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                validate_phase3(directory, {}, {}, 0)

    def test_mathematical_result_cannot_be_fault_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                validate_phase3(directory, {'task':'3.49', 'phase3_qualified':True, 'evidence_level':'synthetic_mathematical'}, {}, 0)

    def test_recovery_targets_require_current_exact_lease(self):
        with self.assertRaises(BoundaryError):
            RecoveryPermit('postgres-primary', 'kd-'+'a'*24, '12345678-1234-1234-1234-123456789abc', 'postgres', 'az-a', (), ('stop','start'))
        with self.assertRaises(BoundaryError):
            RecoveryPermit('a'*64, 'kd-'+'a'*24, '12345678-1234-1234-1234-123456789abc', 'postgres', 'az-a', ('shared-data',), ('stop','start'))

    def test_named_volume_authority_rejects_host_bind_and_foreign_owner(self):
        namespace='kd-'+'b'*24
        permit=RecoveryPermit('a'*64,namespace,'12345678-1234-1234-1234-123456789abc','postgres','az-a',(namespace+'-pg',),('stop','start'))
        labels={'kindle.assurance.namespace':namespace,'kindle.assurance.attempt':permit.attempt,'kindle.assurance.role':'postgres','kindle.assurance.isolated':'true','kindle.assurance.failure-domain':'az-a'}
        metadata={'Id':permit.container_id,'Config':{'Labels':labels},'HostConfig':{},'Mounts':[{'Type':'volume','Name':namespace+'-pg','Driver':'local','Destination':'/var/lib/postgresql/data'}]}
        volumes=[{'Name':namespace+'-pg','Driver':'local','Options':None,'Labels':labels}]
        RecoveryFaults.validate_resource(permit,metadata,volumes)
        with self.assertRaises(BoundaryError):
            RecoveryFaults.validate_resource(permit,{**metadata,'Mounts':[{'Type':'bind','Source':'/','Destination':'/data'}]},volumes)
        with self.assertRaises(BoundaryError):
            RecoveryFaults.validate_resource(permit,metadata,[{**volumes[0],'Options':{'device':'/dev/sda'}}])
        with self.assertRaises(BoundaryError):
            RecoveryFaults.validate_resource(permit,metadata,[{**volumes[0],'Labels':{}}])
        with self.assertRaises(BoundaryError):
            RecoveryFaults.validate_resource(permit,{**metadata,'HostConfig':{'Devices':[{'PathOnHost':'/dev/sda'}]}},volumes)

    def test_denied_webhook_and_upload_cannot_change_persistence(self):
        fields=('streams','stream_outbox','stream_inbox','user_images','user_profiles')
        before={key:[] for key in fields}
        for field in fields:
            with self.subTest(field=field), self.assertRaises(persistence.PersistenceError):
                persistence.assert_webhook_unchanged(before,{**before,field:[{'unexpected':1}]})

    def test_acknowledged_business_row_loss_is_rejected(self):
        fields=('watchlist','account_outbox','chat','chat_outbox','created_pools','fills','stats','stats_outbox','chart_outbox','stats_inbox','pnl_inbox','candles_inbox','charts','buckets','positions')
        before={'database':'owned',**{key:[] for key in fields},'chat':[{'id':'durable-message','content':'persisted'}]}
        with self.assertRaises(persistence.PersistenceError):
            persistence.assert_acknowledged_preserved(before,{**before,'chat':[]})

    def test_rpc_quarantine_preserves_committed_history_and_stops_advancement(self):
        row={'stream_id':'owned-stream','height':100,'block_hash':'0x'+'a'*64,'integrity':'healthy'}
        before={'heads':[row],'quarantine':[]}
        quarantined={**row,'integrity':'quarantined'}
        persistence.assert_rpc_quarantine(before,{'heads':[quarantined],'quarantine':[]},'owned-stream',101)
        for heads in ([{**quarantined,'height':101}],[{**quarantined,'block_hash':'0x'+'b'*64}],[quarantined,{**quarantined,'height':101}]):
            with self.subTest(heads=heads), self.assertRaises(persistence.PersistenceError):
                persistence.assert_rpc_quarantine(before,{'heads':heads,'quarantine':[]},'owned-stream',101)


class ActualPhase3FaultJourneys(unittest.TestCase):
    def test_actual_current_candidate_faults_and_security(self):
        path=os.environ.get('PHASE3_CONFIG_FILE')
        digest=os.environ.get('PHASE3_CONFIG_SHA256')
        source=os.environ.get('QUALIFICATION_CANDIDATE_SHA256')
        output=os.environ.get('PHASE3_OUTPUT_DIR')
        self.assertTrue(path and digest and source and output,'actual admitted2.99, exact runtime/topology and current source binding required; absent providers cannot skip')
        configuration=load_phase3_configuration(Path(path),digest,source)
        result=Phase3Journey(configuration,path,Path(output)).run()
        self.assertTrue(result['phase3_qualified'])
        self.assertFalse(result['production_qualified'])
        self.assertFalse(result['capacity_qualified'])
        self.assertFalse(result['disaster_recovery_qualified'])


if __name__=='__main__':
    unittest.main(verbosity=2)
