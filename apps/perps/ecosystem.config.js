module.exports = {
    apps: [
        {
            name: 'frontend',
            script: 'node_modules/next/dist/bin/next',
            args: 'start -p 3000',
            cwd: '/root/Hyperpax-UI',

            // ---- Cluster topology ----
            exec_mode: 'cluster',
            instances: 'max',           // 18 workers, one per vCPU

            // ---- Memory ceilings ----
            // Per-worker heap: 3 GB. Restart threshold: 3.5 GB RSS.
            // 18 × 3.5 GB = 63 GB worst case, leaves ~33 GB for OS,
            // page cache, PM2 god daemon, nginx, and burst headroom.
            node_args: '--max-old-space-size=3072',
            max_memory_restart: '3500M',

            // ---- Graceful lifecycle ----
            wait_ready: true,           // requires process.send('ready') in your server
            listen_timeout: 10000,      // wait up to 10s for ready signal
            kill_timeout: 8000,         // give SIGTERM 8s before SIGKILL on reload
            shutdown_with_message: true,

            // ---- Crash-loop protection ----
            autorestart: true,
            min_uptime: '30s',          // must stay up 30s to count as "started"
            max_restarts: 10,           // then PM2 backs off
            restart_delay: 4000,
            exp_backoff_restart_delay: 200,

            // ---- Logging ----
            out_file: '/var/log/pm2/frontend-out.log',
            error_file: '/var/log/pm2/frontend-error.log',
            merge_logs: true,           // single file across all 18 workers
            time: true,
            log_date_format: 'YYYY-MM-DD HH:mm:ss.SSS Z',

            // ---- Runtime env ----
            env: {
                NODE_ENV: 'production',
                PORT: 3000,
                // Bump libuv pool from default 4 — matters for DNS, fs, crypto, zlib
                UV_THREADPOOL_SIZE: 32,
                // Larger HTTP header buffer for downstream proxies
                NODE_OPTIONS: '--max-http-header-size=32768',
            },

            // ---- Misc ----
            watch: false,
            source_map_support: false,  // turn on only if you ship maps
            treekill: true,
        },
    ],
};