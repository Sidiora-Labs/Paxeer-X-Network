import { createServer, connect, type Server, type Socket } from 'node:net';
import { startGateway } from '../src/wallet/test/gateway';
import { GATEWAY_PORT } from './environment';

function forward(targetPort: number): Promise<{ server: Server; sockets: Set<Socket> }> {
    const sockets = new Set<Socket>();
    const server = createServer((inbound) => {
        const outbound = connect(targetPort, '127.0.0.1');
        sockets.add(inbound);
        sockets.add(outbound);
        inbound.pipe(outbound);
        outbound.pipe(inbound);
        const drop = () => {
            inbound.destroy();
            outbound.destroy();
            sockets.delete(inbound);
            sockets.delete(outbound);
        };
        inbound.on('error', drop);
        outbound.on('error', drop);
        inbound.on('close', drop);
        outbound.on('close', drop);
    });
    return new Promise((resolve, reject) => {
        server.once('error', reject);
        server.listen(GATEWAY_PORT, '127.0.0.1', () => resolve({ server, sockets }));
    });
}

export default async function globalSetup(): Promise<() => Promise<void>> {
    const gateway = await startGateway();
    const port = Number(new URL(gateway.url).port);
    const { server, sockets } = await forward(port);
    return async () => {
        for (const socket of sockets) socket.destroy();
        await new Promise<void>((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
        await gateway.close();
    };
}
