'use client';
import { useState, useEffect, useRef } from 'react';
import { useAccount } from 'wagmi';
import { useDmChat } from '@/hooks/chat/use-chat';
import { MessageCircle } from 'lucide-react';
import { formatAddress } from '@/utils/format';
import { EmptyState } from '@/ui/primitives';
export default function DmThread({ conversationId, peerAddress, peerName }: { conversationId: string; peerAddress: string; peerName?: string }) {
 const { address } = useAccount();
 const chat = useDmChat(conversationId,peerAddress);
 const [input,setInput]=useState('');
 const [offset,setOffset]=useState(0);
 const [editing,setEditing]=useState<{id:string;content:string}>();
 const wallet=useRef(address);
 const editor=useRef<HTMLInputElement>(null);
 useEffect(()=>{setInput('');setOffset(0);setEditing(undefined);wallet.current=address;},[address,conversationId]);
 const markRead=chat.markRead;
 useEffect(()=>{if(document.visibilityState==='visible')markRead();},[chat.messages,markRead]);
 const current=wallet.current===address;
 const visible=current ? chat.messages.slice(Math.max(0,chat.messages.length-200-offset),Math.max(0,chat.messages.length-offset)) : [];
 const submit=async()=>{const draft=input;if(await chat.sendMessage(draft))setInput(value=>value===draft?'':value);};
 return <div className="flex h-full flex-col">
  <header className="bg-surface-1 px-4 py-3"><a href={`/profile/${peerAddress}`}>{peerName||formatAddress(peerAddress,6)}</a></header>
  <div role="status" aria-live="polite" className="px-4 py-2 text-2xs text-text-faint">{chat.error||(!chat.isAuthenticated?'Sign in to send messages':'')}</div>
  {chat.wsStatus==='authorization-required'&&<button onClick={()=>chat.connectChat()}>Try again</button>}
  <div role="log" aria-label="Messages" tabIndex={0} className="flex-1 overflow-y-auto px-4 py-3">
   {chat.loading?<p>Loading...</p>:!chat.error&&chat.messages.length===0?<EmptyState icon={MessageCircle} title="No messages yet" body="Say hi to start the chat" announce/>:null}
   {chat.messages.length>200+offset&&<button onClick={()=>setOffset(value=>value+200)}>Earlier messages</button>}
   {visible.map(message=><article key={message.id} data-message-id={message.id} className={`my-3 rounded-2xl p-3 ${message.sender.toLowerCase()===address?.toLowerCase()?'bg-accent-soft':'bg-surface-2'}`}>
    {editing?.id===message.id?<div><label className="sr-only" htmlFor={`edit-${message.id}`}>Edit message</label><textarea id={`edit-${message.id}`} value={editing.content} maxLength={2000} onChange={event=>setEditing({id:message.id,content:event.target.value})}/><button onClick={async()=>{if(await chat.reviseMessage(message,editing.content))setEditing(undefined);}}>Save</button><button onClick={()=>setEditing(undefined)}>Cancel</button></div>:<p>{message.deleted?'This message was removed':message.content}</p>}
    {!message.deleted&&message.sequence!=='0'&&message.sender.toLowerCase()===address?.toLowerCase()&&<div>{Date.now()/1000-message.createdAt<=900&&<button onClick={()=>setEditing({id:message.id,content:message.content})}>Edit</button>}<button onClick={()=>chat.reviseMessage(message,'',true)}>Delete</button></div>}
    {message.sendState==='submitting'&&<span>Sending…</span>}
    {message.sendState==='persisted'&&<span>Sent</span>}
    {(message.sendState==='rejected'||message.sendState==='uncertain')&&<div role="status">{message.sendState==='uncertain'?'Not sure this sent':'Couldn\'t send'} <button onClick={()=>chat.retryMessage(message.clientMessageId!)}>Try again</button></div>}
   </article>)}
   {offset>0&&<button onClick={()=>setOffset(value=>Math.max(0,value-200))}>Later messages</button>}
  </div>
  <form className="flex gap-2 bg-surface-1 p-3" onSubmit={event=>{event.preventDefault();void submit();}}>
   <label className="sr-only" htmlFor="dm-composer">Message</label>
   <input id="dm-composer" ref={editor} value={current?input:''} onChange={event=>setInput(event.target.value)} disabled={!chat.isAuthenticated} maxLength={2000} className="flex-1 rounded-full bg-surface-2 px-4 py-2"/>
   <button disabled={!chat.isAuthenticated||!input.trim()}>Send</button>
  </form>
 </div>;
}
