import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

export interface EphemeralPostgres {
  url: string;
  stop(): Promise<void>;
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (addr === null || typeof addr === 'string') {
        srv.close();
        reject(new Error('could not determine a free port'));
        return;
      }
      const port = addr.port;
      srv.close(() => resolve(port));
    });
  });
}

function runAsOwner(bin: string, args: string[]): void {
  const isRoot = typeof process.getuid === 'function' && process.getuid() === 0;
  const [cmd, argv] = isRoot ? ['runuser', ['-u', 'postgres', '--', bin, ...args]] : [bin, args];
  const res = spawnSync(cmd, argv, { encoding: 'utf8' });
  if (res.status !== 0) {
    throw new Error(
      `${bin} ${args.join(' ')} failed with ${res.status}:\nstderr: ${res.stderr ?? ''}\nstdout: ${res.stdout ?? ''}`,
    );
  }
}

export async function startPostgres(): Promise<EphemeralPostgres> {
  const binDir = process.env.PG_BIN_DIR ?? '/usr/lib/postgresql/16/bin';
  const initdb = join(binDir, 'initdb');
  const pgCtl = join(binDir, 'pg_ctl');
  if (!existsSync(initdb) || !existsSync(pgCtl)) {
    throw new Error(`postgres binaries not found under ${binDir}; set PG_BIN_DIR`);
  }
  const isRoot = typeof process.getuid === 'function' && process.getuid() === 0;
  const dir = mkdtempSync(join(tmpdir(), 'gateway-pg-'));
  const dataDir = join(dir, 'data');
  const logFile = join(dir, 'postgres.log');
  if (isRoot) {
    chmodSync(dir, 0o777);
    execFileSync('chown', ['postgres', dir]);
  }
  const port = await freePort();
  runAsOwner(initdb, ['-D', dataDir, '-U', 'postgres', '-A', 'trust', '--no-sync', '-E', 'UTF8']);
  try {
    runAsOwner(pgCtl, [
      '-D',
      dataDir,
      '-l',
      logFile,
      '-w',
      '-o',
      `-p ${port} -c listen_addresses=127.0.0.1 -c unix_socket_directories=${dir} -c fsync=off -c max_connections=200`,
      'start',
    ]);
  } catch (err) {
    let serverLog: string;
    try {
      serverLog = readFileSync(logFile, 'utf8');
    } catch (readErr) {
      serverLog = `(server log unreadable: ${(readErr as Error).message})`;
    }
    throw new Error(`${(err as Error).message}\nserver log kept at ${logFile}:\n${serverLog}`);
  }
  const url = `postgres://postgres@127.0.0.1:${port}/postgres`;

  const stop = async (): Promise<void> => {
    try {
      runAsOwner(pgCtl, ['-D', dataDir, '-m', 'immediate', '-w', 'stop']);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  };

  try {
    process.env.DATABASE_URL = url;
    const { env } = await import('../../src/env.js');
    if (env.DATABASE_URL !== url) {
      throw new Error('startPostgres must run before any module loads src/env.ts');
    }
    const { runMigrations } = await import('../../src/db/migrate.js');
    await runMigrations();
  } catch (err) {
    await stop();
    throw err;
  }
  return { url, stop };
}
