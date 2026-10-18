'use client';
import {useMemo,useState,type ReactNode} from 'react';
import {useQueryClient} from '@tanstack/react-query';
import {useMetadataV2,useMetadataMedia} from '@/hooks/market/use-token-metadata';
import type {MetadataV2Data,MetadataMediaImage} from '@/core/clients/metadata';
import {MetadataImageEditor} from './editor';
import {createMetadataImageTransport,type CreatorImageAuthority} from './transport';

export function MetadataView({tokenAddress,tradePanel,data,pending,error,image}:{tokenAddress:string;tradePanel:ReactNode;data:MetadataV2Data|null|undefined;pending:boolean;error:boolean;image?:MetadataMediaImage|null}){
 const safeImage=!error&&!!image&&image.assetId===data?.token.logoAssetId;
 const safeLink=(value:string)=>{try{const url=new URL(value);return url.protocol==='https:'&&!url.username&&!url.password}catch{return false}};
 return <section aria-label="Coin details"><code>{tokenAddress}</code>{pending?<p role="status">Loading...</p>:null}{error?<p role="alert">Can't load coin details right now, you can still trade</p>:null}{!pending&&!error&&!data?<p role="status">No coin details yet</p>:null}{data?<><h1>{data.displayName??data.token.name}</h1><p>Onchain name: {data.token.name}</p><p>{data.description}</p>{data.socialLinks.filter(link=>safeLink(link.url)).map(link=><a key={link.url} href={link.url} rel="noopener noreferrer" target="_blank">{link.label}</a>)}{safeImage&&image?<img src={image.variants['512']} srcSet={`${image.variants['256']} 256w, ${image.variants['512']} 512w, ${image.variants['1024']} 1024w`} sizes="(max-width: 512px) 100vw, 512px" width={image.width} height={image.height} style={{maxWidth:'100%',height:'auto'}} alt="Coin image"/>:null}</>:null}<div aria-label="Trading">{tradePanel}</div></section>
}
export function MetadataViewer({tokenAddress,tradePanel}:{tokenAddress:string;tradePanel:ReactNode}){
 const query=useMetadataV2(tokenAddress),media=useMetadataMedia(tokenAddress);
 const image=media.data&&query.data&&media.data.metadata.revision===query.data.revision?media.data.image:null;
 return <><MetadataView tokenAddress={tokenAddress} tradePanel={tradePanel} data={query.data} pending={query.isPending} error={query.isError} image={image}/>{media.isError&&query.data?.token.logoAssetId?<p role="alert">Can't load the coin image right now</p>:null}</>;
}
export function CreatorMetadataMedia({authority}:{authority:CreatorImageAuthority}){
 const [revision,setRevision]=useState(authority.revision);const cache=useQueryClient();
 const upload=useMemo(()=>createMetadataImageTransport({...authority,revision}),[authority,revision]);
 return <MetadataImageEditor upload={upload} activeRevision={revision} onApproved={result=>{if(result.revision){setRevision(result.revision);void cache.invalidateQueries({queryKey:['metadata-v2',125,authority.tokenAddress.toLowerCase()]});void cache.invalidateQueries({queryKey:['metadata-media-v2',125,authority.tokenAddress.toLowerCase()]})}}}/>;
}
