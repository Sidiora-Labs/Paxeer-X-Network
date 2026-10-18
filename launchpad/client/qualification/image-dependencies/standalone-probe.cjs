const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const { createServer } = require('node:http');
const { once } = require('node:events');
const { spawn } = require('node:child_process');
(async () => {
  const standalone = path.resolve(process.env.NEXT_STANDALONE_ROOT || path.join(__dirname, '../../apps/web/.next/standalone'));
  const app = path.join(standalone, 'apps/web');
  const server = path.join(app, 'server.js');
  assert.ok(fs.existsSync(server), 'Build the standalone Next app before probing');
  const builtConfig = JSON.parse(fs.readFileSync(path.join(app, '.next/required-server-files.json'), 'utf8')).config;
  assert.equal(builtConfig.images.maximumResponseBody, 8388608);
  assert.equal(builtConfig.images.dangerouslyAllowSVG, false);
  const appRequire = createRequire(server);
  const nextRequire = createRequire(appRequire.resolve('next/package.json'));
  const sharp = nextRequire('sharp');
  assert.equal(sharp.versions.sharp, '0.35.5');
  assert.equal(sharp.versions.heif, '1.23.5', 'Standalone must retain libheif version evidence for Next AVIF decoding');
  assert.equal(sharp.versions.rsvg, '2.63.2');
  console.log(JSON.stringify({ standalone, sharp: sharp.versions }));
  fs.mkdirSync(path.join(app, 'public'), { recursive: true });
  const fixtureDir = fs.mkdtempSync(path.join(app, 'public', 'image-qualification-'));
  const prefix = '/' + path.basename(fixtureDir);
  let child;
  let log;
  try {
    for (const format of ['png', 'jpeg', 'webp', 'avif']) fs.writeFileSync(path.join(fixtureDir, `fixture.${format}`), await sharp({ create: { width: 64, height: 48, channels: 3, background: 'red' } }).toFormat(format).toBuffer());
    fs.writeFileSync(path.join(fixtureDir, 'invalid.png'), 'invalid');
    fs.writeFileSync(path.join(fixtureDir, 'encoded.png'), '<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>');
    fs.copyFileSync(path.join(__dirname, '../../apps/web/public/shadcn.png'), path.join(fixtureDir, 'fallback.png'));
    const socket = createServer(); socket.listen(0, '127.0.0.1'); await once(socket, 'listening'); const port = socket.address().port; await new Promise(r => socket.close(r));
    const logDir = process.env.IMAGE_QUALIFICATION_LOG_DIR || __dirname;
    fs.mkdirSync(logDir, { recursive: true });
    log = fs.createWriteStream(path.join(logDir, 'standalone-http.log'));
    child = spawn(process.execPath, [server], { cwd: app, env: { ...process.env, PORT: String(port), HOSTNAME: '127.0.0.1', NEXT_TELEMETRY_DISABLED: '1' }, stdio: ['ignore', 'pipe', 'pipe'] });
    child.stdout.pipe(log); child.stderr.pipe(log);
    let ready = false;
    for (let i = 0; i < 100; i++) { try { if ((await fetch(`http://127.0.0.1:${port}${prefix}/fixture.png`)).ok) { ready = true; break; } } catch {} await new Promise(r => setTimeout(r, 100)); }
    assert.ok(ready, 'standalone server ready');
    for (const format of ['png', 'jpeg', 'webp', 'avif']) {
      const r = await fetch(`http://127.0.0.1:${port}/_next/image?url=${encodeURIComponent(prefix + '/fixture.' + format)}&w=32&q=75`, { headers: { Accept: 'image/webp' } });
      assert.equal(r.status, 200); assert.equal(r.headers.get('content-type'), 'image/webp');
      const metadata = await sharp(Buffer.from(await r.arrayBuffer())).metadata(); assert.equal(metadata.width, 32); assert.equal(metadata.height, 24);
      console.log('PASS standalone HTTP', format, 'to WebP');
    }
    for (const file of ['invalid.png', 'encoded.png']) {
      const r = await fetch(`http://127.0.0.1:${port}/_next/image?url=${encodeURIComponent(prefix + '/' + file)}&w=32&q=75`);
      assert.equal(r.status, 400); console.log('PASS standalone HTTP rejects', file);
    }
    const fallback = await fetch(`http://127.0.0.1:${port}/_next/image?url=${encodeURIComponent(prefix + '/fallback.png')}&w=32&q=75`, { headers: { Accept: 'image/webp' } });
    assert.equal(fallback.status, 200);
    assert.equal((await sharp(Buffer.from(await fallback.arrayBuffer())).metadata()).width, 32);
    console.log('PASS standalone HTTP existing fallback under final 8 MiB cap');
  } finally {
    if (child && child.exitCode === null) { child.kill('SIGTERM'); await once(child, 'exit'); }
    if (log) log.end();
    fs.rmSync(fixtureDir, { recursive: true });
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
