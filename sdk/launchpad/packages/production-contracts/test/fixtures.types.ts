import type { InputWalletProof, LoginResponse, UInt256 } from '../src/types.gen.js';
// Compile-time contract checks supplement the executable wire validators.
const eoa = {kind:'eoa_eip191',wallet:'0x1111111111111111111111111111111111111111',signature:'0x22'} satisfies InputWalletProof;
const contract = {kind:'erc1271',wallet:eoa.wallet,signature:'0x33'} satisfies InputWalletProof;
// Signature lengths are runtime schema constraints; fixture cryptographic validity is not claimed.
const raw: UInt256 = '115792089237316195423570985008687907853269984665640564039457584007913129639935';
// @ts-expect-error contract amounts are decimal strings, never JS numbers.
const wrongAmount: UInt256 = 123;
// @ts-expect-error unsupported proof discriminants are rejected at compile time.
const wrongProof: InputWalletProof = {kind:'injected',wallet:eoa.wallet,signature:'0x22'};
function nullableResponse(value: LoginResponse): string | null {
  return value.watermark === null ? null : value.watermark.height;
}
void [eoa,contract,raw,wrongAmount,wrongProof,nullableResponse];
