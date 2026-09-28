import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { bootstrap, unpack } from '@mercuryworkshop/proxy-bootstrap';
import { server as wisp } from '@mercuryworkshop/wisp-js/server';

const PORT = Number.parseInt(process.env.PORT ?? '8080', 10);
const PREFIX = '/api/proxy';
const CONTROLLER_VERSION = '0.0.13';
const SCRAMJET_VERSION = '2.0.67-alpha.1';
const EPOXY_VERSION = '3.0.1';

const config = {
  transport: 'libcurl',
  swPath: `${PREFIX}/sw.js`,
  wispPath: `${PREFIX}/wisp/`,
  bootstrapInitPath: `${PREFIX}/bootstrap-init.js`,
  epoxyClientPath: `${PREFIX}/clients/epoxy-client.js`,
  libcurlClientPath: `${PREFIX}/clients/libcurl-client.js`,
  bareClientPath: `${PREFIX}/clients/bare-client.js`,
  scramjetControllerApiPath: `${PREFIX}/controller/controller.api.js`,
  scramjetControllerInjectPath: `${PREFIX}/controller/controller.inject.js`,
  scramjetControllerSwPath: `${PREFIX}/controller/controller.sw.js`,
  scramjetBundlePath: `${PREFIX}/scram/scramjet.js`,
  scramjetWasmPath: `${PREFIX}/scram/scramjet.wasm`,
  scramjetUtilsBundlePath: `${PREFIX}/scram/scramjet-utils.js`,
};

const { routeRequest } = await bootstrap(config);
await unpack(
  `https://registry.npmjs.org/@mercuryworkshop/scramjet-controller/-/scramjet-controller-${CONTROLLER_VERSION}.tgz`,
  'controller',
);
await unpack(
  `https://registry.npmjs.org/@mercuryworkshop/scramjet/-/scramjet-${SCRAMJET_VERSION}.tgz`,
  'scramjet',
);
await unpack(
  `https://registry.npmjs.org/@mercuryworkshop/epoxy-transport/-/epoxy-transport-${EPOXY_VERSION}.tgz`,
  'epoxy-transport',
);
const epoxyClient = await readFile(
  new URL(
    './node_modules/@mercuryworkshop/proxy-bootstrap/dist/.downloads/epoxy-transport/package/dist/index.js',
    import.meta.url,
  ),
);
console.log(
  `Pinned Scramjet Controller ${CONTROLLER_VERSION} with Scramjet ${SCRAMJET_VERSION} and Epoxy ${EPOXY_VERSION}`,
);

const server = http.createServer((request, response) => {
  if (request.url === `${PREFIX}/health`) {
    response.writeHead(200, {
      'Content-Type': 'application/json',
      'Cache-Control': 'no-store',
    });
    response.end(JSON.stringify({ status: 'ok' }));
    return;
  }

  if (request.url === config.epoxyClientPath) {
    response.writeHead(200, {
      'Content-Type': 'application/javascript',
      'Cache-Control': 'no-store',
    });
    response.end(epoxyClient);
    return;
  }

  if (routeRequest(request, response)) return;

  response.writeHead(404, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
  });
  response.end(JSON.stringify({ error: 'Not found' }));
});

server.on('upgrade', (request, socket, head) => {
  if (request.url?.startsWith(`${PREFIX}/wisp/`)) {
    wisp.routeRequest(request, socket, head);
    return;
  }
  socket.destroy();
});

function shutdown() {
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 10_000).unref();
}

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);

server.listen(PORT, '0.0.0.0', () => {
  console.log(`Paxport proxy relay listening on ${PORT}`);
});
