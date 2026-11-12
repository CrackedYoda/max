These are bounded local reproductions of the reviewed behavior. They assert that the defect exists, so PASS does not mean secure. No actual funds, wallets, network broadcasts, or production database are used.

From the repository root, after `cd max-sdk && npm test`, run:

```sh
node security-review/reproductions/signer-concurrency.mjs
```

For the Go reproductions, copy each `.go.txt` file into its matching package as `security_review_test.go`:

- payments → `max-core/internal/payments/`
- holdings → `max-core/internal/holdings/`
- shared → `max-core/internal/shared/`

From `max-core`, run:

```sh
go test ./internal/payments ./internal/holdings ./internal/shared -run SecurityReview -v
```

Remove only those temporary test files after use. They are stored as text artifacts to avoid adding tests that enshrine insecure behavior to the normal suite. Once fixed, replace their assertions with the intended rejection, eviction, or single-execution properties.
