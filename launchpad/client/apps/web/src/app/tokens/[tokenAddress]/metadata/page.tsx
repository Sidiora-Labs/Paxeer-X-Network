'use client';

import { useEffect, useState } from 'react';
import { useParams } from 'next/navigation';
import { useAccount, useSignMessage } from 'wagmi';
import { fetchMetadataV2, type MetadataV2Data } from '@/core/clients/metadata';
import { metadataApiUrl } from '@/core/sdk-config';
import { MetadataV2Editor } from '@/features/metadata-media/editor';
import WalletButton from '@/shell/wallet-button';

export default function TokenMetadataPage() {
 const params=useParams<{tokenAddress:string}>();
 const token=(typeof params.tokenAddress==='string'?params.tokenAddress:'').toLowerCase();
 const {address,isConnected,chainId}=useAccount();
 const {signMessageAsync}=useSignMessage();
 const [metadata,setMetadata]=useState<MetadataV2Data|null>(null);
 const [audience,setAudience]=useState('');
 const [loading,setLoading]=useState(true);
 const [error,setError]=useState('');
 const [reload,setReload]=useState(0);
 useEffect(()=>{
  const controller=new AbortController();let active=true;
  setMetadata(null);setError('');setLoading(true);
  if(!/^0x[a-f0-9]{40}$/.test(token)){setError('This is not a valid coin address');setLoading(false);return()=>controller.abort()}
  try{setAudience(new URL(metadataApiUrl('/'),window.location.origin).origin)}catch{setError("Coin details can't be edited right now");setLoading(false);return()=>controller.abort()}
  fetchMetadataV2(token,controller.signal).then(value=>{if(active){setMetadata(value);if(!value)setError('No details found for this coin')}}).catch(()=>{if(active)setError("Can't load coin details right now");}).finally(()=>{if(active)setLoading(false)});
  return()=>{active=false;controller.abort()};
 },[token,reload]);
 return <div className="mx-auto max-w-2xl px-4 py-8 text-white"><h1 className="text-2xl font-semibold">Coin details</h1>
 {loading&&<p role="status">Loading...</p>}
 {error&&<div role="alert"><p>{error}</p><button type="button" onClick={()=>setReload(n=>n+1)}>Try again</button></div>}
 {metadata&&<><p>{metadata.token.name} ({metadata.token.symbol})</p><p className="break-all">{metadata.token.address}</p>
 {!isConnected||!address?<><p>Connect the creator wallet to edit coin details</p><WalletButton/></>:chainId!==125?<><p role="alert">Switch your wallet to Paxeer to edit coin details</p><WalletButton/></>:<MetadataV2Editor key={`${token}:${address}:${metadata.revision}`} tokenAddress={token} wallet={address} audience={audience} metadata={metadata} signMessage={message=>signMessageAsync({message,account:address})} onCommitted={setMetadata}/>}</>}
 </div>;
}
