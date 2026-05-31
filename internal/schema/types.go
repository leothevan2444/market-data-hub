package schema

type DailyQuoteRecord struct {
	Symbol    string   `json:"symbol"`
	Date      string   `json:"date,omitempty"`
	Open      float64  `json:"open"`
	High      float64  `json:"high"`
	Low       float64  `json:"low"`
	Close     float64  `json:"close"`
	AdjClose  float64  `json:"adjClose"`
	Volume    int64    `json:"volume"`
	Currency  string   `json:"currency,omitempty"`
	Exchange  string   `json:"exchange,omitempty"`
	Source    string   `json:"source,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
	Flags     []string `json:"flags,omitempty"`
}

type DailyMarketFile struct {
	Market        string             `json:"market"`
	Date          string             `json:"date"`
	Type          string             `json:"type"`
	Source        []string           `json:"source"`
	Count         int                `json:"count"`
	SchemaVersion int                `json:"schemaVersion"`
	Records       []DailyQuoteRecord `json:"records"`
}

type LatestQuote struct {
	Price     float64  `json:"price"`
	Change    float64  `json:"change"`
	ChangePct float64  `json:"changePct"`
	Volume    int64    `json:"volume"`
	Open      float64  `json:"open"`
	High      float64  `json:"high"`
	Low       float64  `json:"low"`
	PrevClose float64  `json:"prevClose"`
	Date      string   `json:"date,omitempty"`
	Source    string   `json:"source,omitempty"`
	Stale     bool     `json:"stale,omitempty"`
	Flags     []string `json:"flags,omitempty"`
}

type LatestFull struct {
	Market        string                 `json:"market"`
	AsOf          string                 `json:"asOf"`
	SchemaVersion int                    `json:"schemaVersion"`
	Quotes        map[string]LatestQuote `json:"quotes"`
}

type LatestMinified struct {
	Market string               `json:"market"`
	AsOf   string               `json:"asOf"`
	Schema []string             `json:"schema"`
	Quotes map[string][]float64 `json:"quotes"`
}

type SymbolInfo struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Exchange  string `json:"exchange"`
	AssetType string `json:"assetType"`
	IsEtf     bool   `json:"isEtf"`
	IsActive  bool   `json:"isActive"`
}

type SymbolUniverse struct {
	Date    string       `json:"date"`
	Market  string       `json:"market,omitempty"`
	Source  string       `json:"source"`
	Symbols []SymbolInfo `json:"symbols"`
}

type IngestionRun struct {
	ID             string   `json:"id"`
	Market         string   `json:"market"`
	Date           string   `json:"date"`
	Status         string   `json:"status"`
	Source         string   `json:"source"`
	RecordsTotal   int      `json:"recordsTotal"`
	RecordsSuccess int      `json:"recordsSuccess"`
	RecordsFailed  int      `json:"recordsFailed"`
	FailedSymbols  []string `json:"failedSymbols,omitempty"`
	StartedAt      string   `json:"startedAt"`
	FinishedAt     string   `json:"finishedAt,omitempty"`
	ErrorMessage   string   `json:"errorMessage,omitempty"`
}

type SymbolHistory struct {
	Symbol        string             `json:"symbol"`
	Market        string             `json:"market"`
	SchemaVersion int                `json:"schemaVersion"`
	Records       []DailyQuoteRecord `json:"records"`
}
