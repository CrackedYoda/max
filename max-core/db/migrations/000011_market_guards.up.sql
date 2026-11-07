-- Marketplace guards after the final review (spec §6, §7):
-- a buyer who lets a buy-now sale expire unpaid waits before buying that
-- listing again; the worker finds stale reviews and payments submitted
-- after an admin cancel through the indexes below.
CREATE TABLE market_buy_cooldowns (
    buyer_account_id  uuid NOT NULL REFERENCES accounts(id),
    listing_id        uuid NOT NULL REFERENCES market_listings(id),
    sale_id           uuid NOT NULL REFERENCES market_sales(id),
    until             timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (buyer_account_id, listing_id)
);

CREATE INDEX market_sales_review_idx ON market_sales (created_at) WHERE status = 'pending_review';
CREATE INDEX market_sales_cancelled_tx_idx ON market_sales (next_check_at)
    WHERE status = 'cancelled' AND tx_hash IS NOT NULL AND next_check_at IS NOT NULL;
CREATE INDEX market_sales_buyer_open_idx ON market_sales (buyer_account_id)
    WHERE status IN ('pending_review', 'locked', 'submitted', 'confirming');
