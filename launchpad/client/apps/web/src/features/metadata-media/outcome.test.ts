import {describe,it,expect} from 'vitest';
import {approvedOutcome,revisionValid} from './outcome';
describe('metadata image approval',()=>{
 const url=`https://cdn.example/approved/${'a'.repeat(64)}/256/${'b'.repeat(64)}.png`;
 it('requires correlated approved immutable revision',()=>{const r={operationId:'op',assetId:'11111111-1111-1111-1111-111111111111',state:'APPROVED' as const,revision:'8',url};expect(approvedOutcome(r,'op','7')).toBe(true);for(const v of [{...r,operationId:'other'},{...r,state:'VALIDATING' as const},{...r,revision:'7'},{...r,url:'https://evil.example/raw.png'},{...r,url:url+'?random=1'}])expect(approvedOutcome(v,'op','7')).toBe(false)});
 it('bounds both revisions before bigint parsing',()=>{for(const v of ['', '01','-1','x','9'.repeat(79)]){expect(revisionValid(v)).toBe(false);expect(approvedOutcome({operationId:'op',assetId:'x',state:'APPROVED',revision:'8',url},'op',v)).toBe(false)}})
});
