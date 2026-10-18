'use client';
import {useCallback,useEffect,useRef,useState} from 'react';
import {useAccountSession} from '../profile/use-account-session';
import {accountSession,assertAccount,requireAccount} from '../profile/account-transport';
import {checkedSharePreview} from './share-disclosure';
import {AccountApiError} from '../profile/account-transport';
import {actionFailureMessage} from '@/core/market/unsupported';
import {reportUserFacingError} from '@/core/user-facing-error';
/** Server failures in plain words ("PnL sharing is not available yet." for an unserved route); local validation messages are already sentences. */
const shareFailure=(e:unknown,fallback:string)=>e instanceof AccountApiError?actionFailureMessage('PnL sharing',e):reportUserFacingError(e,fallback,'pnl card');
import {ownerShare,previewShare,publishShare,revokeShare,sanitizedShare,type PublicationField,type SharePreview,type OwnerShare} from './share-transport';
export type MintState={kind:'idle'}|{kind:'minting'}|{kind:'error';message:string}|{kind:'ready';preview:SharePreview;owner?:OwnerShare;error?:string};
export function useCardMinter(){
 const auth=useAccountSession();
 const [record,setRecord]=useState<{scope:string;state:MintState}|null>(null);
 const mutation=useRef<string|null>(null),busy=useRef(false),generation=useRef(0);
 const scope=auth.session?`${auth.session.session.sessionId}:${auth.session.epoch}`:null;
 const state:MintState=record?.scope===scope?record.state:{kind:'idle'};
 useEffect(()=>{generation.current++;mutation.current=null;busy.current=false;setRecord(null);},[scope]);
 useEffect(()=>()=>{generation.current++;},[]);
 const mint=useCallback(async(args:{ownerAddress:string;poolAddress:string;snapshotId?:string})=>{
  if(!scope||busy.current)return;
  const account=requireAccount(args.ownerAddress),observed=++generation.current;
  busy.current=true;setRecord({scope,state:{kind:'minting'}});
  try{
   if(!args.snapshotId)throw new Error('Select an authoritative portfolio snapshot before sharing.');
   const selection={fields:['realizedPnlRaw'] as PublicationField[],includeWallet:false,expiresAt:new Date(Date.now()+86400000).toISOString()};
   const preview=checkedSharePreview(await previewShare({snapshotId:args.snapshotId,...selection}),selection,account.session.wallet);
   assertAccount(account);if(observed!==generation.current)return;
   mutation.current=crypto.randomUUID();setRecord({scope,state:{kind:'ready',preview}});
  }catch(e){if(accountSession()===account&&observed===generation.current)setRecord({scope,state:{kind:'error',message:shareFailure(e,"Couldn't load the preview, try again")}});}
  finally{if(observed===generation.current)busy.current=false;}
 },[scope]);
 async function select(snapshotId:string,fields:PublicationField[],includeWallet:boolean,expiresAt:string){
  if(!scope||busy.current||state.kind!=='ready'||state.owner)return;
  const account=requireAccount(),observed=++generation.current;busy.current=true;
  try{const selection={fields,includeWallet,expiresAt};const preview=checkedSharePreview(await previewShare({snapshotId,...selection}),selection,account.session.wallet);assertAccount(account);if(observed!==generation.current)return;mutation.current=crypto.randomUUID();setRecord({scope,state:{kind:'ready',preview}});}
  catch(e){if(accountSession()===account&&observed===generation.current)setRecord({scope,state:{...state,error:shareFailure(e,"Couldn't load the preview, try again")}});}
  finally{if(observed===generation.current)busy.current=false;}
 }
 async function ownerAction(action:'publish'|'refresh'|'revoke'){
  if(!scope||state.kind!=='ready'||busy.current||!mutation.current)return;
  if(action==='publish'&&state.owner)return;
  const account=requireAccount(),observed=++generation.current;busy.current=true;
  try{
   if(action==='publish'&&Date.parse(state.preview.previewUntil)<=Date.now())throw new Error('Preview expired. Request a fresh disclosure preview.');
   const owner=action==='publish'?await publishShare(state.preview,mutation.current):action==='refresh'?await ownerShare(state.preview.id):await revokeShare(state.preview.id);
   assertAccount(account);if(observed!==generation.current)return;
   if(owner.id!==state.preview.id||owner.share.id!==state.preview.id)throw new Error('Publication acknowledgment identity mismatch');
   const share=sanitizedShare(owner.share);
   if(JSON.stringify(share)!==JSON.stringify(state.preview.share))throw new Error('Publication acknowledgment changed disclosure');
   setRecord({scope,state:{...state,owner:{...owner,share},error:undefined}});
  }catch(e){if(accountSession()===account&&observed===generation.current)setRecord({scope,state:{...state,error:shareFailure(e,"Couldn't share your card, try again")}});}
  finally{if(observed===generation.current)busy.current=false;}
 }
 const reset=useCallback(()=>{if(busy.current)return;generation.current++;setRecord(null);mutation.current=null;},[]);
 return{mint,reset,state,select,publish:()=>ownerAction('publish'),refresh:()=>ownerAction('refresh'),revoke:()=>ownerAction('revoke')};
}
