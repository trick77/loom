package mcp

import (
	"strings"

	"github.com/trick77/loom/internal/classifier"
)

// The shop contract (docs/shop.md) as loom addresses it. The loom-shop
// sidecar serves it; its tests hold these to its names.
const (
	ShopServer     = "shop"
	ShopSearchTool = "search_products"
)

// ContractCategories are the turns a gated contract's tools are offered on,
// whoever serves it: the sidecar, or an mcp.json server of its name with the
// sidecar off (compose sets the URL, the token may be empty).
var ContractCategories = map[string][]string{
	WeatherServer: {string(classifier.Weather)},
	ShopServer:    {string(classifier.Shopping)},
}

// ShopServerConfig is the loom-shop sidecar's endpoint. Best-effort like the
// weather: a dead sidecar costs product search, never boot. Its tool is
// offered on shopping questions only.
func ShopServerConfig(baseURL, token string) ServerConfig {
	return ServerConfig{
		Transport:  TransportStreamableHTTP,
		URL:        strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:    map[string]string{"Authorization": "Bearer " + token},
		Categories: ContractCategories[ShopServer],
		BuiltIn:    true,
		ReadOnly:   true,
	}
}
