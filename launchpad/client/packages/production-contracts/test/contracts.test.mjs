import test from 'node:test';
import assert from 'node:assert/strict';
import { validateContract, decodeContract, ContractValidationError } from '../dist/index.js';
const session = {sessionId:'00000000-0000-4000-8000-000000000001',wallet:'0x'+'11'.repeat(20),accessExpiresAt:'2026-10-09T01:00:00.000Z',refreshExpiresAt:'2026-11-08T00:00:00.000Z',scopes:['profile:read'],deviceLabel:'fixture',revokedAt:null};
const response = {requestId:'00000000-0000-4000-8000-000000000002',data:{session,accessToken:'fixture-not-a-credential'},watermark:null,revision:'1',quality:'complete',nextCursor:null,generatedAt:'2026-10-09T00:00:00.000Z'};
test('additive response compatibility, explicit null and schema major',()=>{
 const value=decodeContract('LoginResponse',{...response,futureAdditiveField:{minor:2}});
 assert.equal(value.data.session.wallet,session.wallet);assert.equal(value.watermark,null);assert.equal(value.nextCursor,null);
 assert.throws(()=>decodeContract('LoginResponse',response,2),/Unsupported schema major/);
 const {watermark,...missing}=response;assert.throws(()=>decodeContract('LoginResponse',missing),ContractValidationError);
});
test('integer strings preserve uint256 and reject numeric coercion/overflow',()=>{
 const max='115792089237316195423570985008687907853269984665640564039457584007913129639935';
 assert.equal(decodeContract('UInt256',max),max);
 for(const invalid of [123,'01','-1','1e5',(BigInt(max)+1n).toString()])assert.equal(validateContract('UInt256',invalid),false);
 assert.equal(validateContract('AggregateUInt','9'.repeat(156)),true);assert.equal(validateContract('AggregateUInt','9'.repeat(157)),false);
 assert.equal(validateContract('PriceComponent','9'.repeat(512)),true);assert.equal(validateContract('PriceComponent','9'.repeat(513)),false);
 assert.equal(validateContract('UInt63','9223372036854775808'),false);
});
test('strict mutation inputs, EOA and variable ERC1271 discriminated proof',()=>{
 const eoa={challengeId:'00000000-0000-4000-8000-000000000003',proof:{kind:'eoa_eip191',wallet:'0x'+'11'.repeat(20),signature:'0x'+'22'.repeat(65)},deviceLabel:'fixture'};
 assert.equal(validateContract('LoginBody',eoa),true);assert.equal(validateContract('LoginBody',{...eoa,admin:true}),false);
 assert.equal(validateContract('LoginBody',{...eoa,proof:{...eoa.proof,kind:'erc1271',signature:'0x'+'33'.repeat(96)}}),true);
 assert.equal(validateContract('LoginBody',{...eoa,proof:{...eoa.proof,signature:'0x22'}}),false);
});
test('invalid contract name and JSON transport cannot become typed success',()=>{
 assert.throws(()=>validateContract('MissingContract',{}),/Unknown contract/);
 assert.throws(()=>decodeContract('LoginResponse','not-an-object'),ContractValidationError);
 assert.equal(validateContract('Session',{...session,revokedAt:'not-a-date'}),false);
});
test('optional clear operations preserve explicit empty/false versus absence',()=>{
 for(const input of [{expectedRevision:'1',description:''},{expectedRevision:'1',tags:[]},{expectedRevision:'1'}])assert.deepEqual(JSON.parse(JSON.stringify(decodeContract('MetadataPatchBody',input))),input);
 for(const input of [{unreadOnly:false},{}])assert.deepEqual(JSON.parse(JSON.stringify(decodeContract('InputTopicFilters',input))),input);
});
test('mathematical integer lexical forms agree after JSON decoding',()=>{
 for(const number of ['1','1.0','1e0']) {const input=JSON.parse(`{"poolAddress":"0x${'11'.repeat(20)}","limit":${number}}`);assert.equal(decodeContract('CalloutListRequest',input).limit,1);}
});
test('application timestamps reject leap seconds and impossible calendar dates',()=>{
 for(const date of ['2026-12-31T23:59:60.000Z','2026-02-30T00:00:00.000Z'])assert.equal(validateContract('Session',{...session,accessExpiresAt:date}),false);
});
test('accounting corpus exact wire validation independent of semantic arithmetic authority',async()=>{
 const {readFile}=await import('node:fs/promises');
 const corpus=JSON.parse(await readFile(new URL('../../../../spec/production-system/integration/contracts/v1/accounting-cases.json',import.meta.url),'utf8'));
 assert.ok(corpus.cases.length>=59);
 for(const fixture of corpus.cases)assert.equal(validateContract(fixture.schema,fixture.instance),fixture.expectedSchemaValidity,fixture.id);
});
test('Gregorian application timestamp bounds and leap-year parity',()=>{
 for(const date of ['0001-01-01T00:00:00.000Z','9999-12-31T23:59:59.999Z','2000-02-29T00:00:00.000Z','2024-02-29T00:00:00.000Z'])assert.equal(validateContract('Session',{...session,accessExpiresAt:date}),true,date);
 for(const date of ['0000-01-01T00:00:00.000Z','1900-02-29T00:00:00.000Z','2100-02-29T00:00:00.000Z','2026-04-31T00:00:00.000Z','2026-13-01T00:00:00.000Z','2026-10-09T24:00:00.000Z','2026-10-09T00:00:60.000Z','2026-10-09T00:00:00Z'])assert.equal(validateContract('Session',{...session,accessExpiresAt:date}),false,date);
});
