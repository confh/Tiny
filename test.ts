import net from "net";

type PendingRequest = {
    resolve: (value: any) => void;
    reject: (error: Error) => void;
    timeout: NodeJS.Timeout;
};

export class TinyKV {
    private socket: net.Socket;
    private buffer = "";
    private nextId = 1;
    private pending = new Map<number, PendingRequest>();
    private connectedPromise: Promise<void>;

    constructor(host = "127.0.0.1", port = 6379) {
        this.socket = net.createConnection({ host, port });

        this.connectedPromise = new Promise((resolve, reject) => {
            this.socket.once("connect", resolve);
            this.socket.once("error", reject);
        });

        this.socket.on("data", (chunk) => {
            this.handleData(chunk);
        });

        this.socket.on("error", (err) => {
            this.failAll(err);
        });

        this.socket.on("close", () => {
            this.failAll(new Error("TinyKV connection closed"));
        });
    }

    private handleData(chunk: Buffer) {
        this.buffer += chunk.toString("utf8");

        while (true) {
            const newline = this.buffer.indexOf("\n");
            if (newline === -1) break;

            const line = this.buffer.slice(0, newline).trim();
            this.buffer = this.buffer.slice(newline + 1);

            if (line.length === 0) continue;

            let msg: any;

            try {
                msg = JSON.parse(line);
            } catch {
                continue;
            }

            const id = msg.id;

            if (typeof id !== "number") {
                continue;
            }

            const pending = this.pending.get(id);
            if (!pending) {
                continue;
            }

            clearTimeout(pending.timeout);
            this.pending.delete(id);

            if (msg.ok === false) {
                pending.reject(new Error(msg.error ?? "TinyKV request failed"));
            } else {
                pending.resolve(msg);
            }
        }
    }

    private failAll(error: Error) {
        for (const [, pending] of this.pending) {
            clearTimeout(pending.timeout);
            pending.reject(error);
        }

        this.pending.clear();
    }

    private async request(payload: Record<string, any>, timeoutMs = 5000): Promise<any> {
        await this.connectedPromise;

        const id = this.nextId++;

        const msg = {
            id,
            ...payload,
        };

        return new Promise((resolve, reject) => {
            const timeout = setTimeout(() => {
                this.pending.delete(id);
                reject(new Error(`TinyKV request timed out: ${payload.cmd}`));
            }, timeoutMs);

            this.pending.set(id, {
                resolve,
                reject,
                timeout,
            });

            this.socket.write(JSON.stringify(msg) + "\n");
        });
    }

    async ping() {
        const res = await this.request({ cmd: "ping" });
        return res.value;
    }

    async set(key: string, value: object, ttlMs?: number) {
        return this.request({ cmd: "set", key, value, ttlMs });
    }

    async get<T = any>(key: string): Promise<T | null> {
        const res = await this.request({ cmd: "get", key });
        return res.exists ? res.value : null;
    }

    async delete(key: string): Promise<boolean> {
        const res = await this.request({ cmd: "delete", key });
        return res.deleted;
    }

    async has(key: string): Promise<boolean> {
        const res = await this.request({ cmd: "has", key });
        return res.exists;
    }

    async keys(): Promise<string[]> {
        const res = await this.request({ cmd: "keys" });
        return res.keys;
    }

    async count(): Promise<number> {
        const res = await this.request({ cmd: "count" });
        return res.count;
    }

    async clear() {
        return this.request({ cmd: "clear" });
    }

    close() {
        this.socket.end();
    }
}

const test = new TinyKV("localhost", 6379)

await test.set("cached_user_settings:702061871292874783", {
    id: "702061871292874783",
    defaultAI: "LEARNLM",
    notes: {}
})

console.log(await test.get("cached_user_settings:702061871292874783"))

test.close()