import type { FastifyInstance, FastifyRequest } from 'fastify';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { verifyAccessToken } from '../auth/jwt.js';
import { privateKeyToAccount } from 'viem/accounts';
import { encodeFunctionData, parseAbi, recoverAddress, serializeSignature, type Hex } from 'viem';
import { z } from 'zod';
import { SponsoredConstruction } from './sign.js';
import { constructionDigest, sponsorQuoteDigest } from '../attestor/client.js';
import { requireAuth } from '../middleware/auth.js';
import {
  WalletArchivedError,
  archivedWalletGuard,
  findWalletByUserId,
  provisionWalletForUser,
} from '../db/wallets.js';
import { getPool } from '../db/pool.js';
import { env } from '../env.js';
import { sharedRpcPool } from '../rpc/pool.js';
import { sharedNonceStore } from '../nonce/store.js';
import { AttestorRefusal, AttestorUnavailable, attestorDaemonFromConfig, mainAccountId, readKernelAvailability } from '../provision/bind.js';
import { ProvisionError, ProvisionRefusedError, provisionAccount, produceIdentityBinding, type ProvisionDeps } from '../provision/state.js';

export interface WalletRoutesOptions {
  provision?: ProvisionDeps | null;
}

let sharedDeps: ProvisionDeps | null | undefined;

export function provisionDepsFromEnv(): ProvisionDeps | null {
  if (sharedDeps !== undefined) return sharedDeps;
  const attestors = attestorDaemonFromConfig(env);
  if (!attestors) {
    sharedDeps = null;
    return sharedDeps;
  }
  const sponsor = env.SPONSOR_PRIVATE_KEY_FILE
    ? privateKeyToAccount(`0x${readFileSync(env.SPONSOR_PRIVATE_KEY_FILE, 'utf8').trim().replace(/^0x/, '')}`)
    : null;
  sharedDeps = {
    pool: getPool(),
    rpc: sharedRpcPool(),
    attestors,
    nonces: sharedNonceStore(),
    sponsor,
    chainId: env.HYPERPAXEER_CHAIN_ID,
    gasCapWei: env.ACCOUNT_SETUP_GAS_CAP_WEI,
    receiptTimeoutMs: 60_000,
    receiptPollMs: 250,
    ...(env.WALLET_IDENTITY_BINDING_TENANT && env.WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE ? {
      identityBinding: {
        issuer: `${env.SUPABASE_URL.replace(/\/$/, '')}/auth/v1`,
        tenant: env.WALLET_IDENTITY_BINDING_TENANT,
        privateKeyFile: env.WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE,
      },
    } : {}),
  };
  return sharedDeps;
}

function bearerToken(req: FastifyRequest): string {
  return (req.headers.authorization ?? '').replace(/^Bearer\s+/i, '').trim();
}

interface UnifiedColumns {
  did: string | null;
  main_account_id: string | null;
  binding_state: string;
}

async function unifiedColumns(walletId: string): Promise<UnifiedColumns> {
  const { rows } = await getPool().query<UnifiedColumns>(
    `select did, main_account_id, binding_state from wallets where id = $1`,
    [walletId],
  );
  return rows[0] ?? { did: null, main_account_id: null, binding_state: 'unbound' };
}

export async function walletRoutes(app: FastifyInstance, opts: WalletRoutesOptions = {}): Promise<void> {
  const deps = (): ProvisionDeps | null => (opts.provision !== undefined ? opts.provision : provisionDepsFromEnv());

  app.post('/v1/wallet/provision', { preHandler: requireAuth }, async (req, reply) => {
    const userId = req.user!.id;
    const provision = deps();
    try {
      if (!provision) {
        if (env.WALLET_IDENTITY_BINDING_TENANT) {
          throw new ProvisionError('attestors_unconfigured', 503, 'identity provisioning requires configured attestors');
        }
        const { wallet } = await provisionWalletForUser(userId, 'standard');
        return reply.send({ wallet, identityBinding: null });
      }
      const out = await provisionAccount(provision, { kind: 'standard', userId, token: bearerToken(req) });
      const wallet = await findWalletByUserId(userId);
      const identityBinding = await produceIdentityBinding(provision, userId, out);
      return reply.send({
        identityBinding,
        wallet: {
          id: out.walletId,
          address: out.address,
          chain_id: wallet?.chain_id ?? env.HYPERPAXEER_CHAIN_ID,
          kind: 'standard',
          created_at: wallet?.created_at ?? null,
          last_used_at: wallet?.last_used_at ?? null,
          did: out.did,
          main_account_id: out.mainAccountId,
          binding_state: out.state === 'active' ? 'bound' : 'pending',
        },
        provisioning: { state: out.state, awaiting: out.awaiting },
      });
    } catch (err) {
      if (err instanceof WalletArchivedError) return reply.code(err.refusal.status).send(err.refusal.body);
      if (err instanceof ProvisionRefusedError) {
        return reply.code(409).send({ error: 'binding_refused', message: err.message, bound_did: err.boundDid });
      }
      if (err instanceof ProvisionError) {
        return reply.code(err.status).send({ error: err.code, message: err.message });
      }
      if (err instanceof AttestorRefusal) {
        return reply.code(err.status === 401 || err.status === 403 ? err.status : 502).send({
          error: 'attestor_refused',
          category: err.category,
          code: err.code,
        });
      }
      if (err instanceof AttestorUnavailable) {
        return reply.code(503).send({ error: 'attestor_unavailable', message: err.message });
      }
      req.log.error({ err, userId }, 'wallet provision failed');
      return reply
        .code(500)
        .send({ error: 'wallet_provision_failed', detail: (err as Error).message });
    }
  });

  app.get('/v1/wallet/me', { preHandler: requireAuth }, async (req, reply) => {
    const userId = req.user!.id;
    const wallet = await findWalletByUserId(userId);
    if (!wallet) {
      const refusal = await archivedWalletGuard({ userId, kind: 'standard' });
      if (refusal) return reply.code(refusal.status).send(refusal.body);
      return reply.code(404).send({ error: 'no_wallet', message: 'wallet not provisioned' });
    }
    const unified = await unifiedColumns(wallet.id);
    const provision = deps();
    const kernel = await readKernelAvailability(provision ? provision.rpc : sharedRpcPool());
    let identityBinding: string | null = null;
    let capsContext: Record<string, unknown> | null = null;
    if (provision?.identityBinding && unified.binding_state === 'bound' && unified.did &&
        unified.main_account_id === mainAccountId(unified.did)) {
      identityBinding = await produceIdentityBinding(provision, userId, {
        state: 'active', awaiting: null, walletId: wallet.id, address: wallet.address as `0x${string}`,
        did: unified.did, mainAccountId: unified.main_account_id, bindMessage: null,
      });
      const assertion = bearerToken(req);
      const claims = await verifyAccessToken(assertion);
      if (identityBinding && claims.sub === userId && Number.isSafeInteger(claims.exp) && claims.exp > Math.floor(Date.now() / 1000)) {
        capsContext = { subject: userId, did: unified.did, account_id: unified.main_account_id,
          address: wallet.address.toLowerCase(), chain_id: wallet.chain_id,
          tenant: provision.identityBinding.tenant, expires_at: String(claims.exp),
          session_id: createHash('sha256').update('LXP/wallet-caps/session/v1\0').update(assertion).digest('hex') };
      }
    }
    return reply.header('Cache-Control', 'no-store').send({
      identityBinding, capsContext,
      wallet: {
        id: wallet.id,
        address: wallet.address,
        chain_id: wallet.chain_id,
        created_at: wallet.created_at,
        last_used_at: wallet.last_used_at,
        did: unified.did,
        main_account_id: unified.main_account_id,
        binding_state: unified.binding_state,
      },
      chain: {
        id: env.HYPERPAXEER_CHAIN_ID,
        rpc_url: env.HYPERPAXEER_RPC_URL,
        explorer_url: env.HYPERPAXEER_EXPLORER_URL ?? null,
      },
      kernel,
    });
  });
}

const SponsorAddress=z.string().regex(/^0x[0-9a-fA-F]{40}$/);
const SponsorUint=z.string().max(78).regex(/^(0|[1-9][0-9]*)$/).refine(value=>BigInt(value)<1n<<256n);
const SponsorSignature=z.string().regex(/^0x[0-9a-fA-F]{130}$/);
const SponsorAuthorization=z.object({chainId:SponsorUint,address:SponsorAddress,nonce:SponsorUint.refine(value=>BigInt(value)<(1n<<64n)-1n),yParity:z.union([z.literal(0),z.literal(1)]),r:z.string().regex(/^0x[0-9a-fA-F]{64}$/),s:z.string().regex(/^0x[0-9a-fA-F]{64}$/)}).strict();
export const SponsoredSubmitBody=z.object({chain_id:SponsorUint,account:SponsorAddress,to:SponsorAddress,data:z.string().max(131074).regex(/^0x(?:[0-9a-fA-F]{2})*$/),value:SponsorUint,construction:SponsoredConstruction,account_signature:SponsorSignature,relayer_signature:SponsorSignature,authorization:SponsorAuthorization,quote_decimals:z.literal(6)}).strict();
export const SponsoredStatusBody=z.object({account:SponsorAddress,sponsor:SponsorAddress,quoteNonce:SponsorUint,relayerSignature:SponsorSignature}).strict();
const executeSponsoredAbi=parseAbi(['function executeSponsored((address to,uint256 value,bytes data)[] calls,(address sponsor,address token,uint256 maxTokenAmount,uint256 tokenAmount,uint256 deadline,uint256 quoteNonce,uint256 gasCost) quote,bytes accountSignature,bytes relayerSignature)']);
class StationAdapterError extends Error { constructor(readonly status:number,readonly code:string){super(code);} }
export interface WalletSponsorRoutesOptions { stationUrl?:string; }
export async function walletSponsorRoutes(app:FastifyInstance,opts:WalletSponsorRoutesOptions={}):Promise<void>{
  const configured=opts.stationUrl??process.env.WALLET_GAS_STATION_URL;
  async function station(path:'submit'|'status',body:unknown):Promise<Record<string,unknown>>{
    if(!configured) throw new StationAdapterError(503,'gas_station_unconfigured');
    let url:URL;try{url=new URL(configured);}catch{throw new StationAdapterError(503,'gas_station_configuration');}
    if(!['https:','http:'].includes(url.protocol)||url.username||url.password||url.search||url.hash) throw new StationAdapterError(503,'gas_station_configuration');
    url.pathname=`${url.pathname.replace(/\/(quote|submit|status)\/?$/,'').replace(/\/$/,'')}/${path}`;
    let response:Response;
    try{response=await fetch(url,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(body),redirect:'error',signal:AbortSignal.timeout(15_000)});}catch{throw new StationAdapterError(503,'gas_station_unavailable');}
    const reader=response.body?.getReader();if(!reader) throw new StationAdapterError(502,'gas_station_invalid_response');
    let text='';let length=0;const decoder=new TextDecoder('utf-8',{fatal:true});
    try{for(;;){const chunk=await reader.read();if(chunk.done)break;length+=chunk.value.length;if(length>65_536){await reader.cancel();throw new StationAdapterError(502,'gas_station_invalid_response');}text+=decoder.decode(chunk.value,{stream:true});}text+=decoder.decode();}finally{reader.releaseLock();}
    let parsed:unknown;try{parsed=JSON.parse(text);}catch{throw new StationAdapterError(502,'gas_station_invalid_response');}
    if(!parsed||typeof parsed!=='object'||Array.isArray(parsed)) throw new StationAdapterError(502,'gas_station_invalid_response');
    if(!response.ok) throw new StationAdapterError(response.status>=400&&response.status<500?response.status:503,typeof (parsed as Record<string,unknown>).error==='string'?String((parsed as Record<string,unknown>).error):'gas_station_refused');
    return parsed as Record<string,unknown>;
  }
  async function owned(req:FastifyRequest,account:string,readOnly=false){
    const wallet=await findWalletByUserId(req.user!.id);
    if(!wallet||wallet.address.toLowerCase()!==account.toLowerCase()||wallet.chain_id!==env.HYPERPAXEER_CHAIN_ID||(!readOnly&&wallet.is_disabled)) throw new StationAdapterError(403,'wallet_owner_mismatch');
    return wallet;
  }
  function statusBody(report:Record<string,unknown>,fallback:Hex|null=null){
    const decimal=(v:unknown):boolean=>typeof v==='string'&&v.length<=78&&/^(0|[1-9][0-9]*)$/.test(v)&&BigInt(v)<(1n<<256n);
    const transaction=(v:unknown):boolean=>v===null||(typeof v==='object'&&v!==null&&!Array.isArray(v)&&decimal((v as Record<string,unknown>).sponsorNonce)&&typeof (v as Record<string,unknown>).transactionHash==='string'&&/^0x[0-9a-fA-F]{64}$/.test(String((v as Record<string,unknown>).transactionHash)));
    const completion=report.completion as Record<string,unknown>|null;
    if(!decimal(report.deadline)||!transaction(report.submission)||!transaction(report.replacement)||(report.state==='completed')!==(completion!==null)||((report.state==='pending'||report.state==='replacing')&&report.submission===null)||(report.state==='replacing'&&report.replacement===null)) throw new StationAdapterError(502,'gas_station_invalid_response');
    if(completion!==null){
      if(typeof completion!=='object'||Array.isArray(completion)||!['consumed','included','reverted','cancelled'].includes(String(completion.outcome))) throw new StationAdapterError(502,'gas_station_invalid_response');
      if(completion.outcome!=='consumed'&&(typeof completion.transactionHash!=='string'||!/^0x[0-9a-fA-F]{64}$/.test(completion.transactionHash))) throw new StationAdapterError(502,'gas_station_invalid_response');
      if((completion.outcome==='included'||completion.outcome==='cancelled')&&!decimal(completion.blockNumber)) throw new StationAdapterError(502,'gas_station_invalid_response');
      if(completion.outcome==='included'&&(!decimal(completion.sidCollected)||!decimal(completion.paxSpent))) throw new StationAdapterError(502,'gas_station_invalid_response');
    }
    const submission=(report.replacement??report.submission) as Record<string,unknown>|null;
    const hash=typeof completion?.transactionHash==='string'?completion.transactionHash:typeof submission?.transactionHash==='string'?submission.transactionHash:fallback;
    if(hash!==null&&(typeof hash!=='string'||!/^0x[0-9a-fA-F]{64}$/.test(hash))) throw new StationAdapterError(502,'gas_station_invalid_response');
    if(!['quoted','pending','replacing','completed'].includes(String(report.state))) throw new StationAdapterError(502,'gas_station_invalid_response');
    const included=report.state==='completed'&&completion?.outcome==='included'&&typeof completion.blockNumber==='string'&&/^(0|[1-9][0-9]*)$/.test(completion.blockNumber)&&typeof completion.transactionHash==='string'&&/^0x[0-9a-fA-F]{64}$/.test(completion.transactionHash);
    const status=included?'confirmed':completion?.outcome==='reverted'?'reverted':completion?.outcome==='cancelled'?'cancelled':'pending';
    return {tx_hash:hash,status,station:report};
  }
  app.post('/v1/wallet/sponsored/status',{preHandler:requireAuth},async(req,reply)=>{
    const parsed=SponsoredStatusBody.safeParse(req.body);if(!parsed.success)return reply.code(400).send({error:'invalid_body',issues:parsed.error.issues});
    try{await owned(req,parsed.data.account,true);return reply.send(statusBody(await station('status',parsed.data)));}
    catch(error){if(error instanceof StationAdapterError)return reply.code(error.status).send({error:error.code,status:'pending'});throw error;}
  });
  app.post('/v1/wallet/sponsored/submit',{preHandler:requireAuth},async(req,reply)=>{
    const parsed=SponsoredSubmitBody.safeParse(req.body);if(!parsed.success)return reply.code(400).send({error:'invalid_body',issues:parsed.error.issues});
    const body=parsed.data;const c=body.construction;const q=c.quote;
    try{
      const wallet=await owned(req,body.account);
      if(BigInt(body.chain_id)!==BigInt(wallet.chain_id)||body.chain_id!==c.chainId||body.authorization.chainId!==body.chain_id||c.account.toLowerCase()!==wallet.address.toLowerCase()||body.to.toLowerCase()!==wallet.address.toLowerCase()||body.value!=='0'||BigInt(q.tokenAmount)>BigInt(q.maxTokenAmount)||q.sponsor.toLowerCase()===wallet.address.toLowerCase()) throw new StationAdapterError(403,'sponsored_construction_mismatch');
      if((await recoverAddress({hash:constructionDigest(c),signature:body.account_signature as Hex})).toLowerCase()!==wallet.address.toLowerCase()||(await recoverAddress({hash:sponsorQuoteDigest(c),signature:body.relayer_signature as Hex})).toLowerCase()!==q.sponsor.toLowerCase()) throw new StationAdapterError(403,'sponsored_signature_mismatch');
      const authorization=body.authorization;
      const authSignature=serializeSignature({r:authorization.r as Hex,s:authorization.s as Hex,yParity:authorization.yParity});
      if((await recoverAddress({hash:constructionDigest({kind:'eip7702_authorization',chainId:authorization.chainId,address:authorization.address,nonce:authorization.nonce}),signature:authSignature})).toLowerCase()!==wallet.address.toLowerCase()) throw new StationAdapterError(403,'authorization_owner_mismatch');
      const expected=encodeFunctionData({abi:executeSponsoredAbi,functionName:'executeSponsored',args:[c.calls.map(call=>({to:call.to as Hex,value:BigInt(call.value),data:call.data as Hex})),{sponsor:q.sponsor as Hex,token:q.token as Hex,maxTokenAmount:BigInt(q.maxTokenAmount),tokenAmount:BigInt(q.tokenAmount),deadline:BigInt(q.deadline),quoteNonce:BigInt(q.quoteNonce),gasCost:BigInt(q.gasCost)},body.account_signature as Hex,body.relayer_signature as Hex]});
      if(expected.toLowerCase()!==body.data.toLowerCase()) throw new StationAdapterError(403,'sponsored_call_changed');
      const identity={account:body.account,sponsor:q.sponsor,quoteNonce:q.quoteNonce,relayerSignature:body.relayer_signature};
      const stationBody={call:{to:body.to,value:body.value,data:body.data},authorization,batch:{chainId:c.chainId,account:c.account,nonce:c.nonce,calls:c.calls,quote:{...q,decimals:body.quote_decimals}},accountSignature:body.account_signature,relayerSignature:body.relayer_signature};
      let hash:Hex|null=null;
      try{
        const submitted=await station('submit',stationBody);
        if(typeof submitted.transactionHash!=='string'||!/^0x[0-9a-fA-F]{64}$/.test(submitted.transactionHash)) throw new StationAdapterError(502,'gas_station_invalid_response');
        hash=submitted.transactionHash as Hex;
      }catch(error){
        if(!(error instanceof StationAdapterError)||error.status<500) throw error;
        try{return reply.send(statusBody(await station('status',identity)));}catch{throw error;}
      }
      try{return reply.send(statusBody(await station('status',identity),hash));}
      catch(error){if(error instanceof StationAdapterError)return reply.send({tx_hash:hash,status:'pending',station:null});throw error;}
    }catch(error){
      if(error instanceof StationAdapterError)return reply.code(error.status).send({error:error.code,status:'pending'});
      return reply.code(403).send({error:'sponsored_construction_invalid',status:'pending'});
    }
  });
}
