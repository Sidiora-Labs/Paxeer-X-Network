import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { PaxeerWallet, PaxeerWalletError } from '../src/core/wallet/embedded/sdk';
import { createPaxeerEIP1193Provider } from '../src/core/wallet/embedded/eip1193';

const TEST_CONFIG = {
  apiUrl: 'https://wallet-api.example.test/',
  supabaseUrl: 'https://auth.example.test',
  supabaseAnonKey: 'test-anon-key',
};

const CALLBACK_SOURCE = readFileSync(new URL('../src/pages/auth/callback.tsx', import.meta.url), 'utf8');

function authOptions(wallet: PaxeerWallet) {
  return wallet.supabase.auth as unknown as { flowType: unknown; detectSessionInUrl: unknown };
}

test('signingPathsUnchanged', async () => {
  const wallet = new PaxeerWallet(TEST_CONFIG);
  const provider = createPaxeerEIP1193Provider({
    wallet,
    chainId: 125,
    rpcUrl: 'https://rpc.example.test',
  });
  try {
    for (const method of ['eth_signTypedData_v4', 'eth_signTypedData_v3', 'eth_signTypedData']) {
      await assert.rejects(provider.request({ method, params: [] }), (err: Error & { code?: number }) => {
        assert.equal(err.code, 4200);
        assert.equal(
          err.message,
          'eth_signTypedData_v4 is not yet supported by the Paxeer Embedded Wallet API. Use an injected wallet for EIP-712 signing for now.',
        );
        return true;
      });
    }

    const tx = { to: '0x0000000000000000000000000000000000001015', value: '0x1', data: '0x' };
    const forwarded: Array<{ method: string; params: unknown[] }> = [
      { method: 'eth_sendTransaction', params: [tx] },
      { method: 'eth_signTransaction', params: [tx] },
      { method: 'personal_sign', params: ['0x68656c6c6f', '0x0000000000000000000000000000000000000001'] },
      { method: 'eth_sign', params: ['0x0000000000000000000000000000000000000001', '0x68656c6c6f'] },
    ];
    for (const req of forwarded) {
      await assert.rejects(provider.request(req), (err: unknown) => {
        assert.ok(err instanceof PaxeerWalletError, `${req.method} must reach the SDK signing call`);
        assert.equal(err.code, 'NO_SESSION');
        assert.equal(err.status, 401);
        assert.equal(err.message, 'not_authenticated');
        return true;
      });
    }

    await assert.rejects(wallet.getWallet(), (err: unknown) => {
      assert.ok(err instanceof PaxeerWalletError);
      assert.equal(err.code, 'NO_SESSION');
      assert.equal(err.status, 401);
      return true;
    });
  } finally {
    provider.destroy();
    await wallet.supabase.auth.stopAutoRefresh();
  }
});

test('pkceFlow', async () => {
  const wallet = new PaxeerWallet(TEST_CONFIG);
  try {
    assert.equal(authOptions(wallet).flowType, 'pkce');

    const { data, error } = await wallet.supabase.auth.exchangeCodeForSession('test-auth-code');
    assert.equal(data.session, null);
    assert.equal(data.user, null);
    assert.ok(error);
    assert.equal(error.code, 'pkce_code_verifier_not_found');
    assert.equal(error.status, 400);
    assert.equal(error.name, 'AuthPKCECodeVerifierMissingError');
  } finally {
    await wallet.supabase.auth.stopAutoRefresh();
  }
});

test('fragmentTokenIgnored', async () => {
  const wallet = new PaxeerWallet(TEST_CONFIG);
  try {
    assert.equal(authOptions(wallet).detectSessionInUrl, false);
    assert.equal(await wallet.getSession(), null);
  } finally {
    await wallet.supabase.auth.stopAutoRefresh();
  }

  const codeRead = CALLBACK_SOURCE.indexOf('new URLSearchParams(window.location.search).get("code")');
  const exchange = CALLBACK_SOURCE.indexOf('wallet.supabase.auth.exchangeCodeForSession(code)');
  const sessionRead = CALLBACK_SOURCE.indexOf('wallet.getSession()');
  assert.ok(codeRead > 0, 'callback reads the code query parameter');
  assert.ok(exchange > codeRead, 'callback exchanges the code after reading it');
  assert.ok(sessionRead > exchange, 'callback exchanges the code before the getSession read');
  assert.equal(CALLBACK_SOURCE.split('exchangeCodeForSession(').length - 1, 1);
  assert.equal(CALLBACK_SOURCE.split('wallet.getSession()').length - 1, 1);
  assert.ok(!CALLBACK_SOURCE.includes('location.hash'), 'callback never reads the URL fragment');
  assert.ok(!CALLBACK_SOURCE.includes('access_token'), 'callback never reads a fragment token');
});
