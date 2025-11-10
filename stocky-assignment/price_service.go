package main

import (
	"context"
	"math/rand"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

func startPriceFetcher(app *App, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	fetchAndStorePrices(app)
	for {
		select {
		case <-ticker.C:
			fetchAndStorePrices(app)
		}
	}
}

var sampleStocks = []string{"RELIANCE", "TCS", "INFY", "MAHINDRA", "CISCO", "HDFC", "ICICI", "WIPRO", "LT", "ADANI", "AXIS", "KOTAK", "BAJAJ", "BHARTI", "VEDANTA"}

func fetchAndStorePrices(app *App) {
	app.Logger.Info("fetching prices (mock)...")
	ctx := context.Background()
	tx, err := app.DB.BeginTxx(ctx, nil)
	if err != nil {
		app.Logger.Error("start tx:", err)
		return
	}
	defer tx.Rollback()

	for _, s := range sampleStocks {
		price := randomPriceForSymbol(s)
		fetchedAt := time.Now().UTC()
		_, err := tx.ExecContext(ctx, `INSERT INTO price_ticks (stock_symbol, price_inr, fetched_at) VALUES ($1,$2,$3)`, s, price, fetchedAt)
		if err != nil {
			app.Logger.Error("insert price:", err)
			return
		}
		app.Logger.Infof("price stored %s -> %s", s, price.String())
	}
	if err := tx.Commit(); err != nil {
		app.Logger.Error("commit price tx:", err)
	}
}

func randomPriceForSymbol(symbol string) decimal.Decimal {
	base := 1000.0
	switch symbol {
	case "RELIANCE":
		base = 2600.0
	case "TCS":
		base = 3300.0
	case "INFY":
		base = 1500.0
	case "HDFC":
		base = 2500.0
	case "ADANI":
		base = 2000.0
	case "AXIS":
		base = 700.0
	case "KOTAK":
		base = 1800.0
	case "MAHINDRA":
		base = 900.0
	case "CISCO":
		base = 4500.0
	case "WIPRO":
		base = 400.0
	case "LT":
		base = 1800.0
	case "BAJAJ":
		base = 3500.0
	case "BHARTI":
		base = 700.0
	case "VEDANTA":
		base = 300.0
	}
	change := (rand.Float64()*0.1 - 0.05)
	p := base * (1.0 + change)
	d := decimal.NewFromFloatWithExponent(p, 0)
	return d.Round(4)
}

func getLatestPriceForSymbol(db *sqlx.DB, symbol string) (decimal.Decimal, error) {
	var p decimal.Decimal
	row := db.QueryRowx(`
        SELECT price_inr 
        FROM price_ticks 
        WHERE stock_symbol=$1 
        ORDER BY fetched_at DESC 
        LIMIT 1`, symbol)
	if err := row.Scan(&p); err != nil {
		return decimal.Zero, err
	}
	return p, nil
}
