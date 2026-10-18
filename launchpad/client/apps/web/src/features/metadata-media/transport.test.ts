import {expect,it} from 'vitest';
import {fingerprintCreatorImage,createMetadataImageTransport} from './transport';
it('uses the actual byte digest as retry identity',async()=>{
 const a=new File([new Uint8Array([97,98,99])],'first.png',{type:'image/png'});
 const b=new File([new Uint8Array([97,98,99])],'other.png',{type:'image/png'});
 expect(await fingerprintCreatorImage(a)).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');expect(await fingerprintCreatorImage(b)).toBe(await fingerprintCreatorImage(a));
});
it('rejects active image formats before requesting authentication or sending bytes',async()=>{
 await expect(fingerprintCreatorImage(new File(['<svg/>'],'x.png',{type:'image/svg+xml'}))).rejects.toThrow('Choose JPEG, PNG or WebP');
 await expect(fingerprintCreatorImage(new File([new Uint8Array(6291457)],'large.png',{type:'image/png'}))).rejects.toThrow('up to 6 MiB');
});

it('rejects unbounded revision before any image operation begins',()=>{
 const authority={tokenAddress:'0x'+'1'.repeat(40),wallet:'0x'+'2'.repeat(40),audience:'https://metadata.example',revision:'9'.repeat(79),authenticationHeaders:async()=>({}),signMessage:async()=>''};
 expect(()=>createMetadataImageTransport(authority)).toThrow('Invalid creator identity.');
});
it('cancelled upload fails before reading selected bytes or authenticating',async()=>{
 const authority={tokenAddress:'0x'+'1'.repeat(40),wallet:'0x'+'2'.repeat(40),audience:'https://metadata.example',revision:'7',authenticationHeaders:async()=>{throw new Error('Authentication must not run')},signMessage:async()=>{throw new Error('Signing must not run')}};
 const upload=createMetadataImageTransport(authority);const controller=new AbortController();controller.abort();
 await expect(upload(new File(['png'],'image.png',{type:'image/png'}),'11111111-1111-1111-1111-111111111111',controller.signal)).rejects.toMatchObject({name:'AbortError'});
});
