'use client';
import {useEffect} from 'react';
import {useQuery,useQueryClient} from '@tanstack/react-query';
import {accountSession,subscribeAccount} from './account-transport';
import {useAccountSession} from './use-account-session';
import {retryTransientOnly} from '@/core/transient-retry';
export function usePrivateAccountQuery<T>(name:string,load:()=>Promise<T>,selection:readonly string[]=[],wallet?:string) {
 const {session}=useAccountSession();const client=useQueryClient();
 const owner=!!session&&(!wallet||session.session.wallet.toLowerCase()===wallet.toLowerCase());
 const identity=owner&&session?[session.session.sessionId,session.session.wallet.toLowerCase(),'125',String(session.epoch)]:[];
 useEffect(()=>subscribeAccount(()=>{const scope=accountSession();void client.cancelQueries({predicate:q=>q.queryKey[0]==='private-account'&&(q.queryKey[1]!==scope?.session.sessionId||q.queryKey[4]!==String(scope?.epoch))});client.removeQueries({predicate:q=>q.queryKey[0]==='private-account'&&(q.queryKey[1]!==scope?.session.sessionId||q.queryKey[4]!==String(scope?.epoch))});}),[client]);
 const query=useQuery({queryKey:['private-account',...identity,name,...selection],queryFn:load,enabled:owner,...retryTransientOnly,staleTime:15000,refetchOnWindowFocus:true,refetchOnReconnect:true});
 return {...query,data:owner?query.data:undefined,owner,session};
}
