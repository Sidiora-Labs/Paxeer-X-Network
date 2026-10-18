import { keccak256, stringToHex, custom, type EIP1193Provider } from 'viem';
import type { RecoveryObservation } from './recovery';

export interface RpcObservation {
  kind:'rpc'; id:number; method:string; startedAt:number; finishedAt:number;
  startedMs:number; finishedMs:number; active:number; request:string;
  response?:string; requestKeccak256?:string; responseKeccak256?:string; redacted?:'wallet_signature'; status?:number; error?:string;
}
export interface OperationObservation {
  kind:'operation'; id:string; operation:'quote'|'simulation'|'parse'|'minimum';
  scheduledMs:number; startedMs:number; finishedMs:number; success:boolean;
  error?:string; contextKey?:string; quoteCalls?:number; simulationCalls?:number;
}
export function percentile(values:readonly number[],fraction:number):number {
  if(!values.length||!Number.isFinite(fraction)||fraction<=0||fraction>1||values.some(value=>!Number.isFinite(value)||value<0))throw new Error('Invalid measured percentile input');
  const sorted=[...values].sort((a,b)=>a-b);return sorted[Math.ceil(sorted.length*fraction)-1];
}
export function operationSummary(samples:readonly OperationObservation[]){
  if(!samples.length||new Set(samples.map(sample=>sample.id)).size!==samples.length)throw new Error('Actual unique operation samples required');
  for(const sample of samples)if(![sample.scheduledMs,sample.startedMs,sample.finishedMs].every(Number.isFinite)||sample.scheduledMs>sample.startedMs||sample.startedMs>sample.finishedMs)throw new Error('Invalid measured operation timeline');
  const corrected=samples.filter(sample=>sample.success).map(sample=>sample.finishedMs-sample.scheduledMs);
  const service=samples.filter(sample=>sample.success).map(sample=>sample.finishedMs-sample.startedMs);
  const elapsed=Math.max(...samples.map(sample=>sample.finishedMs))-Math.min(...samples.map(sample=>sample.scheduledMs));
  if(elapsed<=0)throw new Error('Nonzero actual measurement duration required');
  return {samples:samples.length,successes:corrected.length,failures:samples.length-corrected.length,errorRate:(samples.length-corrected.length)/samples.length,elapsedMs:elapsed,completedRps:corrected.length*1000/elapsed,correctedP95Ms:corrected.length?percentile(corrected,.95):null,correctedP99Ms:corrected.length?percentile(corrected,.99):null,serviceP95Ms:service.length?percentile(service,.95):null,serviceP99Ms:service.length?percentile(service,.99):null};
}
export function recoverySummary(samples:readonly RecoveryObservation[]){
  const starts=samples.filter(sample=>sample.kind==='read_start'),ends=samples.filter(sample=>sample.kind==='read_end'),retries=samples.filter(sample=>sample.kind==='retry_scheduled');
  if(!starts.length||starts.some(sample=>sample.activeReaders<1||sample.activeReaders>4))throw new Error('Actual receipt reader concurrency exceeds four');
  for(const retry of retries){const base=Math.min(10000,500*2**Math.min(retry.retries-1,5));if(retry.delayMs===undefined||retry.delayMs<500||retry.delayMs>10000||retry.delayMs<Math.max(500,Math.floor(base*.8))||retry.delayMs>Math.min(10000,Math.floor(base*1.2)))throw new Error('Receipt retry violates bounded exponential jitter');}
  return {readStarts:starts.length,readEnds:ends.length,maximumActive:Math.max(...starts.map(sample=>sample.activeReaders)),retries:retries.length,wakes:samples.filter(sample=>sample.kind==='wake').length,quarantined:ends.filter(sample=>sample.state==='quarantined').length};
}
export function measuredRpcTransport(url:string,observe:(sample:RpcObservation)=>void,allowFixtureMutation:boolean){
  const endpoint=new URL(url);if(!['127.0.0.1','localhost','[::1]'].includes(endpoint.hostname)||endpoint.protocol!=='http:'||endpoint.username||endpoint.password)throw new Error('Explicit owned loopback RPC required');
  let serial=0,active=0;
  const provider={request:async({method,params=[]}:{method:string;params?:unknown[]})=>{
    if(!/^(eth_|net_|web3_)/.test(method)&&!(allowFixtureMutation&&['evm_setAutomine','evm_mine','anvil_setAutomine','anvil_mine'].includes(method)))throw new Error('Unadmitted measurement RPC method');
    if(!allowFixtureMutation&&['eth_sendTransaction','eth_sendRawTransaction'].includes(method))throw new Error('Fixture mutation not admitted');
    const id=++serial,startedAt=Date.now(),startedMs=performance.now();const request=JSON.stringify({jsonrpc:'2.0',id,method,params});if(request.length>1048576)throw new Error('RPC input exceeds bound');
    const sensitive=['eth_sign','personal_sign','eth_signTypedData','eth_signTypedData_v4'].includes(method);
    const sample:RpcObservation={kind:'rpc',id,method,startedAt,finishedAt:startedAt,startedMs,finishedMs:startedMs,active:++active,request:sensitive?'wallet signing payload redacted':request,...(sensitive?{requestKeccak256:keccak256(stringToHex(request)),redacted:'wallet_signature' as const}:{})};
    try{
      const response=await fetch(endpoint,{method:'POST',headers:{'content-type':'application/json'},body:request,signal:AbortSignal.timeout(10000),redirect:'error'});sample.status=response.status;
      if(!response.body)throw new Error('Missing actual RPC body');const reader=response.body.getReader();let bytes=0;const chunks:Uint8Array[]=[];
      for(;;){const part=await reader.read();if(part.done)break;bytes+=part.value.byteLength;if(bytes>8388608){await reader.cancel();throw new Error('RPC response exceeds bound');}chunks.push(part.value);}
      const body=new Uint8Array(bytes);let offset=0;for(const chunk of chunks){body.set(chunk,offset);offset+=chunk.byteLength;}const responseText=new TextDecoder().decode(body);if(sensitive){sample.responseKeccak256=keccak256(stringToHex(responseText));}else sample.response=responseText;
      if(!response.ok)throw new Error(`RPC HTTP ${response.status}`);const value=JSON.parse(responseText);
      if(value.jsonrpc!=='2.0'||value.id!==id)throw new Error('RPC response identity mismatch');
      if(value.error)throw Object.assign(new Error(String(value.error.message)),{code:value.error.code,data:value.error.data});
      if(!Object.hasOwn(value,'result'))throw new Error('RPC result absent');return value.result;
    }catch(error){sample.error=sensitive?'Wallet signing request failed':error instanceof Error?error.message:String(error);throw error;}
    finally{active--;sample.finishedAt=Date.now();sample.finishedMs=performance.now();observe(Object.freeze(sample));}
  }};
  return custom(provider as EIP1193Provider,{retryCount:0});
}
export interface BrowserObservation {kind:string;txHash?:string;at:number;receiptHeight?:string;watermarkHeight?:string;integrity?:string;requestId?:string;error?:string}
export function assertCompletionEvidence(events:readonly BrowserObservation[],hash:string){
  if(!/^0x[0-9a-fA-F]{64}$/.test(hash)||events.some(event=>!Number.isSafeInteger(event.at)||event.at<0))throw new Error('Invalid actual browser observation identity or clock');
  const matching=events.filter(event=>event.txHash?.toLowerCase()===hash.toLowerCase());
  const completions=matching.filter(event=>event.kind==='trade_confirmed'),toasts=matching.filter(event=>event.kind==='toast_confirmed'),aligned=matching.filter(event=>event.kind==='history_aligned');
  if(completions.length!==1||toasts.length!==1||aligned.length!==1)throw new Error('Exactly one actual trade, toast and aligned history observation required');
  if(!/^(0|[1-9][0-9]{0,77})$/.test(completions[0].receiptHeight??'')||!/^(0|[1-9][0-9]{0,77})$/.test(aligned[0].watermarkHeight??'')||!aligned[0].requestId||aligned[0].requestId.length>256)throw new Error('Actual receipt height, watermark and request identity required');
  const height=BigInt(completions[0].receiptHeight!);
  if(aligned[0].integrity!=='healthy'||BigInt(aligned[0].watermarkHeight!)<height||aligned[0].at<completions[0].at)throw new Error('History evidence does not align to verified receipt');
  return {completionCount:completions.length,toastCount:toasts.length,historyLatencyMs:aligned[0].at-completions[0].at,receiptHeight:height.toString()};
}
export function emitBrowserObservation(observation:Omit<BrowserObservation,'at'>){
  if(typeof window!=='undefined'&&(window as Window&{__L26_MEASUREMENT_ACTIVE__?:boolean}).__L26_MEASUREMENT_ACTIVE__===true)window.dispatchEvent(new CustomEvent('sidiora:trade-measurement',{detail:{...observation,at:Date.now()}}));
}
