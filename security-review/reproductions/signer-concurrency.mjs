// Run after `cd max-sdk && npm test`. No RPC or actual signing occurs.
import assert from 'node:assert/strict';
import { PolicySigner } from '../../max-sdk/build-test/src/signer.js';
let signCalls = 0;
let reads = 0;
let release;
const barrier = new Promise(r => { release = r; });
const claims = { paymentId: 'same-approved-payment', sender: 'same-wallet' };
const client = { agent: {
  verifiedClaims: async () => { if (++reads === 2) release(); await barrier; return claims; },
  submit: async (_id, hash) => ({ txHash: hash }),
}};
const signer = new PolicySigner({ client, sender: 'same-wallet' });
const sign = async () => `distinct-transaction-${++signCalls}`;
await Promise.all([signer.execute(claims.paymentId, sign), signer.execute(claims.paymentId, sign)]);
assert.equal(signCalls, 2);
console.log('CONFIRMED: two concurrent execute calls invoke signing twice for one approved payment. A real submit conflict occurs after broadcasting.');
