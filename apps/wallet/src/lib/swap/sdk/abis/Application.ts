export const SIDIORA_POOL_REGISTRY_ABI = [
  'function getPoolByToken(address token) view returns (address pool)',
] as const;

export const SIDIORA_POOL_ABI = [
  'function getPrice() view returns (uint256 price)',
  'function getReserves() view returns (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenReserve)',
  'function swap(uint256 amountIn, uint256 minAmountOut, bool isBuy, address recipient, uint256 deadline) external returns (uint256 amountOut)',
  'function tokenAddress() view returns (address)',
  'function token() view returns (address)',
  'function totalSupply() view returns (uint256)',
] as const;

export const SIDIORA_QUOTER_ABI = [
  'function quoteExactInput(address pool, uint256 amountIn, bool isBuy) view returns (tuple(uint256 amountOut, uint256 feeAmount, uint256 priceImpactBps) result)',
  'function quoteMultihop(address tokenIn, address tokenOut, uint256 amountIn) view returns (tuple(uint256 amountOut, uint256 intermediateUsdl, uint256 sellFeeAmount, uint256 buyFeeAmount, uint256 sellPriceImpactBps, uint256 buyPriceImpactBps, uint256 combinedPriceImpactBps, address poolA, address poolB) result)',
] as const;

export const SIDIORA_ROUTER_ABI = [
  'function buy(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline) external returns (uint256 amountOut)',
  'function sell(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline) external returns (uint256 amountOut)',
  'function swapTokenToToken(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline) external returns (uint256 amountOut)',
] as const;

export const ERC20_ABI = [
  'function balanceOf(address owner) view returns (uint256)',
  'function decimals() view returns (uint8)',
  'function symbol() view returns (string)',
  'function name() view returns (string)',
  'function approve(address spender, uint256 amount) returns (bool)',
  'function allowance(address owner, address spender) view returns (uint256)',
  'function transfer(address to, uint256 amount) returns (bool)',
  'function totalSupply() view returns (uint256)',
] as const;
