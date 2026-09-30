import http from 'http';
import type { AddressInfo } from 'net';

export interface TestServer {
  readonly url: string;
  close(): Promise<void>;
}

export function createTestServer(handler: http.RequestListener): Promise<TestServer> {
  return new Promise((resolve) => {
    const server = http.createServer(handler);
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address() as AddressInfo;
      resolve({
        url: `http://127.0.0.1:${port}`,
        close: () =>
          new Promise<void>((res, rej) => {
            server.close((err) => (err ? rej(err) : res()));
          }),
      });
    });
  });
}

export function writeJSON(res: http.ServerResponse, data: unknown): void {
  const body = JSON.stringify(data);
  res.setHeader('Content-Type', 'application/json');
  res.setHeader('Content-Length', Buffer.byteLength(body));
  res.end(body);
}
