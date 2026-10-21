import { createRequire } from 'node:module';
import { join } from 'node:path';
import { encrypt } from '../../gateway/src/crypto.ts';

const require = createRequire(join(process.cwd(), 'package.json'));
const { generatePrivateKey, privateKeyToAccount } = require('viem/accounts');

const masterKey = Buffer.from(process.env.CEREMONY_MASTER_KEY ?? '', 'base64');
const count = Number.parseInt(process.argv[2] ?? '1', 10);
if (!Number.isInteger(count) || count < 1) {
  throw new Error('count must be a positive integer');
}

const vectors = [];
for (let i = 0; i < count; i++) {
  const privateKey = generatePrivateKey();
  const account = privateKeyToAccount(privateKey);
  const { ciphertext, version } = encrypt(Buffer.from(privateKey.slice(2), 'hex'), masterKey, 1);
  vectors.push({ envelope: ciphertext, version, address: account.address, key: privateKey.slice(2) });
}
process.stdout.write(JSON.stringify({ vectors }));
