import { describe, it, expect } from 'vitest';
import { ConversationState } from './conversation-state';
import type { PoolMessage } from '../../core/clients/chat-api';
const row = (sequence: number, revision='1'): PoolMessage => ({ id: `message-${sequence}`, conversationId: 'room', poolAddress: 'pool', sender: 'sender', content: `text-${sequence}`, replyToId: null, sequence: String(sequence), revision, createdAt: 1, deleted: false });
describe('conversation state used by connected chat hooks',()=>{
 it('recovers retained201..1000 in stable durable order with duplicate events',()=>{
  const state=new ConversationState();state.read('200');
  for(let sequence=1000;sequence>=201;sequence--)for(let replay=0;replay<10;replay++)state.apply(row(sequence));
  expect(state.messages.map(message=>message.sequence)).toEqual(Array.from({length:800},(_,i)=>String(i+201)));
  expect(state.unreadCount).toBe(800);state.read('1000');state.read('250');expect(state.unreadCount).toBe(0);
 });
 it('retains revision3 tombstone after delayed creation1 and edit2',()=>{
  const state=new ConversationState();state.apply({...row(1,'2'),content:'edit'});state.apply({...row(1,'3'),deleted:true});
  state.apply(row(1));expect(state.messages).toHaveLength(1);expect(state.messages[0]?.content).toBe('');expect(state.messages[0]?.deleted).toBe(true);expect(state.unreadCount).toBe(0);
 });
 it('resolves uncertain send by acknowledged ID without content-based merging',()=>{
  const state=new ConversationState();const pending={clientMessageId:'stable-client',target:'peer',kind:'dm' as const,content:'same body',replyToId:null,sender:'sender',createdAt:1,state:'submitting' as const};
  state.pending.set(pending.clientMessageId,pending);state.uncertain();expect(state.pending.get(pending.clientMessageId)?.state).toBe('uncertain');
  state.acknowledge(pending.clientMessageId,'message-1','1','1');state.apply(row(1));state.apply(row(1));expect(state.messages).toHaveLength(1);expect(state.pending.size).toBe(0);
  state.pending.set('another',{...pending,clientMessageId:'another'});state.apply(row(2));expect(state.pending.has('another')).toBe(true);
 });
 it('clears private pending text, tombstones, cursor and markers on wallet reset',()=>{
  const state=new ConversationState();state.apply(row(1));state.read('1');state.cursor='private-cursor';state.clear();expect(state.messages).toEqual([]);expect(state.pending.size).toBe(0);expect(state.cursor).toBe('');expect(state.readThrough).toBe(0n);
 });
 it('excludes own messages from unread counts and rejects malformed sequence markers',()=>{
  const state=new ConversationState('sender');state.apply(row(1));expect(state.unreadCount).toBe(0);expect(()=>state.read('-1')).toThrow();expect(()=>state.apply({...row(1),sequence:'bad'})).toThrow();
 });
});

describe('interrupted acknowledgement reconciliation', () => {
 it('replay alone resolves an uncertain bubble with its durable client message ID', () => {
  const state = new ConversationState('sender');
  state.pending.set('lost-response', {clientMessageId:'lost-response',target:'peer',kind:'dm',content:'retained draft',replyToId:null,sender:'sender',createdAt:1,state:'uncertain'});
  state.apply({...row(1),clientMessageId:'lost-response'});
  expect(state.pending.size).toBe(0);expect(state.messages).toHaveLength(1);
  state.acknowledge('lost-response','message-1','1','1');expect(state.messages).toHaveLength(1);
 });
 it('duplicate replay still reconciles a pending bubble after a snapshot race', () => {
  const state = new ConversationState();state.apply({...row(1),clientMessageId:'stable'});
  state.pending.set('stable',{clientMessageId:'stable',target:'peer',kind:'dm',content:'retained draft',replyToId:null,sender:'sender',createdAt:1,state:'uncertain'});
  state.apply({...row(1),clientMessageId:'stable'});expect(state.pending.size).toBe(0);expect(state.messages).toHaveLength(1);
 });
 it('rejects identity changes across acknowledgements', () => {
  const state = new ConversationState();state.pending.set('stable',{clientMessageId:'stable',target:'peer',kind:'dm',content:'retained draft',replyToId:null,sender:'sender',createdAt:1,state:'uncertain'});
  state.acknowledge('stable','message-1','1','1');
  expect(()=>state.acknowledge('stable','message-2','2','1')).toThrow('identity changed');
 });
 it('cannot resurrect a tombstone with a conflicting equal revision', () => {
  const state = new ConversationState();state.apply({...row(1,'3'),deleted:true});state.apply({...row(1,'3'),content:'resurrection'});
  expect(state.messages[0]?.deleted).toBe(true);expect(state.messages[0]?.content).toBe('');expect(state.unreadCount).toBe(0);
 });
});

it('a peer cannot resolve a sender pending bubble by reusing its scoped client ID',()=>{
 const state=new ConversationState('sender');state.pending.set('stable',{clientMessageId:'stable',target:'peer',kind:'dm',content:'retained draft',replyToId:null,sender:'sender',createdAt:1,state:'uncertain'});
 state.apply({...row(1),sender:'peer',clientMessageId:'stable'});expect(state.pending.size).toBe(1);
 state.apply({...row(2),clientMessageId:'stable'});expect(state.pending.size).toBe(0);
});
