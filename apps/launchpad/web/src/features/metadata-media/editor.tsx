'use client';
import {useEffect,useId,useRef,useState} from 'react';
import {prepareMetadataV2Operation,sendMetadataV2Operation,type MetadataV2Data,type MetadataV2Operation} from '@/core/clients/metadata';
import {approvedOutcome} from './outcome';
import {reportUserFacingError} from '@/core/user-facing-error';
// Internal adapter outcome, not a frozen HTTP DTO. Serial integration must map
// frozen upload/status responses and preserve verified operation correlation.
export type ImageOutcome={operationId:string;assetId:string;state:'QUARANTINED'|'VALIDATING'|'APPROVED';revision?:string;url?:string};
// The injected transport owns authenticated revisioned operations. Retrying the
// same selected bytes preserves operation identity; selection changes retire it.
export function MetadataImageEditor({upload,activeRevision,onApproved}:{upload:(file:File,operationId:string,signal:AbortSignal)=>Promise<ImageOutcome>;activeRevision:string;onApproved?:(outcome:ImageOutcome)=>void}){
 const inputId=useId();const errorId=useId();
 const [file,setFile]=useState<File|null>(null);const [status,setStatus]=useState('');const [error,setError]=useState('');const [busy,setBusy]=useState(false);const operation=useRef('');const controller=useRef<AbortController|null>(null);const generation=useRef(0);
 useEffect(()=>()=>{generation.current++;controller.current?.abort()},[]);
 async function submit(){if(!file||busy)return;const g=++generation.current;const c=new AbortController();controller.current=c;setBusy(true);setError('');setStatus('Uploading...');try{const r=await upload(file,operation.current,c.signal);if(g!==generation.current)return;if(r.assetId!==operation.current)throw new Error('Image response did not match this operation.');if(approvedOutcome(r,operation.current,activeRevision)){setStatus('Image updated');onApproved?.(r);}else setStatus('Uploaded, checking your image...');}catch(e){if(g!==generation.current)return;if(e instanceof Error&&e.name==='AbortError')setError('Upload cancelled');else setError(reportUserFacingError(e,"Couldn't upload your image, try again",'metadata-image'));setStatus('');}finally{if(g===generation.current)setBusy(false)}}
 return <section aria-label="Coin image"><p>JPG, PNG or WebP, up to 6MB. It shows once it passes a quick check</p><label htmlFor={inputId}>Choose image</label><input id={inputId} type="file" accept="image/jpeg,image/png,image/webp" aria-describedby={errorId} aria-invalid={!!error} disabled={busy} onChange={e=>{generation.current++;controller.current?.abort();const f=e.target.files?.[0]??null;setStatus('');setError('');if(f&&(!['image/jpeg','image/png','image/webp'].includes(f.type)||f.size<1||f.size>6*1024*1024)){setFile(null);setError('Pick a JPG, PNG or WebP up to 6MB');return;}setFile(f);operation.current=crypto.randomUUID();}}/><button disabled={!file||busy} onClick={submit}>Upload</button><button disabled={!busy} onClick={()=>{generation.current++;controller.current?.abort();controller.current=null;setBusy(false);setStatus('');setError('Upload cancelled');}}>Cancel</button><p id={errorId} role="alert">{error}</p><p role="status">{status}</p></section>
}

export function MetadataV2Editor({tokenAddress,wallet,audience,metadata,signMessage,onCommitted}:{tokenAddress:string;wallet:string;audience:string;metadata:MetadataV2Data;signMessage:(message:string)=>Promise<string>;onCommitted:(metadata:MetadataV2Data)=>void}){
 const [name,setName]=useState(metadata.displayName??'');const [symbol,setSymbol]=useState(metadata.displaySymbol??'');const [description,setDescription]=useState(metadata.description);const [status,setStatus]=useState('');const [busy,setBusy]=useState(false);
 const pending=useRef<{key:string;operation:MetadataV2Operation;signature:string}|null>(null);
 async function save(){if(busy)return;setBusy(true);setStatus('Confirm in your wallet...');try{
 const patch={expectedRevision:metadata.revision,description,...(name?{displayName:name}:{}),...(symbol?{displaySymbol:symbol}:{})};const key=JSON.stringify(patch);
 if(!pending.current||pending.current.key!==key||Number(pending.current.operation.expiresAt)<=Math.floor(Date.now()/1000)){const operation=await prepareMetadataV2Operation(tokenAddress,wallet,audience,patch);const signature=await signMessage(operation.statement);pending.current={key,operation,signature}}
 const saved=await sendMetadataV2Operation(pending.current.operation,pending.current.signature);pending.current=null;onCommitted(saved);setStatus('Saved');
 }catch(error){setStatus(reportUserFacingError(error,"Couldn't save, try again",'metadata-save'))}finally{setBusy(false)}}
 return <section aria-label="Coin details"><label htmlFor="metadata-display-name">Name</label><input id="metadata-display-name" value={name} onChange={e=>setName(e.target.value)} disabled={busy}/><label htmlFor="metadata-display-symbol">Ticker</label><input id="metadata-display-symbol" value={symbol} onChange={e=>setSymbol(e.target.value)} disabled={busy}/><label htmlFor="metadata-description">Description</label><textarea id="metadata-description" value={description} onChange={e=>setDescription(e.target.value)} disabled={busy}/><button onClick={save} disabled={busy}>Save</button><p role="status">{status}</p></section>
}
