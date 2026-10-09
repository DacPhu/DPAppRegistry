package license

// Plan is one way to buy a product.
type Plan struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// UnitVND is the price of one unit for one period; 0 for a free plan.
	UnitVND int64 `json:"vnd"`
	// PerUnit names the unit the price is per ("cluster"), empty when flat.
	PerUnit    string `json:"per,omitempty"`
	PeriodDays int    `json:"period_days,omitempty"`
	// MaxQuantity caps one order; 0 means the plan is not sold by quantity.
	MaxQuantity int `json:"max_quantity,omitempty"`
	// Purchasable plans are bought on the store; Contact plans are quoted.
	Purchasable bool  `json:"purchasable"`
	Contact     bool  `json:"contact"`
	FromVND     int64 `json:"from_vnd,omitempty"`
}

// Product is something the registry's owner sells; App is its registry name.
type Product struct {
	ID    string `json:"id"`
	App   string `json:"app"`
	Name  string `json:"name"`
	Plans []Plan `json:"plans"`
}

// Products is the catalog (Mitekbro/docs/app-pricing.md). Prices live here so
// the server, not the page, decides what an order costs.
var Products = []Product{
	{ID: "dpfs", App: "dpfs", Name: "DPFileSystem", Plans: []Plan{
		{ID: "community", Name: "Community"},
		{ID: "business", Name: "Business", UnitVND: 4_900_000, PerUnit: "cluster", PeriodDays: 365, MaxQuantity: 20, Purchasable: true},
		{ID: "enterprise", Name: "Enterprise", FromVND: 25_000_000, PeriodDays: 365, Contact: true},
	}},
}

// Find returns the product and plan with these IDs.
func Find(product, plan string) (Product, Plan, bool) {
	for _, p := range Products {
		if p.ID != product {
			continue
		}
		for _, pl := range p.Plans {
			if pl.ID == plan {
				return p, pl, true
			}
		}
	}
	return Product{}, Plan{}, false
}

// Amount is what quantity units of plan cost for one period.
func Amount(plan Plan, quantity int) int64 {
	if quantity < 1 {
		quantity = 1
	}
	return plan.UnitVND * int64(quantity)
}
