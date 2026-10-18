import TokenMarketLayout from '@/widgets/trading/token-market-layout';
interface TokenModuleProps { poolAddress: string; }
/** Coin page module; the layout resolves the pool or token address through usePoolSnapshot. */
export default function TokenModule({ poolAddress }: TokenModuleProps) {
  return <TokenMarketLayout poolAddress={poolAddress} />;
}
