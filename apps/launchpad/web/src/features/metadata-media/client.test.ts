import {afterEach,describe,it,expect,vi} from 'vitest';
vi.mock('@/core/sdk-config',()=>({metadataApiUrl:(p:string)=>'https://metadata.example'+p}));
vi.mock('@/core/report-error',()=>({reportError:()=>{}}));
import {fetchTokenMetadata,fetchTokenMetadataBatch} from '@/core/clients/metadata';
afterEach(()=>vi.unstubAllGlobals());
describe('metadata availability',()=>{
 it('only404 becomes absent',async()=>{vi.stubGlobal('fetch',vi.fn(async()=>new Response('',{status:404})));expect(await fetchTokenMetadata('0x'+'1'.repeat(40))).toBe(null)});
 it('503 remains rejected rather than null',async()=>{vi.stubGlobal('fetch',vi.fn(async()=>new Response('',{status:503})));await expect(fetchTokenMetadata('0x'+'2'.repeat(40))).rejects.toThrow('Metadata unavailable')});
 it('batch outage does not fan out or manufacture null',async()=>{const fetcher=vi.fn(async()=>new Response('',{status:503}));vi.stubGlobal('fetch',fetcher);await expect(fetchTokenMetadataBatch(['0x'+'3'.repeat(40)])).rejects.toThrow();expect(fetcher).toHaveBeenCalledTimes(1)});
});

it('rejects incomplete200batch',async()=>{vi.stubGlobal('fetch',vi.fn(async()=>new Response('{}',{status:200})));await expect(fetchTokenMetadataBatch(['0x'+'4'.repeat(40)])).rejects.toThrow()});
