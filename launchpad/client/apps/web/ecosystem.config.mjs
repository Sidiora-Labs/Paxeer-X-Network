export default {
  apps: [
    {
      name: 'my-next-app',
      // The entry point is the compiled next server inside your standalone directory
      script: 'node_modules/next/dist/bin/next',
      args: 'start -p 3000', 
      
      // Point the current working directory to your app folder
      cwd: '/sidiora/client/apps/web', 
      
      // Automatically restart if the app crashes
      watch: false,
      max_restarts: 10,
      
      // Recommended for Node.js servers handling Next.js
      exec_mode: 'fork', 
      instances: 1,

      // Environment variables
      env: {
        NODE_ENV: 'development',
        PORT: '3000'
      },
      env_production: {
        NODE_ENV: 'production',
        PORT: '3000',
        // Add other production environment variables here
      }
    }
  ]
};
