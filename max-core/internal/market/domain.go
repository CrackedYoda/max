// Package market holds the MAX ID marketplace domain: pure rules shared by the
// listing, offer and sale services. Nothing here touches the DB or HTTP.
package market

// Listing statuses (spec §4 market_listings.status).
const (
	ListingActive        = "active"
	ListingLocked        = "locked"
	ListingSold          = "sold"
	ListingCancelled     = "cancelled"
	ListingAdminDelisted = "admin_delisted"
)

// Offer statuses (spec §4 market_offers.status).
const (
	OfferLive       = "live"
	OfferUnfunded   = "unfunded"
	OfferAccepted   = "accepted"
	OfferDeclined   = "declined"
	OfferSuperseded = "superseded"
	OfferExpired    = "expired"
	OfferWithdrawn  = "withdrawn"
	OfferLapsed     = "lapsed"
	OfferCompleted  = "completed"
)

// Sale statuses (spec §4 market_sales.status).
const (
	SalePendingReview = "pending_review"
	SaleLocked        = "locked"
	SaleSubmitted     = "submitted"
	SaleConfirming    = "confirming"
	SaleCompleted     = "completed"
	SaleExpired       = "expired"
	SaleCancelled     = "cancelled"
	SaleManualReview  = "manual_review"
	SaleReversed      = "reversed"
)

// IsOpenSale reports whether a sale status blocks listing changes and edits
// to the name (spec §3).
func IsOpenSale(status string) bool {
	switch status {
	case SalePendingReview, SaleLocked, SaleSubmitted, SaleConfirming:
		return true
	}
	return false
}
