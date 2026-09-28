#!/usr/bin/env node
// Generate VAPID keys for Web Push notifications.
// Run: node scripts/generate-vapid-keys.js
// Then add the output to your .env.local file.

const webpush = require('web-push');

const keys = webpush.generateVAPIDKeys();

console.log('Add these to your .env.local:\n');
console.log(`NEXT_PUBLIC_VAPID_PUBLIC_KEY=${keys.publicKey}`);
console.log(`VAPID_PRIVATE_KEY=${keys.privateKey}`);
console.log(`VAPID_SUBJECT=mailto:admin@paxeer.app`);
console.log(`PUSH_ADMIN_KEY=${require('crypto').randomBytes(32).toString('hex')}`);
console.log('\nDone. Never commit the private key or admin key to git.');
