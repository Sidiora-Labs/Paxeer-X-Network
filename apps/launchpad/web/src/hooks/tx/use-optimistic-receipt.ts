'use client';
import { useEffect,useRef,useState } from 'react';
import { usePublicClient } from 'wagmi';
import type { TransactionReceipt } from 'viem';
import { useMarketTopics } from '@/core/realtime/use-data-stream';
import { marketTopics } from '@/core/realtime/data-stream';
import { ReceiptRecoveryScheduler } from '@/widgets/trading/transaction-controls/recovery';
import { EvidenceIntegrityError, transactionFingerprint } from '@/widgets/trading/transaction-controls/providers';
export type OptimisticReceipt=TransactionReceipt;
/**
 * Read hint for the receipt query: the pools the transaction trades on. Their
 * public v2 topics market.trades.<pool> (core/api WS /v2/stream) carry each
 * committed Trade; a Trade whose position.transactionHash is this transaction
 * wakes the receipt recovery immediately. The hint never settles the hook: only
 * the chain receipt does.
 */
export interface StreamConfirm {poolAddress?:string|null;poolAddresses?:readonly(string|null|undefined)[];enabled?:boolean}
function tradeTxHash(payload:unknown):string|undefined{
  if(!payload||typeof payload!=='object')return;
  const position=(payload as {position?:unknown}).position;
  if(!position||typeof position!=='object')return;
  const hash=(position as {transactionHash?:unknown}).transactionHash;return typeof hash==='string'?hash:undefined;
}
function confirmTopics(confirm:StreamConfirm|undefined):string[]{
  const pools=[confirm?.poolAddress,...(confirm?.poolAddresses??[])].filter((pool):pool is string=>typeof pool==='string'&&/^0x[0-9a-fA-F]{40}$/.test(pool));
  return [...new Set(pools.map(pool=>marketTopics.trades(pool)))].sort();
}
export function useOptimisticReceipt(txHash:`0x${string}`|undefined,confirm?:StreamConfirm){
  const publicClient=usePublicClient({chainId:125});const scheduler=useRef<ReceiptRecoveryScheduler|undefined>(undefined);
  const [result,setResult]=useState<{hash?:string;receipt?:TransactionReceipt;error?:unknown;quarantined?:boolean}>({});
  const submittedAt=useRef({hash:txHash,at:Date.now()});
  if(submittedAt.current.hash!==txHash)submittedAt.current={hash:txHash,at:Date.now()};const [clock,setClock]=useState(Date.now());
  useEffect(()=>{submittedAt.current={hash:txHash,at:Date.now()};setClock(Date.now());if(!txHash)return;const timer=setInterval(()=>setClock(Date.now()),500);return()=>clearInterval(timer);},[txHash]);
  useEffect(()=>{
    setResult({});if(!txHash||!publicClient)return;
    const signal=new AbortController();let timer:ReturnType<typeof setTimeout>|undefined;let tries=0;let canonical:TransactionReceipt|undefined;
    const recovery=new ReceiptRecoveryScheduler(async(attempt,abort)=>{
      if(await publicClient.getChainId()!==125)throw new EvidenceIntegrityError('Receipt chain conflict');
      let receipt:TransactionReceipt;
      try{receipt=await publicClient.getTransactionReceipt({hash:txHash});}catch(error){if(error instanceof Error&&/not found|could not be found/i.test(error.message))return undefined;throw error;}
      if(!receipt)return undefined;
      const block=await publicClient.getBlock({blockNumber:receipt.blockNumber});
      if(abort.aborted)return undefined;
      if(receipt.transactionHash.toLowerCase()!==attempt.txHash.toLowerCase()||block.hash!==receipt.blockHash)throw new EvidenceIntegrityError('Receipt integrity conflict');
      canonical=receipt;return receipt;
    });scheduler.current=recovery;
    const register=async()=>{
      try{
        const transaction=await publicClient.getTransaction({hash:txHash});if(signal.signal.aborted)return;
        recovery.watch({version:1,intentId:txHash,chainId:125,from:transaction.from,txHash,submittedAt:submittedAt.current.at,nonce:transaction.nonce,action:'receipt',actionFingerprint:transactionFingerprint(transaction.to??transaction.from,transaction.input,transaction.value)},update=>{if(signal.signal.aborted)return;if(update.receipt&&canonical)setResult({hash:txHash,receipt:canonical});else if(update.error)setResult({hash:txHash,error:update.error,quarantined:update.state==='quarantined'});});
      }catch(error){if(signal.signal.aborted)return;setResult({hash:txHash,error});timer=setTimeout(()=>void register(),Math.min(10000,500*2**Math.min(tries++,5)));}
    };
    void register();return()=>{signal.abort();clearTimeout(timer);recovery.close();if(scheduler.current===recovery)scheduler.current=undefined;};
  },[txHash,publicClient]);
  const topics=confirmTopics(confirm);
  const hint=(payload:unknown)=>{const hash=tradeTxHash(payload);if(txHash&&hash?.toLowerCase()===txHash.toLowerCase())scheduler.current?.wake(txHash);};
  useMarketTopics(topics,{onSnapshot:snapshot=>{for(const entry of snapshot.topics)hint(entry.payload);},onEvent:event=>hint(event.payload),enabled:(confirm?.enabled??true)&&!!txHash&&topics.length>0});
  const receipt=txHash&&result.receipt?.transactionHash.toLowerCase()===txHash.toLowerCase()?result.receipt:undefined;
  const state=receipt?(receipt.status==='success'?'confirmed_success':'confirmed_reverted'):result.hash===txHash&&result.quarantined?'quarantined':!txHash?'idle':clock-submittedAt.current.at>=30000?'delayed':clock-submittedAt.current.at>=2000?'checking':'submitted';
  return {receipt,state,isOptimistic:false,error:result.hash===txHash?result.error:undefined,isPending:!!txHash&&!receipt};
}
