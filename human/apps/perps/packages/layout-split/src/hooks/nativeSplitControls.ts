import {useSyncExternalStore} from "react";
export type NativeSplitControls={layout:"left"|"right";marketLayout:"left"|"top"|"bottom"|"hide";setLayout:(layout:"left"|"right")=>void;setMarketLayout:(layout:"left"|"top"|"bottom"|"hide")=>void};
let controls:NativeSplitControls|null=null;const listeners=new Set<()=>void>();
export function publishNativeSplitControls(value:NativeSplitControls|null){controls=value;listeners.forEach(listener=>listener());}
export function useNativeSplitControls(){return useSyncExternalStore(listener=>{listeners.add(listener);return()=>{listeners.delete(listener);};},()=>controls,()=>null);}
