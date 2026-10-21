// ecosystem.config.cjs
module.exports = {
  apps: [
    {
      name: 'paxeer-wallet',
      script: 'node_modules/next/dist/bin/next',
      args: 'start -p 5000',
      cwd: __dirname,
      instances: 1,
      exec_mode: 'fork',
      autorestart: true,
      max_restarts: 10,
      min_uptime: '10s',
      env: {
        NODE_ENV: 'production',
        PORT: '5000',
        NEXT_MANUAL_SIG_HANDLE: 'true',
      },
      error_file: '/var/log/pm2/paxeer-wallet-error.log',
      out_file: '/var/log/pm2/paxeer-wallet-out.log',
      merge_logs: true,
      time: true,
    },
  ],
};