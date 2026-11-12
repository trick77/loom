package main

import (
	"encoding/json"
	"strings"

	"github.com/trick77/toolkit/mcpserver"
)

// The result's _meta repeats the shop's whole catalog answer, the source of
// its JSON text. It is no contract: Galaxus documents only the text. Three
// things there are worth having and nowhere else: a sale's earlier price,
// labels and how soon an item ships. Read defensively: a _meta missing or
// changed costs these, never the search.

// offer is what _meta adds to a product, by its id (the text's id is the
// node's trackingId).
type offer struct {
	wasPrice     *float64
	rebate       int    // percent off wasPrice
	availability string // mcpserver.Availability*
	condition    string // mcpserver.Condition*
	volume       bool   // cheaper by the piece in quantity
}

// availabilities map the shop's delivery icon (a tick, or a pie filled to a
// quarter, half, three quarters or full) to how soon an item ships. Read off
// product pages on 2026-11-12: a tick ships from the shop's stock in a day or
// two, a full pie from a supplier's or third party's stock in a few days, a
// half pie is ordered from the supplier (a window of one to three weeks).
// Three quarters and a quarter were not seen on a page; one of them also
// stands for "not available, no date": both read as uncertain, never as soon.
var availabilities = map[string]string{
	"TICK":          mcpserver.AvailabilityInStock,
	"FULL":          mcpserver.AvailabilitySupplier,
	"HALF":          mcpserver.AvailabilityOnOrder,
	"THREE_QUARTER": mcpserver.AvailabilityUncertain,
	"QUARTER":       mcpserver.AvailabilityUncertain,
}

type metaNode struct {
	TrackingID     int64             `json:"trackingId"`
	Labels         []json.RawMessage `json:"labels"`
	InsteadOfPrice *struct {
		Type   string  `json:"type"`
		Rebate float64 `json:"rebate"`
		Price  struct {
			AmountInclusive float64 `json:"amountInclusive"`
		} `json:"price"`
	} `json:"insteadOfPrice"`
	Availability *struct {
		Classification string `json:"availabilityClassification"`
	} `json:"availability"`
}

// productID is an id sent as a number or a numeric string; 0 when neither
// (no product has id 0, so it joins nothing).
func productID(raw json.RawMessage) int64 {
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0
		}
		n = json.Number(s)
	}
	id, err := n.Int64()
	if err != nil {
		return 0
	}
	return id
}

// offers reads _meta into offers by product id; nil when it holds none.
func offers(meta json.RawMessage) map[int64]offer {
	var m struct {
		Response struct {
			Data struct {
				Search struct {
					Products struct {
						Edges []struct {
							Node json.RawMessage `json:"node"`
						} `json:"edges"`
					} `json:"products"`
				} `json:"serverToolShopSearch"`
			} `json:"data"`
		} `json:"response"`
	}
	if len(meta) == 0 || json.Unmarshal(meta, &m) != nil {
		return nil
	}
	out := map[int64]offer{}
	for _, e := range m.Response.Data.Search.Products.Edges {
		// Node by node: one the shop changed costs only its own extras.
		var n metaNode
		if json.Unmarshal(e.Node, &n) != nil || n.TrackingID == 0 {
			continue
		}
		var o offer
		if p := n.InsteadOfPrice; p != nil && p.Type == "SALES_PRICE_BEFORE" && p.Price.AmountInclusive > 0 {
			was := p.Price.AmountInclusive
			o.wasPrice = &was
			if p.Rebate > 0 && p.Rebate < 100 {
				o.rebate = int(p.Rebate)
			}
		}
		if a := n.Availability; a != nil {
			o.availability = availabilities[strings.ToUpper(a.Classification)]
		}
		for _, raw := range n.Labels {
			var label string
			if json.Unmarshal(raw, &label) != nil {
				continue
			}
			switch strings.ToUpper(label) {
			case "USED":
				o.condition = mcpserver.ConditionUsed
			case "SHOWROOM":
				if o.condition == "" {
					o.condition = mcpserver.ConditionDisplay
				}
			case "VOLUME_DISCOUNT":
				o.volume = true
			}
		}
		out[n.TrackingID] = o
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// availabilityText is how soon an item ships, in the words the model gets.
var availabilityText = map[string]string{
	mcpserver.AvailabilityInStock:   "in stock, delivered in a day or two",
	mcpserver.AvailabilitySupplier:  "in stock at a supplier, delivered in a few days",
	mcpserver.AvailabilityOnOrder:   "not in stock, ordered from the supplier, delivered in about one to three weeks",
	mcpserver.AvailabilityUncertain: "not in stock, no firm delivery date (weeks, or not available now)",
}

var conditionText = map[string]string{
	mcpserver.ConditionUsed:    "used",
	mcpserver.ConditionDisplay: "showroom item",
}
