#!/usr/bin/env node
// Seed the SID airdrop campaign into the push system.
// Run after deploying: node scripts/seed-sid-campaign.js
// Or use the API directly:
//   curl -X POST https://your-domain/api/push/cron \
//     -H "Content-Type: application/json" \
//     -H "x-push-admin-key: YOUR_KEY" \
//     -d '{ "id": "sid-airdrop-2026", "title": "$SID Airdrop Starts Tomorrow!", "body": "The largest $SID airdrop on Paxeer Network begins tomorrow. Hold $SID in your wallet to qualify.", "tag": "sid-airdrop", "url": "/" }'

const fs = require('fs');
const path = require('path');

const DATA_DIR = process.env.PUSH_DATA_DIR || path.join(process.cwd(), '.push-data');
const CAMPAIGNS_FILE = path.join(DATA_DIR, 'campaigns.json');

if (!fs.existsSync(DATA_DIR)) {
  fs.mkdirSync(DATA_DIR, { recursive: true });
}

const campaign = {
  id: 'sid-airdrop-2026',
  title: '$SID Airdrop Starts Tomorrow!',
  body: 'The largest $SID airdrop on Paxeer Network begins tomorrow. Hold $SID in your wallet to qualify.',
  url: '/',
  tag: 'sid-airdrop',
  // Send immediately (no scheduledAt means it fires on the next cron run)
};

let campaigns = [];
if (fs.existsSync(CAMPAIGNS_FILE)) {
  try {
    campaigns = JSON.parse(fs.readFileSync(CAMPAIGNS_FILE, 'utf-8'));
  } catch { campaigns = []; }
}

if (campaigns.find(c => c.id === campaign.id)) {
  console.log('Campaign "sid-airdrop-2026" already exists. Skipping.');
} else {
  campaigns.push(campaign);
  fs.writeFileSync(CAMPAIGNS_FILE, JSON.stringify(campaigns, null, 2));
  console.log('Seeded campaign: $SID Airdrop Starts Tomorrow!');
  console.log('It will be sent to all subscribers on the next cron run.');
  console.log(`Trigger it: curl -H "x-push-admin-key: YOUR_KEY" https://your-domain/api/push/cron`);
}
