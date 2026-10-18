import { useState, useCallback } from "react";
import type { SWRMutationConfiguration } from "swr/mutation";
import { nativeRequest } from "@paxeer-network/paxeer-backend/nativeClient";
export function useMutation<T,E>(url:string,method:"POST"|"PUT"|"DELETE"|"GET"="POST",options?:SWRMutationConfiguration<T,E>) {
  const [data,setData]=useState<T>();const [error,setError]=useState<E>();const [isMutating,setMutating]=useState(false);
  const mutation=useCallback(async (body:Record<string,any>|null,params?:Record<string,any>,config?:SWRMutationConfiguration<T,E>) => {
    setMutating(true);setError(undefined);
    try {
      const query=params?new URLSearchParams(params).toString():"";
      const result=await nativeRequest(url+(query?`${url.includes("?")?"&":"?"}${query}`:""),{method,...(body?{body:JSON.stringify(body)}:{})});
      setData(result as T);(config?.onSuccess??options?.onSuccess)?.(result as T,url,config??options as any);return result;
    } catch(value) {setError(value as E);throw value;} finally {setMutating(false);}
  },[url,method,options]);
  return [mutation,{data,error,isMutating,reset:()=>{setData(undefined);setError(undefined);}}] as const;
}
