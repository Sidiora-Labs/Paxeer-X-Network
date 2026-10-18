/** Optional exchange analytics is disabled for the native paxeer-network application. */
const disabled = (..._args:any[])=>{};
export const useTrack=()=>({track:disabled,identify:disabled,setTrackUserId:disabled,tracking:disabled,setIdentify:disabled});
