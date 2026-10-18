import {expect,it} from 'vitest';
import {renderToStaticMarkup} from 'react-dom/server';
import {MetadataView} from './viewer';
import type {MetadataV2Data} from '@/core/clients/metadata';
it('keeps authoritative token address and trading visible during metadata outage',()=>{
 const token='0x'+'1'.repeat(40);
 const html=renderToStaticMarkup(<MetadataView tokenAddress={token} tradePanel={<button>Trade token</button>} data={null} pending={false} error={true}/>);
 expect(html).toContain(token);expect(html).toContain('Trade token');expect(html).toContain('load coin details right now, you can still trade');expect(html).toContain('role="alert"');
});
it('renders creator text inert and distinguishes onchain provenance',()=>{
 const data:MetadataV2Data={token:{address:'0x'+'1'.repeat(40),name:'Canonical token',symbol:'CAN',decimals:6,decimalsQuality:'verified',totalSupplyRaw:'1000000',supplyQuality:'verified',metadataRevision:'8',logoAssetId:null,sourcePosition:{chainId:'125',height:'1',blockHash:'0x'+'a'.repeat(64),transactionHash:'0x'+'b'.repeat(64),transactionIndex:'0',logIndex:'0'}},displayName:'<script>alert(1)</script>',displaySymbol:null,description:'<img src=x onerror=alert(1)>',tags:[],socialLinks:[],revision:'8'};
 const html=renderToStaticMarkup(<MetadataView tokenAddress={data.token.address} tradePanel={<button>Trade</button>} data={data} pending={false} error={false}/>);
 expect(html).not.toContain('<script>');expect(html).not.toContain('<img src=x');expect(html).toContain('&lt;script&gt;');expect(html).toContain('Onchain name:');expect(html).toContain('Canonical token');expect(html).not.toContain('revision');
});

it('filters unsafe response links at the rendering boundary',()=>{
 const data:MetadataV2Data={token:{address:'0x'+'1'.repeat(40),name:'Canonical token',symbol:'CAN',decimals:6,decimalsQuality:'verified',totalSupplyRaw:'1000000',supplyQuality:'verified',metadataRevision:'8',logoAssetId:null,sourcePosition:{chainId:'125',height:'1',blockHash:'0x'+'a'.repeat(64),transactionHash:'0x'+'b'.repeat(64),transactionIndex:'0',logIndex:'0'}},displayName:null,displaySymbol:null,description:'',tags:[],socialLinks:[{label:'unsafe',url:'javascript:alert(1)'},{label:'credentials',url:'https://user:password@example.com'},{label:'safe',url:'https://example.com'}],revision:'8'};
 const html=renderToStaticMarkup(<MetadataView tokenAddress={data.token.address} tradePanel={<button>Trade</button>} data={data} pending={false} error={false}/>);
 expect(html).not.toContain('javascript:');expect(html).not.toContain('user:password');expect(html).toContain('https://example.com');
});
