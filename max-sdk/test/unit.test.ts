import assert from "node:assert/strict";
import { test, describe } from "node:test";
import { webcrypto } from "node:crypto";

import {
  MaxClient,
  MaxApiError,
  AuthorizationError,
  AUTHORIZATION_DOMAIN,
  PolicySigner,
  assertEvmTransactionMatches,
  assertSolanaTransferMatches,
  evmTransactionFromClaims,
  formatUnits,
  normalizeName,
  parseUnits,
  verifyAuthorization,
  type AuthorizationClaims,
  type AuthorizationKey,
  type SignedAuthorization,
} from "../src/index.js";

type Handler = (req: { method: string; url: URL; headers: Headers; body: any }) => Response | Promise<Response>;

function mockFetch(routes: Record<string, Handler | Handler[] | Response>) {
  const calls: { method: string; path: string; headers: Headers; body: any }[] = [];
  const counters: Record<string, number> = {};
  const fetchFn = (async (input: string, init: RequestInit = {}) => {
    const url = new URL(input);
    const method = init.method ?? "GET";
    const key = `${method} ${url.pathname}`;
    const headers = new Headers(init.headers);
    const body = init.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ method, path: url.pathname + url.search, headers, body });
    const route = routes[key];
    if (!route) return json(404, { error: { code: "NOT_FOUND", message: key } });
    if (route instanceof Response) return route.clone();
    const n = (counters[key] = (counters[key] ?? 0) + 1);
    const h = Array.isArray(route) ? route[Math.min(n, route.length) - 1]! : route;
    return h({ method, url, headers, body });
  }) as unknown as typeof fetch;
  return { fetchFn, calls };
}

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });
}
const ok = (data: unknown, headers?: Record<string, string>) => json(200, { data }, headers);

const client = (fetchFn: typeof fetch, extra: object = {}) =>
  new MaxClient({ baseUrl: "http://api.test/", fetch: fetchFn, maxRetries: 2, ...extra });

describe("http", () => {
  test("unwraps data and maps API errors with code, status and request id", async () => {
    const { fetchFn } = mockFetch({
      "GET /v1/resolve/max.yoda": ok({ address: "So1", resolutionVersion: 3 }),
      "GET /v1/resolve/max.nobody": () =>
        json(404, { error: { code: "MAX_ID_NOT_FOUND", message: "MAX ID not found" } }, { "X-Request-ID": "req-1" }),
    });
    const c = client(fetchFn);
    const r = await c.resolve("max.yoda", { chain: "solana" });
    assert.equal(r.address, "So1");

    await assert.rejects(c.resolve("max.nobody", { chain: "solana" }), (e: unknown) => {
      assert.ok(e instanceof MaxApiError);
      assert.equal(e.code, "MAX_ID_NOT_FOUND");
      assert.equal(e.status, 404);
      assert.equal(e.requestId, "req-1");
      assert.ok(e.is("MAX_ID_NOT_FOUND"));
      return true;
    });
  });

  test("retries GETs on 5xx but never retries a non-idempotent POST", async () => {
    const { fetchFn, calls } = mockFetch({
      "GET /v1/chains": [() => json(503, { error: { code: "UNHEALTHY", message: "x" } }), () => ok([{ key: "solana" }])],
      "POST /v1/ids": () => json(503, { error: { code: "UNHEALTHY", message: "x" } }),
    });
    const c = client(fetchFn);
    assert.equal((await c.chains())[0]!.key, "solana");
    await assert.rejects(c.ids.claim({ name: "yoda" }), MaxApiError);
    assert.equal(calls.filter((x) => x.path === "/v1/ids").length, 1);
  });

  test("retries prepare with the same idempotency key so a retry can never double-create", async () => {
    const { fetchFn, calls } = mockFetch({
      "POST /v1/transfers/prepare": [() => json(502, { error: { code: "INTERNAL_ERROR", message: "x" } }), ({ body }) => ok({ id: "p1", key: body.idempotencyKey })],
    });
    const p: any = await client(fetchFn).transfers.prepare({ recipient: "max.yoda", chain: "solana", amount: "0.1", senderAddress: "S" });
    const keys = calls.map((c) => c.body.idempotencyKey);
    assert.equal(keys.length, 2);
    assert.equal(keys[0], keys[1]);
    assert.match(keys[0], /^[0-9a-f-]{36}$/);
    assert.equal(p.key, keys[0]);
  });

  test("does not retry 4xx", async () => {
    const { fetchFn, calls } = mockFetch({
      "GET /v1/chains": () => json(400, { error: { code: "BAD_REQUEST", message: "no" } }),
    });
    await assert.rejects(client(fetchFn).chains());
    assert.equal(calls.length, 1);
  });

  test("sends API keys as Bearer tokens", async () => {
    const { fetchFn, calls } = mockFetch({ "GET /v1/agent/me": ok({ agentId: "a" }) });
    await client(fetchFn, { apiKey: "maxk_abc_def" }).agent.me();
    assert.equal(calls[0]!.headers.get("Authorization"), "Bearer maxk_abc_def");
  });

  test("persistSession keeps the login cookie for server-side scripts and logout clears it", async () => {
    const { fetchFn, calls } = mockFetch({
      "POST /v1/wallets/challenge": ok({ nonce: "n".repeat(32), message: "sign me", expiresAt: "" }),
      "POST /v1/wallets/verify": () => ok({ purpose: "login" }, { "Set-Cookie": "max_session=tok123; Path=/; HttpOnly; SameSite=Lax" }),
      "GET /v1/auth/me": ok({ id: "acct" }),
      "POST /v1/auth/logout": () => new Response(null, { status: 204, headers: { "Set-Cookie": "max_session=; Max-Age=0" } }),
    });
    const c = client(fetchFn, { persistSession: true });
    const signed: string[] = [];
    await c.auth.login({ namespace: "solana", address: "Addr", signMessage: async (m) => (signed.push(m), "sig") });
    assert.deepEqual(signed, ["sign me"]);
    assert.equal(calls[1]!.body.signature, "sig");
    await c.auth.me();
    assert.equal(calls[2]!.headers.get("Cookie"), "max_session=tok123");
    await c.auth.logout();
    assert.equal(c.http.hasSession, false);
  });

  test("waitFor polls until a terminal status", async () => {
    const { fetchFn } = mockFetch({
      "GET /v1/transfers/p1": [() => ok({ id: "p1", status: "SUBMITTED" }), () => ok({ id: "p1", status: "CONFIRMING" }), () => ok({ id: "p1", status: "COMPLETED" })],
    });
    const seen: string[] = [];
    const p = await client(fetchFn).transfers.waitFor("p1", { intervalMs: 1, onUpdate: (x) => seen.push(x.status) });
    assert.equal(p.status, "COMPLETED");
    assert.deepEqual(seen, ["SUBMITTED", "CONFIRMING", "COMPLETED"]);
  });
});

// ---- authorizations ----

const b64 = (u: Uint8Array) => Buffer.from(u).toString("base64");

async function newMaxKey() {
  const kp = (await webcrypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])) as CryptoKeyPair;
  const raw = new Uint8Array(await webcrypto.subtle.exportKey("raw", kp.publicKey));
  const key: AuthorizationKey = { keyId: "k1", algorithm: "ed25519", publicKey: b64(raw), domain: AUTHORIZATION_DOMAIN };
  const sign = async (claims: object, keyId = "k1"): Promise<SignedAuthorization> => {
    const payload = new TextEncoder().encode(JSON.stringify(claims));
    const msg = new Uint8Array([...new TextEncoder().encode(AUTHORIZATION_DOMAIN), ...payload]);
    const sig = new Uint8Array(await webcrypto.subtle.sign({ name: "Ed25519" }, kp.privateKey, msg));
    return { algorithm: "ed25519", keyId, payload: b64(payload), signature: b64(sig) };
  };
  return { key, sign };
}

const evmClaims = (over: Partial<AuthorizationClaims> = {}): AuthorizationClaims => ({
  version: 1, paymentId: "pay-1", agentId: "a", credentialId: "c", decision: "auto_approved", policyVersion: 2,
  chain: "base", sender: "0xAbC0000000000000000000000000000000000001", payTo: "0x9999", recipient: "max.yoda",
  recipientAddress: "0x9999", route: "direct", asset: { kind: "native", decimals: 18 }, amountBaseUnits: "1000",
  instructions: { evm: { chainId: 8453, to: "0x9999999999999999999999999999999999999999", value: "0x3e8", data: "0x" } },
  issuedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 600_000).toISOString(),
  ...over,
});

describe("verifyAuthorization", () => {
  test("accepts a valid authorization and returns its claims", async () => {
    const { key, sign } = await newMaxKey();
    const claims = await verifyAuthorization(await sign(evmClaims()), [key], { paymentId: "pay-1", sender: "0xabc0000000000000000000000000000000000001" });
    assert.equal(claims.amountBaseUnits, "1000");
  });

  const rejects: [string, (m: Awaited<ReturnType<typeof newMaxKey>>) => Promise<[SignedAuthorization, AuthorizationKey[], object?]>, RegExp][] = [
    ["tampered amount", async ({ key, sign }) => {
      const sa = await sign(evmClaims());
      const tampered = Buffer.from(sa.payload, "base64").toString().replace('"1000"', '"9000"');
      return [{ ...sa, payload: Buffer.from(tampered).toString("base64") }, [key]];
    }, /signature is invalid/],
    ["signed by another key", async ({ key }) => [await (await newMaxKey()).sign(evmClaims()), [key]], /signature is invalid/],
    ["unknown key id", async ({ key, sign }) => [await sign(evmClaims(), "k9"), [key]], /no MAX key/],
    ["expired", async ({ key, sign }) => [await sign(evmClaims({ expiresAt: new Date(Date.now() - 1000).toISOString() })), [key]], /expired/],
    ["not approved", async ({ key, sign }) => [await sign(evmClaims({ decision: "pending_approval" })), [key]], /decision is pending_approval/],
    ["other payment", async ({ key, sign }) => [await sign(evmClaims()), [key], { paymentId: "pay-2" }], /different payment/],
    ["other sender", async ({ key, sign }) => [await sign(evmClaims()), [key], { sender: "0xdead" }], /different sending wallet/],
  ];
  for (const [name, build, msg] of rejects) {
    test(`rejects: ${name}`, async () => {
      const [sa, keys, opts] = await build(await newMaxKey());
      await assert.rejects(verifyAuthorization(sa, keys, opts), (e: unknown) => e instanceof AuthorizationError && msg.test(e.message));
    });
  }
});

describe("transaction matching", () => {
  const claims = evmClaims();
  test("EVM: builds and accepts the exact transaction", () => {
    const tx = evmTransactionFromClaims(claims);
    assert.equal(tx.value, 1000n);
    assertEvmTransactionMatches(claims, { ...tx, to: tx.to.toUpperCase().replace("0X", "0x") });
  });
  for (const [name, tx] of [
    ["different recipient", { to: "0x1111111111111111111111111111111111111111", value: 1000n, data: "0x", chainId: 8453 }],
    ["different value", { to: "0x9999999999999999999999999999999999999999", value: 1001n, data: "0x", chainId: 8453 }],
    ["extra calldata", { to: "0x9999999999999999999999999999999999999999", value: 1000n, data: "0xa9059cbb", chainId: 8453 }],
    ["different chain", { to: "0x9999999999999999999999999999999999999999", value: 1000n, data: "0x", chainId: 1 }],
  ] as const) {
    test(`EVM: rejects ${name}`, () => assert.throws(() => assertEvmTransactionMatches(claims, tx), AuthorizationError));
  }

  const spl = evmClaims({
    instructions: { solana: { kind: "spl_transfer_checked", to: "Owner", mint: "Mint", tokenProgram: "Tok", decimals: 5, amount: "100", destinationTokenAccount: "Ata" } },
  });
  test("Solana: accepts the authorized SPL transfer", () => {
    assertSolanaTransferMatches(spl, { kind: "spl_transfer_checked", mint: "Mint", tokenProgram: "Tok", amount: 100n, decimals: 5, destinationTokenAccount: "Ata" });
  });
  test("Solana: rejects a different destination account", () => {
    assert.throws(() => assertSolanaTransferMatches(spl, { kind: "spl_transfer_checked", mint: "Mint", amount: 100n, destinationTokenAccount: "Evil" }), AuthorizationError);
  });
});

describe("PolicySigner", () => {
  test("verifies before signing and submits the signed hash", async () => {
    const { key, sign } = await newMaxKey();
    const signed = await sign(evmClaims());
    const { fetchFn, calls } = mockFetch({
      "GET /v1/agent/transfers/pay-1/authorization": ok(signed),
      "GET /v1/agent-authorizations/keys": ok([key]),
      "POST /v1/transfers/pay-1/submit": ({ body }) => ok({ id: "pay-1", status: "SUBMITTED", txHash: body.txHash }),
    });
    const signer = new PolicySigner({ client: client(fetchFn, { apiKey: "maxk_x_y" }), sender: "0xabc0000000000000000000000000000000000001" });
    const p = await signer.execute("pay-1", async (claims) => {
      assertEvmTransactionMatches(claims, evmTransactionFromClaims(claims));
      return "0xhash";
    });
    assert.equal(p.txHash, "0xhash");
    assert.deepEqual(calls.map((c) => c.method + " " + c.path), [
      "GET /v1/agent/transfers/pay-1/authorization",
      "GET /v1/agent-authorizations/keys",
      "POST /v1/transfers/pay-1/submit",
    ]);
  });

  test("never calls the signing function when verification fails", async () => {
    const { key } = await newMaxKey();
    const forged = await (await newMaxKey()).sign(evmClaims());
    const { fetchFn, calls } = mockFetch({
      "GET /v1/agent/transfers/pay-1/authorization": ok(forged),
      "GET /v1/agent-authorizations/keys": ok([key]),
    });
    let signedCalled = false;
    const signer = new PolicySigner({ client: client(fetchFn), sender: "0xabc0000000000000000000000000000000000001" });
    await assert.rejects(signer.execute("pay-1", async () => ((signedCalled = true), "0x")), AuthorizationError);
    assert.equal(signedCalled, false);
    assert.ok(!calls.some((c) => c.path.endsWith("/submit")));
  });

  test("leaves pending proposals unsigned unless asked to wait", async () => {
    const { fetchFn } = mockFetch({
      "POST /v1/agent/transfers": ok({ payment: { id: "pay-9" }, authorization: { decision: "pending_approval", reasons: ["over limit"] } }),
    });
    const signer = new PolicySigner({ client: client(fetchFn), sender: "S" });
    const res = await signer.proposeAndExecute({ recipient: "max.yoda", chain: "solana", amount: "5" }, async () => {
      throw new Error("must not sign");
    });
    assert.equal(res.transfer.authorization.decision, "pending_approval");
    assert.equal(res.payment, undefined);
  });
});

describe("default MAX IDs", () => {
  test("me() exposes defaultMaxId and primaryMaxId", async () => {
    const def = { id: "id_1", handle: "max.k7m2x9p4q8r3t5vz" };
    const { fetchFn } = mockFetch({
      "GET /v1/auth/me": ok({ id: "a1", type: "human", status: "active", createdAt: "2026-01-01T00:00:00Z", defaultMaxId: def, primaryMaxId: def }),
    });
    const me = await client(fetchFn).auth.me();
    assert.deepEqual(me.defaultMaxId, def);
    assert.equal(me.primaryMaxId?.handle, def.handle);
  });

  test("ids.list carries isDefault; renaming a default surfaces DEFAULT_ID_LOCKED", async () => {
    const { fetchFn } = mockFetch({
      "GET /v1/ids": ok([{ id: "id_1", name: "yoda", isDefault: false }, { id: "id_2", name: "k7m2x9p4q8r3t5vz", isDefault: true }]),
      "POST /v1/ids/id_2/rename": json(409, { error: { code: "DEFAULT_ID_LOCKED", message: "default IDs cannot be renamed" } }),
    });
    const c = client(fetchFn);
    const ids = await c.ids.list();
    assert.deepEqual(ids.map((i) => i.isDefault), [false, true]);
    await assert.rejects(
      c.renames.request("id_2", { newName: "other", feeChain: "base", senderAddress: "0xabc" }),
      (e: unknown) => e instanceof MaxApiError && e.code === "DEFAULT_ID_LOCKED",
    );
  });
});

describe("units", () => {
  test("parseUnits / formatUnits round-trip without floats", () => {
    assert.equal(parseUnits("0.1", 9), 100_000_000n);
    assert.equal(parseUnits("1.50", 6), 1_500_000n);
    assert.equal(parseUnits("9007199254.740993", 6), 9007199254740993n);
    assert.equal(formatUnits(100_000_000n, 9), "0.1");
    assert.equal(formatUnits("1", 6), "0.000001");
    assert.throws(() => parseUnits("0.0000001", 6));
    assert.throws(() => parseUnits("1e6", 6));
  });
  test("normalizeName mirrors the API", () => {
    assert.equal(normalizeName("MAX.Yoda"), "yoda");
    assert.equal(normalizeName("research-bot"), "research-bot");
    for (const bad of ["yo", "yo--da", "-yoda", "yöda", "yо da"]) assert.equal(normalizeName(bad), null);
  });
});

describe("native sessions", () => {
  test("login with tokenDelivery body stores the token as a Bearer for later calls", async () => {
    const { fetchFn, calls } = mockFetch({
      "POST /v1/wallets/challenge": ok({ nonce: "n".repeat(32), message: "m", expiresAt: "" }),
      "POST /v1/wallets/verify": ({ body }) => ok({ purpose: "login", sessionToken: body.tokenDelivery === "body" ? "tok-native" : undefined }),
      "GET /v1/auth/me": ok({ id: "a" }),
    });
    const c = client(fetchFn);
    const res = await c.auth.login({ namespace: "solana", address: "A", signMessage: async () => "s" }, { tokenDelivery: "body" });
    assert.equal(res.sessionToken, "tok-native");
    await c.auth.me();
    assert.equal(calls[2]!.headers.get("Authorization"), "Bearer tok-native");
  });
});
