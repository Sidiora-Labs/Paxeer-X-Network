import os
import pathlib
import subprocess
import sys
root = pathlib.Path(__file__).resolve().parents[4]
required = ['NEXT_PUBLIC_USER_API_URL', 'NEXT_PUBLIC_GATEWAY_URL', 'L31_REAL_ACCESS_TOKEN', 'L31_REAL_SESSION_ID', 'L31_REAL_POOL_ADDRESS', 'NEXT_PUBLIC_PNL_API_URL', 'L31_REAL_OTHER_ACCESS_TOKEN', 'L31_REAL_OWNER_ASSET_ID', 'L31_REAL_EXPIRED_ACCESS_TOKEN', 'L31_REAL_DEPOSIT_FROM_UTC', 'L31_REAL_DEPOSIT_TO_UTC', 'L31_REAL_REBUY_TOKEN_ADDRESS', 'L31_REAL_REBUY_REMAINING_BASIS_USDL_RAW', 'L31_REAL_RPC_URL', 'L31_REAL_USDL_ADDRESS', 'L31_REAL_DEPOSIT_TRANSACTION_HASH', 'L31_REAL_FIRST_BUY_TRANSACTION_HASH', 'L31_REAL_FULL_SELL_TRANSACTION_HASH', 'L31_REAL_REBUY_TRANSACTION_HASH']
missing = [name for name in required if not os.environ.get(name)]
if missing:
    raise SystemExit('Real account/identity/indexer fixture inputs required: ' + ', '.join(missing))
command = [str(root / 'apps/launchpad/web/node_modules/.bin/vitest'), 'run', '--config', 'apps/launchpad/qualification/account-ui/vitest.config.mts', 'apps/launchpad/qualification/account-ui/account-journeys.test.tsx']
raise SystemExit(subprocess.run(command, cwd=root).returncode)
