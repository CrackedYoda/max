package market

import "github.com/google/uuid"

// PartyFacts are the relationships between buyer and seller (spec §7).
// BuyerAdmin and SellerAdmin are the administrator account when that party is
// an agent, else nil.
type PartyFacts struct {
	BuyerAccount, SellerAccount uuid.UUID
	SharedWallet                bool // a wallet ever linked to both accounts
	PayerWalletEverSellers      bool // the paying wallet was ever linked to the seller
	BuyerAdmin, SellerAdmin     *uuid.UUID
}

// SelfTrade reports whether the trade must be refused as a self or wash trade.
func SelfTrade(p PartyFacts) bool {
	if p.BuyerAccount == p.SellerAccount || p.SharedWallet || p.PayerWalletEverSellers {
		return true
	}
	if p.BuyerAdmin != nil && *p.BuyerAdmin == p.SellerAccount {
		return true
	}
	if p.SellerAdmin != nil && *p.SellerAdmin == p.BuyerAccount {
		return true
	}
	return p.BuyerAdmin != nil && p.SellerAdmin != nil && *p.BuyerAdmin == *p.SellerAdmin
}
