import { MaxApiError, MaxNetworkError } from "./errors.js";

export interface ClientOptions {
  /** API origin, e.g. "https://api.max.example" or "http://localhost:8080". */
  baseUrl: string;
  /** Agent API key (`maxk_…`). Sent as a Bearer token. */
  apiKey?: string;
  /**
   * Native apps: a session token from `auth.login(wallet, { tokenDelivery: "body" })`,
   * kept in secure storage and sent as a Bearer token.
   */
  sessionToken?: string;
  /** Custom fetch (tests, proxies, older runtimes). Defaults to globalThis.fetch. */
  fetch?: typeof fetch;
  /**
   * Browsers: send the session cookie cross-origin. Defaults to "include".
   */
  credentials?: RequestCredentials;
  /**
   * Server-side scripts: remember the session cookie returned by wallet login
   * and send it on later requests. Browsers manage cookies themselves.
   */
  persistSession?: boolean;
  /** Per-attempt timeout. Default 15s. */
  timeoutMs?: number;
  /** Retries for safe requests on network errors and 5xx. Default 2. */
  maxRetries?: number;
  /** Extra headers on every request. */
  headers?: Record<string, string>;
}

export interface RequestOptions {
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  /**
   * Allows retrying a non-GET request. Only set for endpoints that are
   * idempotent by key (prepare, propose, rename), where a replay returns the
   * original result instead of acting twice.
   */
  retrySafe?: boolean;
  signal?: AbortSignal;
}

const SESSION_COOKIE = "max_session";

export class HttpClient {
  private readonly baseUrl: string;
  private readonly fetchFn: typeof fetch;
  private sessionCookie?: string;
  private sessionToken?: string;

  constructor(private readonly opts: ClientOptions) {
    if (!opts.baseUrl) throw new Error("MaxClient: baseUrl is required");
    this.baseUrl = opts.baseUrl.replace(/\/+$/, "");
    const f = opts.fetch ?? globalThis.fetch;
    if (!f) throw new Error("MaxClient: no fetch implementation available; pass options.fetch");
    this.fetchFn = f.bind(globalThis);
    this.sessionToken = opts.sessionToken;
  }

  /** Set or clear the Bearer session token (native apps). */
  setSessionToken(token: string | undefined): void {
    this.sessionToken = token;
  }

  get<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("GET", path, { ...opts, retrySafe: true });
  }
  post<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("POST", path, opts);
  }
  put<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("PUT", path, opts);
  }
  patch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("PATCH", path, opts);
  }
  delete<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    return this.request<T>("DELETE", path, opts);
  }

  /** The raw response of a request that may redirect (used for OAuth callbacks in tests). */
  url(path: string, query?: RequestOptions["query"]): string {
    return this.baseUrl + path + toQuery(query);
  }

  async request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
    const maxRetries = opts.retrySafe ? (this.opts.maxRetries ?? 2) : 0;
    let attempt = 0;
    for (;;) {
      try {
        return await this.once<T>(method, path, opts);
      } catch (err) {
        const retryable =
          err instanceof MaxNetworkError || (err instanceof MaxApiError && (err.status >= 500 || err.status === 429));
        if (!retryable || attempt >= maxRetries || opts.signal?.aborted) throw err;
        attempt++;
        await sleep(Math.min(250 * 2 ** (attempt - 1), 2000) * (0.5 + Math.random() / 2));
      }
    }
  }

  private async once<T>(method: string, path: string, opts: RequestOptions): Promise<T> {
    const headers: Record<string, string> = { Accept: "application/json", ...this.opts.headers };
    if (opts.body !== undefined) headers["Content-Type"] = "application/json";
    if (this.opts.apiKey) headers["Authorization"] = `Bearer ${this.opts.apiKey}`;
    else if (this.sessionToken) headers["Authorization"] = `Bearer ${this.sessionToken}`;
    if (this.sessionCookie) headers["Cookie"] = `${SESSION_COOKIE}=${this.sessionCookie}`;

    const timeout = AbortSignal.timeout(this.opts.timeoutMs ?? 15_000);
    const signal = opts.signal ? anySignal([opts.signal, timeout]) : timeout;

    let res: Response;
    try {
      res = await this.fetchFn(this.baseUrl + path + toQuery(opts.query), {
        method,
        headers,
        body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
        credentials: this.opts.credentials ?? "include",
        signal,
      });
    } catch (err) {
      throw new MaxNetworkError(`${method} ${path} failed: ${(err as Error)?.message ?? err}`, err);
    }

    if (this.opts.persistSession) this.captureSession(res);
    const requestId = res.headers.get("X-Request-ID") ?? undefined;
    if (res.status === 204) return undefined as T;

    let body: any;
    try {
      body = await res.json();
    } catch {
      throw new MaxApiError("INVALID_RESPONSE", `unexpected non-JSON response (HTTP ${res.status})`, res.status, requestId);
    }
    if (!res.ok || body?.error) {
      const e = body?.error ?? {};
      const details = e.details && typeof e.details === "object" ? (e.details as Record<string, unknown>) : undefined;
      throw new MaxApiError(e.code ?? "HTTP_ERROR", e.message ?? `HTTP ${res.status}`, res.status, requestId, details);
    }
    return body.data as T;
  }

  private captureSession(res: Response): void {
    const raw = typeof (res.headers as any).getSetCookie === "function"
      ? ((res.headers as any).getSetCookie() as string[])
      : [res.headers.get("set-cookie") ?? ""];
    for (const c of raw) {
      const m = new RegExp(`(?:^|,\\s*)${SESSION_COOKIE}=([^;]*)`).exec(c);
      if (!m) continue;
      const expired = /max-age=(0|-\d+)/i.test(c) || m[1] === "";
      this.sessionCookie = expired ? undefined : m[1];
    }
  }

  /** Forget a persisted session (server-side scripts). */
  clearSession(): void {
    this.sessionCookie = undefined;
    this.sessionToken = undefined;
  }

  get hasSession(): boolean {
    return this.sessionCookie !== undefined;
  }
}

function toQuery(q?: RequestOptions["query"]): string {
  if (!q) return "";
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) if (v !== undefined) params.set(k, String(v));
  const s = params.toString();
  return s ? `?${s}` : "";
}

function anySignal(signals: AbortSignal[]): AbortSignal {
  const any = (AbortSignal as any).any;
  if (typeof any === "function") return any(signals);
  const ctrl = new AbortController();
  for (const s of signals) {
    if (s.aborted) return AbortSignal.abort(s.reason);
    s.addEventListener("abort", () => ctrl.abort(s.reason), { once: true });
  }
  return ctrl.signal;
}

export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(signal.reason);
    const t = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => (clearTimeout(t), reject(signal.reason)), { once: true });
  });
}

export function idempotencyKey(): string {
  return globalThis.crypto.randomUUID();
}

export function enc(segment: string): string {
  return encodeURIComponent(segment);
}
