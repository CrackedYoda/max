// Tests assign `fakeMax.transfers` etc.; services call max().
export const fakeMax = { transfers: {}, auth: {}, wallets: {}, http: { setSessionToken() {}, clearSession() {} } };
export const max = () => fakeMax;
