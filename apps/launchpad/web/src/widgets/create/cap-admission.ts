import type { RuntimeAuthority } from '@/core/runtime-authority';
import type { AccountScope, LaunchDeployment } from './creation-adapters';
import type { CreationDraftV2 } from './creation-storage';
import { address } from './creation-domain';
import { readFreshCapability, CapabilityAdmissionError } from '../../../../../../services/launchpad/security/control/cap-admission';

export class KnownCapabilityAdmissionError extends Error {
 constructor(message:string,readonly cause?:unknown){super(message);this.name='KnownCapabilityAdmissionError';}
}
export type CreationCapabilityAdmission=(draft:CreationDraftV2,scope:AccountScope)=>Promise<void>;
export function runtimeCreationAdmission(authority:RuntimeAuthority,deployment:LaunchDeployment):CreationCapabilityAdmission {
 return async(draft,scope)=>{
  try{
   if(Date.now()>=authority.expiresAt||authority.revision.revisionSha256!==deployment.revision||scope.chainId!==125||address(draft.creator)!==address(scope.creator)||draft.chainId!==125)throw new Error('Committed creation authority expired or the reviewed deployment changed.');
   for(const [role,expected] of [['Router',deployment.router],['SidioraFactory',deployment.factory],['USDL',deployment.usdl],['ProtocolConfig',deployment.protocolConfig],['PoolRegistry',deployment.poolRegistry]] as const)if(address(authority.network.network.addresses[role]!)!==address(expected))throw new Error('Creation authority differs from the reviewed deployment.');
   if(draft.opticalSelection!=='standard')throw new Error('Selected optical schema has not been admitted.');
   const signal=AbortSignal.timeout(15000);
   const capability=await readFreshCapability('pool_creation',signal);
   if(!capability.allowed||capability.stale)throw new CapabilityAdmissionError(capability);
   const head=await authority.network.committedHead(signal);
   const proof=await authority.preflight('creation',{blockHash:head.hash,trader:address(scope.creator) as `0x${string}`,recipient:address(scope.creator) as `0x${string}`,creationPath:'Router',name:draft.name,symbol:draft.symbol,feeStrategy:draft.feeStrategy,optical:'0x0000000000000000000000000000000000000000'},signal);
   if(proof.supported!==true||proof.executionPermitted!==true||BigInt(String(proof.blockNumber))!==head.number)throw new Error('Creation is unavailable under the committed deployment capability.');
   if(Date.now()>=authority.expiresAt)throw new Error('Creation authority expired before admission.');
  }catch(error){if(error instanceof KnownCapabilityAdmissionError)throw error;throw new KnownCapabilityAdmissionError(error instanceof Error?error.message:'Creation authority is unavailable.',error);}
 };
}
