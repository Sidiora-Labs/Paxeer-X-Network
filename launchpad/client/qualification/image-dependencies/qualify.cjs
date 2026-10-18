const assert = require('node:assert/strict');
const { createRequire } = require('node:module');
const { createServer } = require('node:http');
const { once } = require('node:events');
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const appRequire = createRequire(path.resolve(__dirname, '../../apps/web/package.json'));
const nextRequire = createRequire(appRequire.resolve('next/package.json'));
const sharp = nextRequire('sharp');
const storageRequire = createRequire(appRequire.resolve('@react-native-async-storage/async-storage/package.json'));
const nativeRequire = createRequire(storageRequire.resolve('react-native/package.json'));
const cliRequire = createRequire(nativeRequire.resolve('@react-native/community-cli-plugin/package.json'));
const metroRequire = createRequire(cliRequire.resolve('metro/package.json'));
const imageSize = metroRequire('image-size');
const metroAssets = metroRequire('./src/Assets.js');
const checks = [];
async function check(name, fn) { await fn(); checks.push(name); console.log('PASS', name); }
async function freePort() { const s=createServer();s.listen(0,'127.0.0.1');await once(s,'listening');const p=s.address().port;await new Promise(r=>s.close(r));return p; }
(async () => {
  console.log(JSON.stringify({ node:process.version,platform:process.platform,arch:process.arch,sharp:sharp.versions,imageSizePath:metroRequire.resolve('image-size'), imageSizeExports:Object.keys(imageSize) }));
  assert.equal(sharp.versions.sharp,'0.35.5');
  assert.match(metroRequire.resolve('image-size'), /image-size@2\.0\.3/);
  const fixtures={};
  for(const format of ['png','jpeg','webp','avif']) fixtures[format]=await sharp({create:{width:64,height:48,channels:3,background:{r:120,g:80,b:40}}}).toFormat(format).toBuffer();
  for(const [format,buffer] of Object.entries(fixtures)) await check(`native ${format} metadata/resize`,async()=>{
    const m=await sharp(buffer).metadata();assert.equal(m.width,64);assert.equal(m.height,48);
    const result=await sharp(buffer).rotate().resize(32).toFormat(format).toBuffer();const r=await sharp(result).metadata();assert.equal(r.width,32);assert.equal(r.height,24);
    const size=imageSize.default(buffer);assert.equal(size.width,64);assert.equal(size.height,48);
    if(format!=='avif') assert.deepEqual(metroAssets.getAssetSize(format,buffer,`fixture.${format}`),{width:64,height:48});
  });
  await check('Metro real filesystem getAssetData preserves raster dimensions',async()=>{
    const dir=fs.mkdtempSync(path.join(__dirname,'metro-assets-'));
    try {
      for(const format of ['png','jpeg','webp']) {
        const file=path.join(dir,`fixture.${format}`);fs.writeFileSync(file,fixtures[format]);
        const data=await metroAssets.getAssetData(file,`fixture.${format}`,[],null,'/assets');
        assert.equal(data.width,64);assert.equal(data.height,48);assert.equal(data.files[0],file);
      }
    } finally {fs.rmSync(dir,{recursive:true});}
  });
  await check('malformed native and Metro inputs reject',async()=>{
    await assert.rejects(sharp(Buffer.from('invalid')).metadata());
    assert.throws(()=>metroAssets.getAssetSize('png',Buffer.from('invalid'),'bad.png'));
    assert.throws(()=>metroAssets.getAssetSize('png',Buffer.alloc(0),'empty.png'));
  });
  await check('patched malformed ICNS/JXL/HEIF parsing terminates in bounded child',async()=>{
    const fixturePaths=['heif-undersized-ispe.heic','icns-zero-length-entry.icns','jxl-header-only-jxlp.jxl'].map(name=>path.join(__dirname,'invalid',name));
    const code = `const assert=require('node:assert/strict');const fs=require('node:fs');const size=require(${JSON.stringify(metroRequire.resolve('image-size'))}).default;for(const file of ${JSON.stringify(fixturePaths)}) { const bytes=fs.readFileSync(file); assert.throws(()=>size(bytes)); }`;
    const result=spawnSync(process.execPath,['-e',code],{timeout:2000,encoding:'utf8'});assert.equal(result.status,0,result.stderr);assert.equal(result.error,undefined);
  });
  const giant=await sharp({create:{width:5000,height:5000,channels:3,background:'white'}}).png().toBuffer();
  await check('native pixel bounds reject',async()=>{await assert.rejects(sharp(giant,{limitInputPixels:16777216}).resize(32).toBuffer(),/pixel limit/);});
  const svg=Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="64" height="48"><rect width="64" height="48" fill="red"/></svg>');
  await check('patched SVG native decode works',async()=>{assert.equal((await sharp(svg).png().toBuffer({resolveWithObject:true})).info.width,64);});
  const origin=createServer((req,res)=>{
    res.setHeader('Cache-Control','public,max-age=60');
    const name=(req.url||'').split('/').pop();
    if(name==='redirect-denied'){res.writeHead(302,{Location:'/denied/png'});return res.end();}
    if(name==='redirect'){res.writeHead(302,{Location:'/allowed/png'});return res.end();}
    if(name==='oversize'){res.setHeader('Content-Type','image/png');for(let i=0;i<9;i++)res.write(Buffer.alloc(1024*1024));return res.end();}
    const body=name==='svg'||name==='encoded.png'?svg:name==='giant'?giant:name==='malformed'?Buffer.from('not an image'):fixtures[name];
    res.setHeader('Content-Type',name==='svg'||name==='encoded.png'?'image/svg+xml':'image/png');res.end(body||'absent');
  });
  origin.listen(0,'127.0.0.1');await once(origin,'listening');const originPort=origin.address().port;
  const fallbackFixture=path.join(__dirname,'fixture/public/fallback.png');
  fs.mkdirSync(path.dirname(fallbackFixture),{recursive:true});
  fs.copyFileSync(path.join(__dirname,'../../apps/web/public/shadcn.png'),fallbackFixture);
  const port=await freePort();
  const fixtureModules=path.join(__dirname,'fixture/node_modules');
  if(!fs.existsSync(fixtureModules)) fs.symlinkSync('../../../apps/web/node_modules',fixtureModules,'dir');
  const logDir = process.env.IMAGE_QUALIFICATION_LOG_DIR || __dirname;
  fs.mkdirSync(logDir, { recursive: true });
  const log=fs.createWriteStream(path.join(logDir,'next-http.log'));
  const child=spawn(process.execPath,[appRequire.resolve('next/dist/bin/next'),'dev',path.join(__dirname,'fixture'),'-H','127.0.0.1','-p',String(port)],{env:{...process.env,NEXT_TELEMETRY_DISABLED:'1',IMAGE_FIXTURE_ORIGIN_PORT:String(originPort)},stdio:['ignore','pipe','pipe']});child.stdout.pipe(log);child.stderr.pipe(log);
  try {
    let ready=false;
    for(let i=0;i<100;i++){try{const r=await fetch(`http://127.0.0.1:${port}/`);if(r.ok){ready=true;break;}}catch{}await new Promise(r=>setTimeout(r,200));}
    assert.ok(ready,'Next fixture ready');
    async function optimize(resource,accept='image/webp'){return fetch(`http://127.0.0.1:${port}/_next/image?url=${encodeURIComponent(`http://127.0.0.1:${originPort}/${resource}`)}&w=32&q=75`,{headers:{accept}});}
    await check('Next HTTP existing7.7MB authored fallback remains available under8MiB cap',async()=>{
      const r=await fetch(`http://127.0.0.1:${port}/_next/image?url=%2Ffallback.png&w=32&q=75`,{headers:{accept:'image/webp'}});
      assert.equal(r.status,200);assert.equal((await sharp(Buffer.from(await r.arrayBuffer())).metadata()).width,32);
    });
    for(const format of Object.keys(fixtures)) await check(`Next HTTP ${format} to WebP`,async()=>{const r=await optimize(`allowed/${format}`);assert.equal(r.status,200);assert.equal(r.headers.get('content-type'),'image/webp');const m=await sharp(Buffer.from(await r.arrayBuffer())).metadata();assert.equal(m.width,32);assert.equal(m.height,24);});
    await check('Next HTTP AVIF output',async()=>{const r=await optimize('allowed/png','image/avif');assert.equal(r.status,200);assert.equal(r.headers.get('content-type'),'image/avif');assert.equal((await sharp(Buffer.from(await r.arrayBuffer())).metadata()).width,32);});
    for(const name of ['svg','encoded.png','malformed','oversize']) await check(`Next HTTP rejects ${name}`,async()=>{const r=await optimize(`allowed/${name}`);assert.equal(r.status,name==='oversize'?413:400);});
    await check('Next HTTP denied path',async()=>{assert.equal((await optimize('denied/png')).status,400);});
    await check('Next HTTP denied origin and malformed bounds',async()=>{
      const resource=encodeURIComponent(`http://localhost:${originPort}/allowed/png`);
      assert.equal((await fetch(`http://127.0.0.1:${port}/_next/image?url=${resource}&w=32&q=75`)).status,400);
      for(const query of ['w=0&q=75','w=32&q=101','w=999999&q=75']) assert.equal((await fetch(`http://127.0.0.1:${port}/_next/image?url=${encodeURIComponent(`http://127.0.0.1:${originPort}/allowed/png`)}&${query}`)).status,400);
    });
    await check('Next HTTP response attachment and sandbox headers',async()=>{const r=await optimize('allowed/png');assert.match(r.headers.get('content-disposition'),/^attachment/);assert.match(r.headers.get('content-security-policy'),/sandbox/);});
    await check('Next HTTP eight concurrent raster requests',async()=>{const results=await Promise.all(Array.from({length:8},()=>optimize('allowed/jpeg')));for(const r of results) assert.equal(r.status,200);});
    await check('Next HTTP redirect can escape allowed path (known limitation)',async()=>{assert.equal((await optimize('allowed/redirect-denied')).status,200);});
    await check('Next HTTP same-origin redirect',async()=>{assert.equal((await optimize('allowed/redirect')).status,200);});
    await check('Next HTTP pixel failure returns original (known limitation)',async()=>{const r=await optimize('allowed/giant');assert.equal(r.status,200);const bytes=Buffer.from(await r.arrayBuffer());assert.deepEqual(bytes,giant);assert.equal((await sharp(bytes).metadata()).width,5000);});
    console.log(JSON.stringify({passed:checks.length,checks,rss:process.memoryUsage().rss,limitation:'Next falls back to original on pixel-limit optimization error; not a fail-closed decoded-dimension admission control.'}));
  } finally {child.kill('SIGTERM');await once(child,'exit');origin.closeAllConnections();await new Promise(r=>origin.close(r));log.end();fs.rmSync(fallbackFixture);}
})().catch(e=>{console.error(e);process.exitCode=1;});
