import {useSymbolStore} from "./provider/store/symbolStore";
export const usePreLoadData=()=>{const symbols=useSymbolStore(state=>state.data);return {error:null,done:Boolean(symbols?.length)};};
