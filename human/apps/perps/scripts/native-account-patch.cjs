const ts=require("typescript"),fs=require("fs");const file="packages/core/src/account.ts";let source=fs.readFileSync(file,"utf8");
const replacements={
 setAddress:`{ if (!address || !wallet?.provider) throw new SDKError("Connect a Paxeer wallet first.");
 const chainId=this.parseChainId(wallet.chain.id);this.keyStore.setAddress(address);
 this.walletAdapterManager.switchWallet(wallet.chain.namespace,address,chainId,{provider:wallet.provider,contractManager:this.contractManger});
 const nextState:AccountState={status:AccountStatusEnum.EnableTrading,validating:false,address,accountId:address,mainAccountId:address,isNew:false,chainNamespace:wallet.chain.namespace,connectWallet:{name:wallet.wallet.name,chainId},subAccounts:[]};
 this.updateState(nextState);this._ee.emit(EVENT_NAMES.statusChanged,nextState);return nextState.status; }`,
 createAccount:`{ if(!this.address) throw new SDKError("Connect your Paxeer wallet first.");return this.accountId; }`,
 createPaxeerNetworkKey:`{ throw new SDKError("paxeer-network signs contract transactions with the connected wallet; a separate trading key is not required."); }`,
 restoreSubAccount:`{ return this.stateValue; }`,
 getSubAccounts:`{ return []; }`,
 getSubAccountBalances:`{ return {}; }`,
 refreshSubAccountBalances:`{ return {}; }`,
 switchAccount:`{ if(accountId!==this.accountId) throw new SDKError("Subaccounts are not available on the configured Paxeer contracts.");return this.stateValue; }`,
 disconnect:`{ this.updateState({status:AccountStatusEnum.NotConnected,validating:false});this._ee.emit(EVENT_NAMES.statusChanged,this.stateValue); }`,
 settle:`{ throw new SDKError("Paxeer position PnL settles through the position contract when a position is closed."); }`,
};
const sf=ts.createSourceFile(file,source,ts.ScriptTarget.Latest,true);const edits=[];
function visit(node){if(ts.isMethodDeclaration(node)&&node.body&&replacements[node.name.getText(sf)])edits.push([node.body.getStart(sf),node.body.end,replacements[node.name.getText(sf)]]);ts.forEachChild(node,visit)}visit(sf);
for(const [start,end,text] of edits.sort((a,b)=>b[0]-a[0]))source=source.slice(0,start)+text+source.slice(end);
fs.writeFileSync(file,source);
