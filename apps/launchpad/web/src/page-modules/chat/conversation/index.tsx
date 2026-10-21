'use client';
import { useParams } from 'next/navigation';
import { useEffect, useState } from 'react';
import { useAccount } from 'wagmi';
import Link from 'next/link';
import { MessageCircle, SearchX } from 'lucide-react';
import DmThread from '@/widgets/chat/dm-thread';
import { getDmConversations, type DmConversation } from '@/core/clients/chat-api';
import { isSignedOutError } from '@/core/market/unsupported';
import { Button, EmptyState, ErrorState } from '@/ui/primitives';
import { SignedOutState } from '@/ui/shared/signed-out-state';

type Failure = { kind: 'signed-out' } | { kind: 'not-found' } | { kind: 'error'; message: string };

export default function ConversationModule(){
 const params=useParams();const {address}=useAccount();
 const id=typeof params.conversationId==='string'?params.conversationId:'';
 const [value,setValue]=useState<{wallet:string;conversation:DmConversation}>();const [failure,setFailure]=useState<Failure>();const [attempt,setAttempt]=useState(0);
 useEffect(()=>{const controller=new AbortController();setValue(undefined);setFailure(undefined);if(address)void getDmConversations(controller.signal).then(rows=>{if(controller.signal.aborted)return;const conversation=rows.find(row=>row.id===id);if(!conversation){setFailure({kind:'not-found'});return;}setValue({wallet:address,conversation});}).catch(cause=>{if(controller.signal.aborted)return;setFailure(isSignedOutError(cause)?{kind:'signed-out'}:{kind:'error',message:'Check your connection and try again'});});return()=>controller.abort();},[address,id,attempt]);
 const body=!address||failure?.kind==='signed-out'
  ?<SignedOutState icon={MessageCircle} title="Sign in to read this conversation" body="Direct messages are only visible to the people in them."/>
  :failure?.kind==='not-found'
   ?<EmptyState icon={SearchX} title="Conversation not found" body="It may have been deleted, or it belongs to another wallet" action={<Button variant="secondary" size="sm" href="/chat">Back to messages</Button>}/>
   :failure?.kind==='error'
    ?<ErrorState title="Can't load this chat right now" message={failure.message} onRetry={()=>setAttempt(value=>value+1)}/>
    :value&&value.wallet===address?<DmThread key={`${address}:${id}`} conversationId={id} peerAddress={value.conversation.peer}/>:<p role="status" className="p-4 text-sm text-text-faint">Loading...</p>;
 return <div className="mx-auto flex h-[calc(100vh-60px)] max-w-2xl flex-col text-text"><Link href="/chat">Back to messages</Link>{body}</div>;
}
