# MAX app

Self-custody mobile wallet for MAX: create or import a wallet, claim a `max.` handle (including X-verified reserved names), rename it for a fee, send to names, receive by QR. Built with Expo SDK 58 (Expo Router, React Native 0.88) from the "MAX Web Frontend" canvas app artboards, following the Noma app's architecture and security patterns.

## Run it

```bash
npm install
cp .env.example .env        # API URL, web URL, RPCs
npx expo prebuild -p ios    # generates ios/ (never edit it by hand)
npx expo run:ios --port 8082  # dev build; Expo Go lacks the Face ID keychain entitlement
```

If CocoaPods fails with `Unicode Normalization not appropriate for ASCII-8BIT`, run it with `LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8`.

The app talks to `max-core` (`EXPO_PUBLIC_MAX_API_URL`, `http://localhost:8080` in dev). Networks come from `GET /v1/chains`. Balances and broadcasting go through the API's `POST /v1/rpc/:chain` proxy, so RPC provider keys stay on the server and never ship in the app.

Marketplace sales settle in USDC on Solana. `EXPO_PUBLIC_MARKET_ASSETS` (`chain:mint:decimals`, mirroring max-core `MARKET_ASSETS`) pins each asset's decimals; before signing, the app converts the price the user saw with those decimals (cross-checked against the mint through its own RPC) and verifies the sale's instructions with `assertSaleInstructionsMatch`. Optional `EXPO_PUBLIC_MARKET_TREASURY` pins the MAX Solana treasury (max-core `TREASURY_SOLANA`): when set, a sale whose fee goes elsewhere is refused before signing. After an uncertain send the app re-sends the same signed bytes, and offers a new signature only once the first one's blockhash has expired.

Checks: `npx tsc --noEmit`, `npx expo lint`, `npx expo-doctor`.

## Layout

```
src/app/                 routes (Expo Router)
  _layout.tsx            fonts, providers, Stack.Protected guards, lock overlay
  index.tsx              anchor: → /welcome or /home
  (onboarding)/          welcome, backup, verify, import, passcode
  (app)/(tabs)/          home, send, receive, market, ids
  (app)/                 claim, review (sign sheet), scan, destination, edit-id,
                         reveal, export-key, security, add-account,
                         rename (paid rename), verify (reserved names via X),
                         sell (list / reprice / delist), market/[name] (buy, offer),
                         market/offers (listings, offers, sales),
                         verify/x (X deep-link landing, Android)
src/services/
  secure.ts              the ONLY SecureStore module
  passcode.ts            salted verifier, escalating lockout, Face ID
  keys.ts                BIP-39 / BIP-32 / SLIP-10 derivation, SDK signers
  vault.ts               in-memory accounts while unlocked
  session.ts             boot, unlock/lock, MAX sign-in, step-up, removal
  chain.ts               balances, fees, sign + broadcast
  xVerify.ts             X OAuth round trip (auth session → max://verify/x → complete)
src/store/               zustand: session status, memory-only flow drafts
src/hooks/               React Query hooks, app lock, secure screen
src/components/          ui primitives (Noma-style), auth, wallet, payments
```

## Security model

- **Keys stay on the phone.** The recovery phrase and imported keys are stored in the iOS Keychain / Android Keystore as `WHEN_UNLOCKED_THIS_DEVICE_ONLY`. They are not in backups and can't be read while the phone is locked. Keys are loaded into memory only after the passcode or Face ID check, and dropped on lock.
- **Derivation matches Phantom and MetaMask.** EVM uses `m/44'/60'/0'/0/0`; Solana uses `m/44'/501'/0'/0'` (SLIP-10). This was checked against the standard `abandon … about` test vectors.
- **Passcode.** Only a salted SHA-256 verifier is stored. After 5 wrong tries there is an escalating lockout (30 s up to 15 min). Unlike Noma, wrong tries never wipe the wallet, because with self-custody a wipe could destroy funds.
- **Face ID** reads a random secret stored behind `requireAuthentication` (its own keychain service). If biometric enrollment changes, Face ID is turned off and the passcode is required.
- **Step-up.** Signing a payment, revealing the phrase, exporting a key, adding or removing accounts, and removing the wallet all ask for Face ID or the passcode again. The reveal and export screens run that check themselves, so no route into them skips it.
- **App lock.** The app locks on background and after 5 minutes of inactivity. A privacy cover and the iOS app-switcher blur hide content. Screen capture is blocked on screens that show secrets.
- **Signing.** The phone signs exactly MAX's `instructions` for the prepared payment. That payment is locked to a fresh resolution and `resolutionVersion`, with one idempotency key per exact set of terms. The tx hash is computed **before** broadcasting and the signed bytes are kept. A dropped connection therefore leads to "checking, won't send twice": retries re-broadcast the same bytes and re-submit the same hash, and the app never signs a second transaction.
- **Renames.** Wallet & IDs → **Rename** on an ID card. The app checks cooldown and fee options (`renames.eligibility`), checks the new name, asks for Face ID or the passcode, reserves the name (`renames.request`, one idempotency key per exact terms), then signs the fee payment on the phone and broadcasts it. It records the payment with `transfers.submit` and polls `renames.get` until the rename leaves `awaiting_payment`. The fee follows the same signing rule as payments: it is signed once and retries reuse the kept bytes. If the app closed after reserving but before paying, reopening Rename offers to pay the reserved fee. Only fee networks this phone has an RPC and an account for are offered. The ID's own destination account pays when it is on the phone.
- **Reserved names (X).** Wallet & IDs → **Reserved names & X**, or the link on Claim when a name is reserved for an X account. iOS runs X OAuth in `ASWebAuthenticationSession` (`expo-web-browser`), which cannot carry the app's MAX token. So the app starts a *native* flow (`verifications.startX({ client: "native" })`). MAX's callback then forwards `state` + `code` to `max://verify/x` without completing anything. The app finishes with `POST /v1/verifications/x/complete` (`verifications.completeX`), using the same session that started the flow. An app that intercepts the deep link can't complete it: the state is bound to the starting session and PKCE keeps the verifier on the server. The deep link is set by `X_NATIVE_RESULT_REDIRECT` in max-core (default `max://verify/x`). Without X credentials (`X_CLIENT_ID` empty), starting returns `X_VERIFICATION_DISABLED` and the screen says so.
- **MAX session.** The app signs in by signing a SIWE/SIWS challenge with an on-device key. It uses `tokenDelivery: "body"` and stores the token in SecureStore. Because the phone holds the keys, an expired session is renewed silently. Every key on the phone is linked to the account.
- Secrets never go into route params or AsyncStorage (lint-enforced). `Math.random` is banned in favour of `expo-crypto`.

## Deviations from the artboards

- **No fiat totals.** The home card shows your largest native balance instead of a dollar total, because MAX has no price feed.
- **Passcode is entered twice** to confirm it.
- **Scan** reads MAX profile QR codes and opens Send pre-filled. It never pays directly.
- **"Add or import account"** imports a private key only (one recovery phrase per phone).
