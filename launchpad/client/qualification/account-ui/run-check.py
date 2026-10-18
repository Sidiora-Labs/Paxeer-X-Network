"""Native owned check runner; preserve subprocess exit independently of executor wrapper."""
import json, pathlib, subprocess, sys
root = pathlib.Path(__file__).resolve().parents[3]
command = [str(root/'client/apps/web/node_modules/.bin/vitest'), 'run', '--config', 'client/qualification/account-ui/vitest.config.mts']
result = subprocess.run(command, cwd=root)
(root/'evidence/production-system/L31/phase1/native-exit.json').write_text(json.dumps({'command':command,'exit_code':result.returncode})+'\n')
sys.exit(result.returncode)
