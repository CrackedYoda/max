package market

import "time"

// Reason codes returned by Listable.
const (
	ReasonNotActive      = "not_active"
	ReasonOwnerSuspended = "owner_suspended"
	ReasonReservedClaim  = "reserved_claim"
	ReasonHoldingPeriod  = "holding_period"
	ReasonRenameCooldown = "rename_cooldown"
	ReasonRenamePending  = "rename_pending"
	ReasonMarketBanned   = "market_banned"
	// ReasonPayoutWalletRemoved: the listing's payout address is no longer
	// an active wallet of the seller, so a buy or accept would pay a stranger.
	ReasonPayoutWalletRemoved = "payout_wallet_removed"
	// ReasonUnavailable replaces moderation reasons in public views.
	ReasonUnavailable = "unavailable"
)

// PublicReason hides moderation outcomes (bans, account suspension) and the
// seller's wallet changes from anyone but the owner.
func PublicReason(reason string) string {
	switch reason {
	case ReasonMarketBanned, ReasonOwnerSuspended, ReasonPayoutWalletRemoved:
		return ReasonUnavailable
	}
	return reason
}

// NameFacts are the stored facts about a MAX ID needed to decide listability.
type NameFacts struct {
	Status, OwnerStatus  string
	HasLiveReservedClaim bool
	RenamePending        bool
	ListableAt           time.Time
	RenameAvailableAt    time.Time
	OwnerBanned          bool
}

// Listable reports whether a name may be listed or sold at now (spec §3). On
// failure it returns the first failing reason in a fixed order.
func Listable(f NameFacts, now time.Time) (ok bool, reason string) {
	switch {
	case f.Status != "active":
		return false, ReasonNotActive
	case f.OwnerStatus != "active":
		return false, ReasonOwnerSuspended
	case f.HasLiveReservedClaim:
		return false, ReasonReservedClaim
	case now.Before(f.ListableAt):
		return false, ReasonHoldingPeriod
	case now.Before(f.RenameAvailableAt):
		return false, ReasonRenameCooldown
	case f.RenamePending:
		return false, ReasonRenamePending
	case f.OwnerBanned:
		return false, ReasonMarketBanned
	}
	return true, ""
}
