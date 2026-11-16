package chainassets

import (
	"os"
	"strings"
)

// Endpoint credentials stay server-side. Known network names determine the env
// keys; clients can neither name environment variables nor choose RPC targets.
func (c *Config) LoadEnvironment() {
	for _, network := range catalog() {
		prefix := "DEXWALLET_CHAIN_" + strings.ToUpper(strings.ReplaceAll(network.ID, "-", "_"))
		index := -1
		for i, e := range c.Networks {
			if e.Network == network.ID {
				index = i
				break
			}
		}
		endpointURL := os.Getenv(prefix + "_RPC_URL")
		if index < 0 && endpointURL != "" {
			c.Networks = append(c.Networks, Endpoint{Network: network.ID})
			index = len(c.Networks) - 1
		}
		if index < 0 {
			continue
		}
		e := &c.Networks[index]
		if endpointURL != "" {
			e.URL = endpointURL
		}
		e.APIKey = os.Getenv(prefix + "_API_KEY")
		if contract := os.Getenv(prefix + "_TEST_USDT_CONTRACT"); contract != "" {
			e.TestUSDTContract = contract
		}
	}
}
