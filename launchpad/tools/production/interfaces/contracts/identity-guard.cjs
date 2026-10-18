'use strict';
const assert=require('node:assert/strict');
const EXPECTED_PROXY_ROLES=Object.freeze(['EventEmitter','ProtocolConfig','Router','Quoter','SidioraFactory','PoolRegistry','FeeAccumulator','SidioraNFT','FeesRouter'].sort());
function assertChainId(chainId){assert.equal(BigInt(chainId),125n,'Wrong chain: Paxeer production/fixture identity must be125');}
function assertIdentityAttestation(result){
 assertChainId(result.chainId);
 assert.equal(result.status,'observed');
 assert.equal(result.configuration.creationFee,'1000000');
 assert.equal(result.usdlDecimals,'6');
 assert.equal(result.configuration.usdl.toLowerCase(),result.expectedUsdl.toLowerCase());
 assert.equal(result.usdlCodeHashMatchesManifest,true,'USDLcodeidentity mismatch');
 assert.equal(result.beacon.implementationMatchesManifest,true,'Beacon implementation address mismatch');
 assert.equal(result.beacon.implementationHashMatchesManifest,true,'Beacon implementation code mismatch');
 assert.equal(result.beacon.implementationHash,result.beacon.expectedImplementationHash,'Beacon implementation recorded code hash mismatch');
 assert.deepEqual(result.identities.map(i=>i.role).sort(),EXPECTED_PROXY_ROLES,'Expected exact nine unique proxy roles');
 for(const i of result.identities){
  assert.equal(i.hasCode,true,`${i.role}:missingcode`);
  assert.equal(i.runtimeHashMatchesManifest,true,`${i.role}:proxy runtime code drift`);
  assert.equal(i.runtimeHash,i.expectedRuntimeHash,`${i.role}:proxy recorded code hash mismatch`);
  assert.equal(i.implementationMatchesManifest,true,`${i.role}:implementation address drift`);
  assert.equal(i.implementationHashMatchesRecorded,true,`${i.role}:implementation code drift`);
  assert.equal(i.implementationHash,i.expectedImplementationHash,`${i.role}:implementation recorded code hash mismatch`);
 }
 const expectedArtifacts=[...EXPECTED_PROXY_ROLES,'SidioraPool'].sort();
 assert.deepEqual(result.localArtifactComparison.map(i=>i.role).sort(),expectedArtifacts,'Expected exact core implementation artifact coverage');
 for(const i of result.localArtifactComparison){
  assert.equal(i.equalsObservedImplementationHash,true,`${i.role}:local artifact code drift`);
  const expected=i.role==='SidioraPool'?result.beacon.implementationHash:result.identities.find(p=>p.role===i.role).implementationHash;
  assert.equal(i.selfPatchedArtifactHash,expected,`${i.role}:local artifact hash mismatch`);
 }
 assert.ok(result.block.hash&&result.block.number>0);
 return {identityVerified:true,governanceHandoffComplete:result.beacon.ownershipComplete,releaseEligible:result.beacon.ownershipComplete,scope:'Single-endpoint pinned-block code identity; no node-consensus or throughput qualification'};
}
module.exports={assertChainId,assertIdentityAttestation,EXPECTED_PROXY_ROLES};
