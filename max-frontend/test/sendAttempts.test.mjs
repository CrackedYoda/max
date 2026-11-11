// security-review S03: after an ambiguous wallet failure the web flows used to
// sign a fresh transaction. These tests drive the send/recovery engine with
// fake wallets and chains where the broadcast succeeds and the response is lost.
import assert from "node:assert/strict";
import { beforeEach, describe, test } from "node:test";

// A localStorage stand-in shared by "page loads" (fresh module instances).
const store = new Map();
globalThis.localStorage = {
  get length() { return store.size; },
  key: (i) => [...store.keys()][i] ?? null,
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => void store.set(k, String(v)),
  removeItem: (k) => void store.delete(k),
};

let load = 0;
const freshPage = () => import(`../src/services/sendAttempts.ts?page=${++load}`);
const meta = (paymentId) => ({ paymentId, chain: "base", summary: "1 ETH to max.yoda", purpose: { kind: "send" } });
const SENDER = "0xAbC0000000000000000000000000000000000001";
const RECIPIENT = "0x0000000000000000000000000000000000000bob";

/** An EVM chain plus an EIP-1193 wallet whose sends can land and then "lose" the response. */
function fakeEvm({ loseResponse = false, reject = false } = {}) {
  const chain = { head: 100, nonce: 7, pending: 7, blocks: new Map() };
  const sends = [];
  const provider = {
    async request({ method, params }) {
      switch (method) {
        case "eth_getTransactionCount": return "0x" + (params[1] === "pending" ? chain.pending : chain.nonce).toString(16);
        case "eth_blockNumber": return "0x" + chain.head.toString(16);
        case "eth_sendTransaction": {
          if (reject) throw Object.assign(new Error("User rejected the request."), { code: 4001 });
          const t = params[0];
          const tx = { hash: `0xhash${sends.length + 1}`, from: t.from, nonce: chain.pending, to: t.to, value: t.value, input: t.data };
          sends.push(tx);
          chain.head += 1;
          chain.blocks.set(chain.head, [tx]);
          chain.nonce += 1;
          chain.pending += 1;
          if (loseResponse) throw Object.assign(new Error("Internal JSON-RPC error: request rejected by upstream"), { code: -32603 });
          return tx.hash;
        }
      }
      throw new Error(method);
    },
  };
  const reader = {
    nonce: async (_a, tag) => (tag === "pending" ? chain.pending : chain.nonce),
    blockNumber: async () => chain.head,
    blockTransactions: async (n) => chain.blocks.get(n) ?? [],
  };
  return { chain, sends, provider, reader };
}

const evmTerms = (id) => ({ ...meta(id), sender: SENDER, chainId: 8453, to: RECIPIENT, value: "0xde0b6b3a7640000", data: "0x" });

/** A Solana cluster and signer; the broadcast can land and then throw. */
function fakeSolana({ loseResponse = false } = {}) {
  const chain = { height: 1000, landed: new Set() };
  let signs = 0;
  const raws = [];
  const steps = {
    sign: async () => ({ signature: `sig${++signs}`, raw: `raw${signs}`, lastValidBlockHeight: chain.height + 150 }),
    sendRaw: async (raw) => {
      raws.push(raw);
      chain.landed.add(raw.replace("raw", "sig"));
      if (loseResponse) throw new Error("fetch failed");
    },
  };
  const reader = {
    hasSignature: async (sig) => chain.landed.has(sig),
    blockHeight: async () => chain.height,
    sendRaw: steps.sendRaw,
  };
  return { chain, steps, reader, raws, signs: () => signs };
}

beforeEach(() => store.clear());

describe("EVM", () => {
  test("broadcast succeeded, response lost: no second send, recovery finds the hash", async () => {
    const m = await freshPage();
    const evm = fakeEvm({ loseResponse: true });
    const first = await m.sendEvmOnce(evmTerms("pay-1"), evm.provider);
    assert.equal(first.kind, "unknown");

    // The person presses Send again, even after a reload.
    const page2 = await freshPage();
    const retry = await page2.sendEvmOnce(evmTerms("pay-1"), evm.provider);
    assert.equal(retry.kind, "pending");
    assert.equal(evm.sends.length, 1, "the wallet was asked to send only once");

    const check = await page2.checkEvmAttempt(page2.loadAttempt("pay-1"), evm.reader);
    assert.deepEqual(check, { kind: "landed", txHash: "0xhash1" });
  });

  test("an error that only says 'rejected' after a broadcast is not a rejection", async () => {
    const m = await freshPage();
    const evm = fakeEvm({ loseResponse: true }); // message contains "rejected", code is not 4001
    assert.equal((await m.sendEvmOnce(evmTerms("pay-1"), evm.provider)).kind, "unknown");
    assert.ok(m.loadAttempt("pay-1"));
  });

  test("a user rejection clears the attempt and allows a fresh send", async () => {
    const m = await freshPage();
    const evm = fakeEvm({ reject: true });
    assert.equal((await m.sendEvmOnce(evmTerms("pay-1"), evm.provider)).kind, "not_sent");
    assert.equal(m.loadAttempt("pay-1"), null);
  });

  test("a successful send keeps its hash until MAX records it", async () => {
    const m = await freshPage();
    const evm = fakeEvm();
    assert.deepEqual(await m.sendEvmOnce(evmTerms("pay-1"), evm.provider), { kind: "sent", txHash: "0xhash1" });
    assert.equal((await freshPage()).loadAttempt("pay-1").txHash, "0xhash1");
  });

  test("a nonce that never moves means nothing was sent, but only after the quiet period", async () => {
    const m = await freshPage();
    const evm = fakeEvm();
    const t0 = 1_000_000;
    const a = m.saveAttempt({ ...evmTerms("pay-1"), namespace: "evm", nonce: 7, fromBlock: 100, createdAt: t0 });
    assert.equal((await m.checkEvmAttempt(a, evm.reader, t0 + 60_000)).kind, "pending");
    evm.chain.pending = 8; // waiting in the mempool
    assert.equal((await m.checkEvmAttempt(a, evm.reader, t0 + m.EVM_QUIET_MS)).kind, "pending");
    evm.chain.pending = 7;
    assert.equal((await m.checkEvmAttempt(a, evm.reader, t0 + m.EVM_QUIET_MS)).kind, "not_sent");
  });

  test("a nonce used by some other transaction means this payment was not sent", async () => {
    const m = await freshPage();
    const evm = fakeEvm();
    const a = m.saveAttempt({ ...evmTerms("pay-1"), namespace: "evm", nonce: 7, fromBlock: 100, createdAt: Date.now() });
    evm.chain.head = 101;
    evm.chain.nonce = evm.chain.pending = 8;
    evm.chain.blocks.set(101, [{ hash: "0xother", from: SENDER.toLowerCase(), nonce: 7, to: "0xelsewhere", value: "0x1", input: "0x" }]);
    assert.deepEqual(await m.checkEvmAttempt(a, evm.reader), { kind: "not_sent" });
  });
});

describe("Solana", () => {
  test("broadcast succeeded, response lost: same bytes only, recovery finds the signature", async () => {
    const m = await freshPage();
    const sol = fakeSolana({ loseResponse: true });
    const first = await m.sendSolanaOnce(meta("pay-2"), sol.steps);
    assert.equal(first.kind, "unknown");
    assert.equal(first.attempt.txHash, "sig1", "the signature is known before broadcasting");

    const page2 = await freshPage();
    assert.equal((await page2.sendSolanaOnce(meta("pay-2"), sol.steps)).kind, "pending");
    assert.equal(sol.signs(), 1, "signed only once");

    assert.deepEqual(await page2.checkSolanaAttempt(page2.loadAttempt("pay-2"), sol.reader), { kind: "landed", txHash: "sig1" });
  });

  test("a broadcast that never landed is resent with the same bytes until the blockhash expires", async () => {
    const m = await freshPage();
    const sol = fakeSolana();
    sol.steps.sendRaw = async (raw) => {
      sol.raws.push(raw);
      throw new Error("fetch failed"); // never reaches the cluster
    };
    sol.reader.sendRaw = sol.steps.sendRaw;
    const first = await m.sendSolanaOnce(meta("pay-2"), sol.steps);
    assert.equal(first.kind, "unknown");

    assert.equal((await m.checkSolanaAttempt(first.attempt, sol.reader)).kind, "pending");
    assert.deepEqual(sol.raws, ["raw1", "raw1"]);

    sol.chain.height = first.attempt.lastValidBlockHeight + 1;
    assert.equal((await m.checkSolanaAttempt(first.attempt, sol.reader)).kind, "not_sent");
    assert.equal(sol.signs(), 1);
  });

  test("a failed signature request sent nothing and saves no attempt", async () => {
    const m = await freshPage();
    const r = await m.sendSolanaOnce(meta("pay-2"), { sign: async () => { throw Object.assign(new Error("User rejected"), { code: 4001 }); }, sendRaw: async () => assert.fail() });
    assert.equal(r.kind, "not_sent");
    assert.equal(m.loadAttempt("pay-2"), null);
  });
});

test("attempts are listed per purpose for the recovery screen", async () => {
  const m = await freshPage();
  m.saveAttempt({ ...meta("a"), namespace: "solana", txHash: "s", raw: "r", lastValidBlockHeight: 1, createdAt: 2 });
  m.saveAttempt({ ...meta("b"), purpose: { kind: "rename", maxId: "id1", requestId: "rq" }, namespace: "solana", txHash: "s", raw: "r", lastValidBlockHeight: 1, createdAt: 1 });
  const page2 = await freshPage();
  assert.deepEqual(page2.listAttempts((p) => p.kind === "send").map((a) => a.paymentId), ["a"]);
  assert.deepEqual(page2.listAttempts((p) => p.kind === "rename" && p.maxId === "id1").map((a) => a.paymentId), ["b"]);
});
