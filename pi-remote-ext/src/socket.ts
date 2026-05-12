// SPDX-License-Identifier: MIT
import { EventEmitter } from "node:events";
import { type Socket, connect as netConnect } from "node:net";

export type DaemonDial = (path: string) => Promise<Socket>;

export interface DaemonSocketOptions {
  path: string;
  autoReconnect?: boolean;
  connectTimeoutMs?: number;
  onScheduleReconnect?: (delayMs: number) => void;
  dial?: DaemonDial;
}

const INITIAL_BACKOFF_MS = 1000;
const MAX_BACKOFF_MS = 30_000;
const DEFAULT_CONNECT_TIMEOUT_MS = 5000;

const defaultDial: DaemonDial = (path) =>
  new Promise<Socket>((resolve, reject) => {
    const sock = netConnect(path);
    const onError = (err: Error) => {
      sock.removeListener("connect", onConnect);
      reject(err);
    };
    const onConnect = () => {
      sock.removeListener("error", onError);
      resolve(sock);
    };
    sock.once("error", onError);
    sock.once("connect", onConnect);
  });

export class DaemonSocket extends EventEmitter {
  private readonly path: string;
  private readonly autoReconnect: boolean;
  private readonly connectTimeoutMs: number;
  private readonly onScheduleReconnect: ((delayMs: number) => void) | undefined;
  private readonly dial: DaemonDial;

  private socket: Socket | undefined;
  private buffer = "";
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private nextDelayMs = INITIAL_BACKOFF_MS;
  private closed = false;
  private _connected = false;
  private hasEverConnected = false;

  constructor(opts: DaemonSocketOptions) {
    super();
    this.path = opts.path;
    this.autoReconnect = opts.autoReconnect ?? true;
    this.connectTimeoutMs = opts.connectTimeoutMs ?? DEFAULT_CONNECT_TIMEOUT_MS;
    this.onScheduleReconnect = opts.onScheduleReconnect;
    this.dial = opts.dial ?? defaultDial;
  }

  get connected(): boolean {
    return this._connected;
  }

  async connect(): Promise<void> {
    if (this.closed) {
      throw new Error("DaemonSocket has been closed");
    }
    // Initial connect: single attempt with timeout. Per SPEC § 6.3, a failure
    // here means "log, continue without remote" — no retries until the next
    // session. The exponential-backoff reconnect loop only runs after a
    // successful initial connect drops.
    const sock = await this.dialWithTimeout();
    this.attach(sock);
  }

  send(msg: unknown): void {
    if (!this._connected || !this.socket) return;
    try {
      this.socket.write(`${JSON.stringify(msg)}\n`);
    } catch {
      // Socket has gone away mid-write (EPIPE etc). The 'close' handler will
      // run reconnect logic; nothing to do here.
    }
  }

  close(): void {
    this.closed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
    if (this.socket) {
      this.socket.removeAllListeners("close");
      this.socket.destroy();
      this.socket = undefined;
    }
    this._connected = false;
  }

  private dialWithTimeout(): Promise<Socket> {
    return new Promise<Socket>((resolve, reject) => {
      let settled = false;
      const timer = setTimeout(() => {
        if (settled) return;
        settled = true;
        reject(new Error(`connect timeout after ${this.connectTimeoutMs}ms`));
      }, this.connectTimeoutMs);
      this.dial(this.path).then(
        (sock) => {
          if (settled) {
            sock.destroy();
            return;
          }
          settled = true;
          clearTimeout(timer);
          resolve(sock);
        },
        (err) => {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
          reject(err);
        },
      );
    });
  }

  private attach(sock: Socket): void {
    this.socket = sock;
    this._connected = true;
    this.buffer = "";
    this.nextDelayMs = INITIAL_BACKOFF_MS;
    const wasReconnect = this.hasEverConnected;
    this.hasEverConnected = true;

    sock.on("data", (chunk: Buffer) => this.onData(chunk));
    sock.on("error", () => {
      // The matching 'close' event will run reconnect.
    });
    sock.on("close", () => {
      this._connected = false;
      this.socket = undefined;
      this.emit("disconnected");
      if (this.autoReconnect && !this.closed) {
        this.scheduleReconnect();
      }
    });

    if (wasReconnect) this.emit("reconnected");
  }

  private onData(chunk: Buffer): void {
    this.buffer += chunk.toString("utf8");
    let nl = this.buffer.indexOf("\n");
    while (nl >= 0) {
      const line = this.buffer.slice(0, nl);
      this.buffer = this.buffer.slice(nl + 1);
      if (line.length > 0) {
        try {
          const msg: unknown = JSON.parse(line);
          this.emit("message", msg);
        } catch {
          // Malformed line: skip rather than tear down the whole connection.
        }
      }
      nl = this.buffer.indexOf("\n");
    }
  }

  private scheduleReconnect(): void {
    if (this.closed || this.reconnectTimer) return;
    const delay = Math.min(this.nextDelayMs, MAX_BACKOFF_MS);
    this.onScheduleReconnect?.(delay);
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = undefined;
      if (this.closed) return;
      this.dialWithTimeout().then(
        (sock) => {
          if (this.closed) {
            sock.destroy();
            return;
          }
          this.attach(sock);
        },
        () => {
          this.nextDelayMs = Math.min(this.nextDelayMs * 2, MAX_BACKOFF_MS);
          if (!this.closed) this.scheduleReconnect();
        },
      );
    }, delay);
  }
}
