package utils

import "strings"

func GetWSEndpoint(endpoint string) string {
	// replace http(s) with ws(s)
	wsEndpoint := strings.Replace(endpoint, "http://", "ws://", 1)
	wsEndpoint = strings.Replace(wsEndpoint, "https://", "wss://", 1)
	// replace the default port with the default ws port
	if strings.Contains(endpoint, ":") {
		wsEndpoint = strings.Replace(wsEndpoint, ":8545", ":8546", 1)
	}
	// Handle the pattern used by route53 endpoints
	if strings.Contains(endpoint, "evm-rpc") {
		wsEndpoint = strings.Replace(wsEndpoint, "evm-rpc", "evm-ws", 1)
	}
	return wsEndpoint
}
