import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { UsersHttpClient, UsersResponseError, UsersApiError } from '../dist/index.js';
const wallet = '0x' + 'a'.repeat(40), pool = '0x' + 'b'.repeat(40);
const auth = { signature: 'caller-signed-value', message: 'caller-message' };
const profile = { wallet_address: wallet, display_name: null, bio: null, socials: { website: null, twitter: null, telegram: null, discord: null }, images: { avatar: null, banner: null }, created_pools: [], created_at: null, updated_at: null };
test('real loopback transport rejects malformed successful DTOs on every route', async () => {
  let payload = {}, status = 200;
  const requests = [];
  const server = createServer(async (req, res) => {
    let body = ''; for await (const chunk of req) body += chunk;
    requests.push({ method: req.method, url: req.url, headers: req.headers, body });
    res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(payload));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const client = new UsersHttpClient({ baseUrl: `http://127.0.0.1:${server.address().port}` });
  try {
    await assert.rejects(client.getProfile(wallet), UsersResponseError);
    payload = profile; assert.deepEqual(await client.getProfile(wallet), profile);
    for (const invalid of [{ ...profile, created_pools: null }, { ...profile, socials: {} }, { ...profile, updated_at: 2 ** 53 }, { ...profile, images: { avatar: 3, banner: null } }]) {
      payload = invalid; await assert.rejects(client.getProfile(wallet), UsersResponseError);
    }
    payload = { wallet_address: wallet, pools: [{ pool_address: pool, added_at: '1' }] };
    await assert.rejects(client.getWatchlist(wallet), UsersResponseError);
    payload = { wallet_address: wallet, pools: [] }; assert.deepEqual(await client.getWatchlist(wallet), payload);
    const data = { display_name: 'Name', bio: '', website: '', twitter: '', telegram: '', discord: '' };
    const mutations = [() => client.updateProfile(wallet, { ...auth, data }), () => client.addWatchlist(wallet, pool, auth), () => client.removeWatchlist(wallet, pool, auth)];
    for (const call of mutations) { payload = { success: 'false' }; await assert.rejects(call(), UsersResponseError); payload = { success: true }; assert.deepEqual(await call(), payload); }
    assert.deepEqual(JSON.parse(requests.at(-1).body), auth);
    assert.equal(requests.at(-1).method, 'DELETE');
    const file = new Blob(['image'], { type: 'image/png' });
    payload = { success: true }; await assert.rejects(client.uploadImage(wallet, 'avatar', file, auth), UsersResponseError);
    payload = { success: 'false', url: 'https://example.com/image' }; await assert.rejects(client.uploadImage(wallet, 'banner', file, auth), UsersResponseError);
    payload = { success: true, url: 'https://example.com/image' }; assert.deepEqual(await client.uploadImage(wallet, 'avatar', file, auth), payload);
    assert.match(requests.at(-1).headers['content-type'], /^multipart\/form-data; boundary=/);
    assert.match(requests.at(-1).body, /name="signature"/);
    status = 403; payload = { statusCode: 403, error: 'Forbidden', message: 'Invalid signature' };
    await assert.rejects(client.getProfile(wallet), error => error instanceof UsersApiError && error.status === 403 && error.body.message === 'Invalid signature');
    assert.throws(() => client.getProfile('invalid'), /Invalid EVM address/);
    assert.throws(() => client.addWatchlist(wallet, pool, { signature: '', message: '' }), /Signature and message/);
  } finally { await new Promise(resolve => server.close(resolve)); }
});
