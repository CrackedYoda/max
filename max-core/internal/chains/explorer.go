package chains

import "strings"

// TxURL renders an explorer link; templates may place the hash with {tx}.
func (c Chain) TxURL(hash string) string {
	if strings.Contains(c.ExplorerTxURL, "{tx}") {
		return strings.ReplaceAll(c.ExplorerTxURL, "{tx}", hash)
	}
	return c.ExplorerTxURL + hash
}
