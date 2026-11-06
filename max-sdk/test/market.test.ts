import assert from "node:assert/strict";
import { test, describe } from "node:test";

import { MaxClient, MaxApiError, isPendingApproval, type SaleView } from "../src/index.js";

interface Call { method: string; path: string; headers: Headers; body: any }

function mock(status: number, data: unknown) {
  const calls: Call[] = [];
  const fetchFn = (async (input: string, init: RequestInit = {}) => {
    const url = new URL(input);
    calls.push({ method: init.method ?? "GET", path: url.pathname + url.search, headers: new Headers(init.headers), body: init.body ? JSON.parse(String(init.body)) : undefined });
    if (status === 204) return new Response(null, { status });
    return new Response(JSON.stringify(status >= 400 ? data : { data }), { status, headers: { "Content-Type": "application/json" } });
  }) as unknown as typeof fetch;
  const c = new MaxClient({ baseUrl: "http://api.test", fetch: fetchFn, maxRetries: 0 });
  return { c, calls };
}

const ID = "11111111-1111-4111-8111-111111111111";

describe("market routes", () => {
  const cases: { name: string; status?: number; run: (c: MaxClient) => Promise<unknown>; method: string; path: string; body?: unknown }[] = [
    { name: "search", run: (c) => c.market.search({ q: "yo", minPrice: "5", sort: "price_asc", limit: 10 }), method: "GET", path: "/v1/market/listings?q=yo&minPrice=5&sort=price_asc&limit=10" },
    { name: "name", run: (c) => c.market.name("max.yoda"), method: "GET", path: "/v1/market/names/max.yoda" },
    { name: "eligibility", run: (c) => c.market.eligibility(ID), method: "GET", path: `/v1/market/eligibility/${ID}` },
    { name: "list", run: (c) => c.market.list({ maxId: ID, price: "250", chain: "solana", asset: "mint", payoutAddress: "pay" }), method: "POST", path: "/v1/market/listings", body: { maxId: ID, price: "250", chain: "solana", asset: "mint", payoutAddress: "pay" } },
    { name: "updatePrice", run: (c) => c.market.updatePrice(ID, "300"), method: "PATCH", path: `/v1/market/listings/${ID}`, body: { price: "300" } },
    { name: "delist", status: 204, run: (c) => c.market.delist(ID), method: "DELETE", path: `/v1/market/listings/${ID}` },
    { name: "offer", run: (c) => c.market.offer({ name: "yoda", amount: "10", chain: "solana", asset: "mint", fromAddress: "b" }), method: "POST", path: "/v1/market/offers", body: { name: "yoda", amount: "10", chain: "solana", asset: "mint", fromAddress: "b" } },
    { name: "withdrawOffer", status: 204, run: (c) => c.market.withdrawOffer(ID), method: "DELETE", path: `/v1/market/offers/${ID}` },
    { name: "accept", run: (c) => c.market.accept(ID, { payoutAddress: "p" }), method: "POST", path: `/v1/market/offers/${ID}/accept`, body: { payoutAddress: "p" } },
    { name: "decline", status: 204, run: (c) => c.market.decline(ID, { note: "no" }), method: "POST", path: `/v1/market/offers/${ID}/decline`, body: { note: "no" } },
    { name: "pay", run: (c) => c.market.pay(ID, { idempotencyKey: "key-12345" }), method: "POST", path: `/v1/market/offers/${ID}/pay`, body: { idempotencyKey: "key-12345" } },
    { name: "submit", run: (c) => c.market.submit(ID, "sig"), method: "POST", path: `/v1/market/sales/${ID}/submit`, body: { txHash: "sig" } },
    { name: "getSale", run: (c) => c.market.getSale(ID), method: "GET", path: `/v1/market/sales/${ID}` },
    { name: "me", run: (c) => c.market.me(), method: "GET", path: "/v1/market/me" },
    { name: "agents.marketAuthorizations", run: (c) => c.agents.marketAuthorizations(ID, { decision: "pending_approval" }), method: "GET", path: `/v1/agents/${ID}/market-authorizations?decision=pending_approval` },
    { name: "agents.approveMarket", run: (c) => c.agents.approveMarket(ID), method: "POST", path: `/v1/agent-approvals/market/${ID}/approve` },
    { name: "agents.denyMarket", run: (c) => c.agents.denyMarket(ID), method: "POST", path: `/v1/agent-approvals/market/${ID}/deny` },
    { name: "admin.marketListings", run: (c) => c.admin.marketListings({ status: "active", limit: 5 }), method: "GET", path: "/v1/admin/market/listings?status=active&limit=5" },
    { name: "admin.marketOffers", run: (c) => c.admin.marketOffers({ accountId: ID }), method: "GET", path: `/v1/admin/market/offers?accountId=${ID}` },
    { name: "admin.marketSales", run: (c) => c.admin.marketSales({ name: "yoda" }), method: "GET", path: "/v1/admin/market/sales?name=yoda" },
    { name: "admin.marketDelist", run: (c) => c.admin.marketDelist(ID, "spam"), method: "POST", path: `/v1/admin/market/listings/${ID}/delist`, body: { reason: "spam" } },
    { name: "admin.marketApproveSale", run: (c) => c.admin.marketApproveSale(ID), method: "POST", path: `/v1/admin/market/sales/${ID}/approve`, body: {} },
    { name: "admin.marketDenySale", run: (c) => c.admin.marketDenySale(ID, "r"), method: "POST", path: `/v1/admin/market/sales/${ID}/deny`, body: { reason: "r" } },
    { name: "admin.marketCancelSale", run: (c) => c.admin.marketCancelSale(ID, "r"), method: "POST", path: `/v1/admin/market/sales/${ID}/cancel`, body: { reason: "r" } },
    { name: "admin.marketReverseSale", run: (c) => c.admin.marketReverseSale(ID, "r"), method: "POST", path: `/v1/admin/market/sales/${ID}/reverse`, body: { reason: "r" } },
    { name: "admin.marketRestrictions", run: (c) => c.admin.marketRestrictions({ accountId: ID, all: true }), method: "GET", path: `/v1/admin/market/restrictions?accountId=${ID}&all=true` },
    { name: "admin.marketRestrict", run: (c) => c.admin.marketRestrict({ accountId: ID, kind: "ban", reason: "x" }), method: "POST", path: "/v1/admin/market/restrictions", body: { accountId: ID, kind: "ban", reason: "x" } },
    { name: "admin.marketLiftRestriction", run: (c) => c.admin.marketLiftRestriction(ID, "ok"), method: "DELETE", path: `/v1/admin/market/restrictions/${ID}`, body: { reason: "ok" } },
  ];
  for (const t of cases) {
    test(`${t.name} -> ${t.method} ${t.path}`, async () => {
      const { c, calls } = mock(t.status ?? 200, { ok: true });
      await t.run(c);
      assert.equal(calls.length, 1);
      assert.equal(calls[0]!.method, t.method);
      assert.equal(calls[0]!.path, t.path);
      assert.deepEqual(calls[0]!.body, t.body);
    });
  }

  test("buy sends a generated idempotency key and is retried with the same key", async () => {
    const bodies: any[] = [];
    let n = 0;
    const fetchFn = (async (_i: string, init: RequestInit) => {
      bodies.push(JSON.parse(String(init.body)));
      return ++n === 1
        ? new Response("{}", { status: 503, headers: { "Content-Type": "application/json" } })
        : new Response(JSON.stringify({ data: { sale: { id: "s1" } } }), { status: 201, headers: { "Content-Type": "application/json" } });
    }) as unknown as typeof fetch;
    const c = new MaxClient({ baseUrl: "http://api.test", fetch: fetchFn, maxRetries: 2 });
    const r = (await c.market.buy(ID, { fromAddress: "b" })) as SaleView;
    assert.equal(r.sale.id, "s1");
    assert.equal(bodies.length, 2);
    assert.ok(bodies[0].idempotencyKey.length >= 8);
    assert.equal(bodies[0].idempotencyKey, bodies[1].idempotencyKey);
    assert.equal(bodies[0].fromAddress, "b");
  });

  test("a 202 pending_approval is a typed result, not an error", async () => {
    const pending = { status: "pending_approval", authorizationId: "auth-1", reasons: ["over_limit"] };
    const { c } = mock(202, pending);
    const calls = [
      c.market.buy(ID, { fromAddress: "b" }),
      c.market.list({ maxId: ID, price: "1", chain: "solana", asset: "m", payoutAddress: "p" }),
      c.market.updatePrice(ID, "2"),
      c.market.offer({ name: "yoda", amount: "1", chain: "solana", asset: "m", fromAddress: "b" }),
      c.market.accept(ID),
      c.market.pay(ID),
    ];
    for (const r of await Promise.all(calls)) {
      assert.ok(isPendingApproval(r));
      if (isPendingApproval(r)) {
        assert.equal(r.authorizationId, "auth-1");
        assert.deepEqual(r.reasons, ["over_limit"]);
      }
    }
  });

  test("errors surface as MaxApiError with the market code", async () => {
    const { c } = mock(409, { error: { code: "LISTING_NOT_ACTIVE", message: "nope" } });
    await assert.rejects(c.market.buy(ID, { fromAddress: "b" }), (e: unknown) => e instanceof MaxApiError && e.code === "LISTING_NOT_ACTIVE" && e.status === 409);
  });

  test("path segments are encoded", async () => {
    const { c, calls } = mock(200, {});
    await c.market.name("a/b");
    assert.equal(calls[0]!.path, "/v1/market/names/a%2Fb");
  });
});
