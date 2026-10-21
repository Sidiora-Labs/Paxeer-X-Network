import { defineConfig,mergeConfig } from 'vitest/config';
import { resolve } from 'node:path';
import baseline from '../../../../vitest.config';
export default mergeConfig(baseline,defineConfig({cacheDir:process.env.L26_CACHE_DIR??resolve(process.cwd(),'../../../evidence/production-system/L26/phase2/vite-cache'),test:{fileParallelism:false,maxWorkers:1,minWorkers:1,include:['src/widgets/trading/transaction-controls/domain.test.ts','src/widgets/trading/transaction-controls/receipt.chain.test.ts','src/widgets/trading/transaction-controls/trade-journey.integration.test.tsx','src/hooks/tx/use-optimistic-receipt.integration.test.tsx',...(process.env.L26_PHASE4==='1'?['src/widgets/trading/transaction-controls/measurement.integration.test.tsx']:[])]}}));
