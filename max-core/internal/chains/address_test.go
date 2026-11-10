package chains

import (
	"strings"
	"testing"
)

// lastCharSwapped changes the final character so the checksum no longer matches.
func lastCharSwapped(s string) string {
	last := s[len(s)-1]
	repl := byte('q')
	if last == 'q' || last == 'Q' {
		repl = 'p'
	}
	if last >= 'A' && last <= 'Z' && last != 'Q' {
		repl = 'Q'
	}
	return s[:len(s)-1] + string(repl)
}

var validAddresses = []struct {
	ns   Namespace
	in   string
	want string
}{
	{NamespaceBitcoin, "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"},
	{NamespaceBitcoin, "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy", "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"},
	{NamespaceBitcoin, "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"},
	{NamespaceBitcoin, "bc1qrp33g0q5c5txsp9arysrx4k6zdkfs4nce4xj0gdcccefvpysxf3qccfmv3", "bc1qrp33g0q5c5txsp9arysrx4k6zdkfs4nce4xj0gdcccefvpysxf3qccfmv3"},
	{NamespaceBitcoin, "bc1p5d7rjq7g6rdk2yhzks9smlaqtedr4dekq08ge8ztwac72sfr9rusxg3297", "bc1p5d7rjq7g6rdk2yhzks9smlaqtedr4dekq08ge8ztwac72sfr9rusxg3297"},
	{NamespaceBitcoin, "BC1QAR0SRRR7XFKVY5L643LYDNW9RE59GTZZWF5MDQ", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"},
	{NamespaceBitcoin, "  bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq\n", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"},
	{NamespaceLitecoin, "LaMT348PWRnrqeeWArpwQPbuanpXDZGEUz", "LaMT348PWRnrqeeWArpwQPbuanpXDZGEUz"},
	{NamespaceLitecoin, "MGxNPPB7eBoWPUaprtX9v9CXJZoD2465zN", "MGxNPPB7eBoWPUaprtX9v9CXJZoD2465zN"},
	{NamespaceLitecoin, "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy", "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"},
	{NamespaceLitecoin, "ltc1qar0srrr7xfkvy5l643lydnw9re59gtzz24wl4s", "ltc1qar0srrr7xfkvy5l643lydnw9re59gtzz24wl4s"},
	{NamespaceTron, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"},
	{NamespaceEVM, "0x52908400098527886E0F7030069857D2E4169EE7", "0x52908400098527886e0f7030069857d2e4169ee7"},
	{NamespaceEVM, "0x52908400098527886e0f7030069857d2e4169ee7", "0x52908400098527886e0f7030069857d2e4169ee7"},
}

func TestNormalizeAddress_Valid(t *testing.T) {
	for _, v := range validAddresses {
		got, err := NormalizeAddress(v.ns, v.in)
		if err != nil {
			t.Errorf("%s %q: unexpected error %v", v.ns, v.in, err)
			continue
		}
		if got != v.want {
			t.Errorf("%s %q: got %q, want %q", v.ns, v.in, got, v.want)
		}
	}
}

func TestNormalizeAddress_Invalid(t *testing.T) {
	type tc struct {
		ns Namespace
		in string
	}
	var cases []tc
	for _, v := range validAddresses {
		if v.ns == NamespaceEVM && strings.ToLower(v.in) == v.in {
			continue // an all-lowercase EVM address carries no checksum
		}
		cases = append(cases, tc{v.ns, lastCharSwapped(strings.TrimSpace(v.in))})
	}
	cases = append(cases,
		tc{NamespaceBitcoin, "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwF5mdq"},  // mixed case bech32
		tc{NamespaceLitecoin, "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"}, // bitcoin HRP on litecoin
		tc{NamespaceBitcoin, "ltc1qar0srrr7xfkvy5l643lydnw9re59gtzz24wl4s"}, // litecoin HRP on bitcoin
		tc{NamespaceBitcoin, "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"},  // testnet
		tc{NamespaceBitcoin, "LaMT348PWRnrqeeWArpwQPbuanpXDZGEUz"},          // litecoin version on bitcoin
		tc{NamespaceTron, "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"},             // wrong version byte
		tc{NamespaceEVM, "0x52908400098527886e0F7030069857D2E4169EE7"},      // bad EIP-55
		tc{NamespaceBitcoin, ""},
		tc{NamespaceTron, ""},
	)
	for _, c := range cases {
		if got, err := NormalizeAddress(c.ns, c.in); err == nil {
			t.Errorf("%s %q: expected error, got %q", c.ns, c.in, got)
		}
	}
}

func TestReceiveOnlyChains(t *testing.T) {
	want := map[string]struct {
		ns       Namespace
		coin     string
		decimals int
		explorer string
	}{
		"bitcoin":  {NamespaceBitcoin, "BTC", 8, "https://mempool.space/tx/"},
		"litecoin": {NamespaceLitecoin, "LTC", 8, "https://litecoinspace.org/tx/"},
		"tron":     {NamespaceTron, "TRX", 6, "https://tronscan.org/#/transaction/"},
	}
	for key, w := range want {
		c, ok := Lookup(key)
		if !ok {
			t.Fatalf("%s not supported", key)
		}
		if !c.ReceiveOnly || c.Testnet || c.Namespace != w.ns || c.NativeCoin != w.coin || c.NativeDecimals != w.decimals || c.ExplorerTxURL != w.explorer {
			t.Errorf("%s = %+v", key, c)
		}
	}
	for _, c := range Supported {
		if _, isNew := want[c.Key]; !isNew && c.ReceiveOnly {
			t.Errorf("%s should not be receive-only", c.Key)
		}
	}
}

func TestNamespaces(t *testing.T) {
	for _, s := range []string{"evm", "solana", "bitcoin", "litecoin", "tron"} {
		ns, ok := ParseNamespace(s)
		if !ok || !IsPasteable(ns) {
			t.Errorf("%s: parse=%v pasteable=%v", s, ok, IsPasteable(ns))
		}
	}
	for _, ns := range []Namespace{NamespaceBitcoin, NamespaceLitecoin, NamespaceTron} {
		if IsLinkable(ns) {
			t.Errorf("%s must not be linkable", ns)
		}
	}
}

func TestLookupSendable(t *testing.T) {
	for _, k := range []string{"bitcoin", "litecoin", "tron", "nope"} {
		if _, ok := LookupSendable(k); ok {
			t.Errorf("%s should not be sendable", k)
		}
	}
	if _, ok := LookupSendable("base"); !ok {
		t.Error("base should be sendable")
	}
}
