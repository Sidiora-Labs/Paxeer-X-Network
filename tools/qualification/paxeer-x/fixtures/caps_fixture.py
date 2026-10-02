import json
import os
from pathlib import Path
import subprocess

SCENARIOS = ('empty-module', 'prefix-empty', 'populated', 'mixed-owner', 'mutation')
PREREQUISITE_MISSING = 3


class MissingPrerequisite(RuntimeError):
    pass


def run_scenarios(executable, runtime, output, first_sequence=1):
    """Runs every caps scenario in canonical order against one live isolated runtime.

    The scenarios mutate shared chain state, so they run once, in order, and each
    continues from the account sequences the previous scenario reported."""
    output = Path(output)
    output.mkdir(mode=0o700, exist_ok=False)
    state = {'CAPS_TREASURY_SEQUENCE': first_sequence, 'CAPS_BOB_SEQUENCE': first_sequence,
             'CAPS_TREASURY_SOURCE_SEQUENCE': first_sequence, 'CAPS_BOB_SOURCE_SEQUENCE': first_sequence}
    results = {}
    for scenario in SCENARIOS:
        env = dict(runtime.env, PAXEER_X_FIXTURE_KEYS=str(runtime.directory / 'keys'),
                   LAYERX_CAPS_SOCKET=str(runtime.directory / 'run/layerxd.lni.sock'),
                   LAYERX_CAPS_SALT=str(runtime.directory / 'salt'),
                   **{name: str(value) for name, value in state.items()})
        if 'populated' in results:
            env['CAPS_GRANT_ISSUED_MS'] = str(results['populated']['grant_issued_ms'])
        result = subprocess.run([str(executable), '--out', str(output), '--scenario', scenario],
                                env=env, capture_output=True, timeout=300)
        (output / (scenario + '.log')).write_bytes(result.stdout + result.stderr)
        if result.returncode == PREREQUISITE_MISSING:
            raise MissingPrerequisite('caps fixture prerequisite missing in scenario ' + scenario)
        if result.returncode:
            raise RuntimeError('caps fixture scenario ' + scenario + ' exit ' + str(result.returncode))
        record = json.loads((output / (scenario + '.json')).read_text())
        if record.get('scenario') != scenario or not (output / (scenario + '.bin')).stat().st_size:
            raise RuntimeError('caps fixture scenario output missing: ' + scenario)
        state = {'CAPS_TREASURY_SEQUENCE': record['next_sequence']['treasury'],
                 'CAPS_BOB_SEQUENCE': record['next_sequence']['bob'],
                 'CAPS_TREASURY_SOURCE_SEQUENCE': record['next_source_sequence']['treasury'],
                 'CAPS_BOB_SOURCE_SEQUENCE': record['next_source_sequence']['bob']}
        results[scenario] = record
    if results['empty-module']['items'] != 0 or results['populated']['items'] == 0:
        raise RuntimeError('caps fixture scenarios did not produce empty and populated snapshots')
    if len({record['state_root'] for record in results.values()}) != len(SCENARIOS):
        raise RuntimeError('caps fixture scenarios did not advance the committed root')
    for path in output.iterdir():
        os.chmod(path, 0o600)
    return results
