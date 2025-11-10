package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

func (a *App) PostRewardHandler(c *gin.Context) {
	var req RewardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.Logger.Warn("bad request:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rewardedAt := time.Now().UTC()

	price, err := getLatestPriceForSymbol(a.DB, req.StockSymbol)
	if err != nil {
		a.Logger.Warn("no price available, will still record reward:", err)
		price = decimal.Zero
	}

	cost := price.Mul(req.Quantity)
	brokeragePct := decimal.NewFromFloat(0.0002) 
	sttPct := decimal.NewFromFloat(0.001)        
	gstPct := decimal.NewFromFloat(0.18)         

	brokerage := cost.Mul(brokeragePct)
	stt := cost.Mul(sttPct)
	gst := brokerage.Mul(gstPct)

	totalFees := brokerage.Add(stt).Add(gst)
	totalCashOutflow := cost.Add(totalFees)

	ctx := context.Background()
	tx, err := a.DB.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		a.Logger.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	defer tx.Rollback()

	var rewardID string
	err = tx.GetContext(ctx, &rewardID, `
		INSERT INTO rewards (user_id, stock_symbol, quantity, rewarded_at, idempotency_key, notes)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id
	`, req.UserID, req.StockSymbol, req.Quantity, rewardedAt, nullString(req.IdempotencyKey), req.Notes)

	if err != nil {
		if isUniqueViolation(err) {
			a.Logger.Infof("duplicate reward (idempotency), returning 200")
			c.JSON(http.StatusOK, gin.H{"status": "duplicate", "message": "reward already processed"})
			return
		}
		a.Logger.Error("insert reward:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not insert reward"})
		return
	}

	txID := generateUUID()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (tx_id, account, entry_type, amount_inr, stock_symbol, stock_quantity, ref_id)
		VALUES ($1,$2,'DEBIT',$3,$4,$5,$6)
	`, txID, "stock:"+req.StockSymbol, nilDecimalOr(price.Mul(req.Quantity)), req.StockSymbol, req.Quantity, rewardID)
	if err != nil {
		a.Logger.Error("ledger insert:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ledger error"})
		return
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (tx_id, account, entry_type, amount_inr, stock_symbol, stock_quantity, ref_id)
		VALUES ($1,$2,'CREDIT',$3,$4,$5,$6)
	`, txID, "cash:exchange", totalCashOutflow, req.StockSymbol, nil, rewardID)
	if err != nil {
		a.Logger.Error("ledger insert 2:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ledger error 2"})
		return
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (tx_id, account, entry_type, amount_inr, ref_id)
		VALUES ($1,$2,'DEBIT',$3,$4)
	`, txID, "fees:brokerage_stt_gst", totalFees, rewardID)
	if err != nil {
		a.Logger.Error("ledger fee insert:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ledger fee error"})
		return
	}

	// commit
	if err := tx.Commit(); err != nil {
		a.Logger.Error("commit:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}

	a.Logger.WithFields(logrus.Fields{
		"reward_id": rewardID, "user": req.UserID, "stock": req.StockSymbol, "qty": req.Quantity.String(),
	}).Info("reward recorded")

	c.JSON(http.StatusOK, gin.H{
		"reward_id":          rewardID,
		"stock":              req.StockSymbol,
		"quantity":           req.Quantity.String(),
		"estimated_cost_inr": totalCashOutflow.StringFixed(4),
	})
}

func (a *App) GetTodayStocksHandler(c *gin.Context) {
	userId := c.Param("userId")
	start := time.Now().UTC().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)

	a.Logger.WithFields(logrus.Fields{
		"user": userId, "start": start, "end": end,
	}).Info("fetching today's stocks")

	var rows []struct {
		StockSymbol string          `db:"stock_symbol" json:"stock_symbol"`
		Quantity    decimal.Decimal `db:"quantity" json:"quantity"`
		RewardedAt  time.Time       `db:"rewarded_at" json:"rewarded_at"`
	}
	err := a.DB.Select(&rows, `
		SELECT stock_symbol, SUM(quantity) as quantity, min(rewarded_at) as rewarded_at
		FROM rewards
		WHERE user_id=$1 AND rewarded_at >= $2 AND rewarded_at < $3
		GROUP BY stock_symbol
	`, userId, start, end)
	if err != nil {
		a.Logger.Error(err)
		c.JSON(500, gin.H{"error": "db error"})
		return
	}
	c.JSON(200, rows)
}

func (a *App) GetHistoricalINRHandler(c *gin.Context) {
	userId := c.Param("userId")
	rows, err := a.DB.Queryx(`
		SELECT 
			date_series.day,
			COALESCE(ROUND(SUM(r.quantity * pt.price_inr), 4), 0) as inr_value
		FROM 
			generate_series(
				CURRENT_DATE - INTERVAL '365 days',
				CURRENT_DATE - INTERVAL '1 day',
				'1 day'::interval
			) AS date_series(day)
		INNER JOIN rewards r ON 
			r.user_id = $1
			AND DATE(r.rewarded_at AT TIME ZONE 'Asia/Kolkata') <= date_series.day
		LEFT JOIN LATERAL (
			SELECT price_inr
			FROM price_ticks pt
			WHERE pt.stock_symbol = r.stock_symbol
				AND DATE(pt.fetched_at AT TIME ZONE 'Asia/Kolkata') = date_series.day
			ORDER BY pt.fetched_at DESC
			LIMIT 1
		) pt ON true
		GROUP BY date_series.day
		ORDER BY date_series.day DESC;
		`, userId)
	if err != nil {
		a.Logger.Error(err)
		c.JSON(500, gin.H{"error": "db error"})
		return
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var day time.Time
		var inr sql.NullString
		if err := rows.Scan(&day, &inr); err != nil {
			a.Logger.Error(err)
			continue
		}
		day = day.UTC()
		out = append(out, map[string]string{
			"day":       day.Format("2006-01-02"),
			"inr_value": inr.String,
		})
	}
	c.JSON(200, out)
}

func (a *App) GetStatsHandler(c *gin.Context) {
	userId := c.Param("userId")
	start := time.Now().UTC().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	var totals []struct {
		StockSymbol string          `db:"stock_symbol" json:"stock_symbol"`
		Quantity    decimal.Decimal `db:"quantity" json:"quantity"`
	}
	if err := a.DB.Select(&totals, `
SELECT stock_symbol, SUM(quantity) AS quantity
FROM rewards
WHERE user_id=$1 AND rewarded_at >= $2 AND rewarded_at < $3
GROUP BY stock_symbol
`, userId, start, end); err != nil {
		a.Logger.Error(err)
		c.JSON(500, gin.H{"error": "db error"})
		return
	}

	var holdings []struct {
		StockSymbol string          `db:"stock_symbol"`
		Quantity    decimal.Decimal `db:"quantity"`
	}
	if err := a.DB.Select(&holdings, `
SELECT stock_symbol, SUM(quantity) AS quantity FROM rewards WHERE user_id=$1 GROUP BY stock_symbol
`, userId); err != nil {
		a.Logger.Error(err)
		c.JSON(500, gin.H{"error": "db error holdings"})
		return
	}

	totalInr := decimal.Zero
	perStock := []map[string]string{}
	for _, h := range holdings {
		price, err := getLatestPriceForSymbol(a.DB, h.StockSymbol)
		if err != nil {
			price = decimal.Zero
		}
		val := price.Mul(h.Quantity)
		totalInr = totalInr.Add(val)
		perStock = append(perStock, map[string]string{
			"stock":     h.StockSymbol,
			"quantity":  h.Quantity.String(),
			"price_inr": price.StringFixed(4),
			"value_inr": val.StringFixed(4),
		})
	}
	c.JSON(200, gin.H{
		"today_totals":        totals,
		"portfolio_value_inr": totalInr.StringFixed(4),
		"per_stock":           perStock,
	})
}

func (a *App) GetPortfolioHandler(c *gin.Context) {
	userId := c.Param("userId")
	var holdings []struct {
		StockSymbol string          `db:"stock_symbol"`
		Quantity    decimal.Decimal `db:"quantity"`
	}
	if err := a.DB.Select(&holdings, `
SELECT stock_symbol, SUM(quantity) AS quantity FROM rewards WHERE user_id=$1 GROUP BY stock_symbol
`, userId); err != nil {
		a.Logger.Error(err)
		c.JSON(500, gin.H{"error": "db error holdings"})
		return
	}
	out := []map[string]string{}
	total := decimal.Zero
	for _, h := range holdings {
		price, _ := getLatestPriceForSymbol(a.DB, h.StockSymbol)
		val := price.Mul(h.Quantity)
		total = total.Add(val)
		out = append(out, map[string]string{
			"stock":     h.StockSymbol,
			"quantity":  h.Quantity.String(),
			"price_inr": price.StringFixed(4),
			"value_inr": val.StringFixed(4),
		})
	}
	c.JSON(200, gin.H{"total_value_inr": total.StringFixed(4), "holdings": out})
}

func (a *App) CorporateActionHandler(c *gin.Context) {
	var req CorporateActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error bad request": err.Error()})
		return
	}

	tx, err := a.DB.Beginx()
	if err != nil {
		a.Logger.Error("failed to start tx:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db transaction error"})
		return
	}
	defer tx.Rollback()

	action := req.Action
	symbol := req.Symbol
	ratio := req.Ratio
	newSymbol := req.NewSymbol

	switch action {
	case "SPLIT":
		if ratio <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid split ratio"})
			return
		}

		result, err := tx.Exec(`UPDATE rewards SET quantity = quantity * $1 WHERE stock_symbol = $2`, ratio, symbol)
		if err != nil {
			a.Logger.Error("failed to update rewards:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update rewards"})
			return
		}
		rowsAffected, _ := result.RowsAffected()
		a.Logger.Infof("Updated %d reward records for %s", rowsAffected, symbol)

		result, err = tx.Exec(`UPDATE price_ticks SET price_inr = price_inr / $1 WHERE stock_symbol = $2`, ratio, symbol)
		if err != nil {
			a.Logger.Error("failed to update price_ticks:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update prices"})
			return
		}
		rowsAffected, _ = result.RowsAffected()
		a.Logger.Infof("Updated %d price records for %s", rowsAffected, symbol)

		txID := generateUUID()
		if _, err := tx.Exec(`
			INSERT INTO ledger_entries (
				id, tx_id, account, entry_type, stock_symbol, stock_quantity, amount_inr
			)
			VALUES (uuid_generate_v4(), $1, 'corporate_action:split', 'DEBIT', $2, NULL, NULL)
		`, txID, symbol); err != nil {
			a.Logger.Error("failed to insert ledger entry:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record ledger entry"})
			return
		}

		a.Logger.Infof("Applied SPLIT %.2fx for %s", ratio, symbol)

	case "MERGER":
		if ratio <= 0 || newSymbol == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid merger ratio or new_symbol missing"})
			return
		}

		result, err := tx.Exec(`
			UPDATE rewards
			SET stock_symbol = $1, quantity = quantity * $2
			WHERE stock_symbol = $3
		`, newSymbol, ratio, symbol)
		if err != nil {
			a.Logger.Error("failed to merge rewards:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to merge rewards"})
			return
		}
		rowsAffected, _ := result.RowsAffected()
		a.Logger.Infof("Updated %d reward records for merger %s -> %s", rowsAffected, symbol, newSymbol)

		txID := generateUUID()
		if _, err := tx.Exec(`
			INSERT INTO ledger_entries (id, tx_id, account, entry_type, stock_symbol)
			VALUES (uuid_generate_v4(), $1, 'corporate_action:merger', 'DEBIT', $2)
		`, txID, symbol); err != nil {
			a.Logger.Error("failed to insert merger ledger entry:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record ledger entry"})
			return
		}

		a.Logger.Infof("Applied MERGER %s -> %s ratio %.2f", symbol, newSymbol, ratio)

	case "DELIST":
		result, err := tx.Exec(`
			UPDATE rewards SET notes = 'DELISTED'
			WHERE stock_symbol = $1
		`, symbol)
		if err != nil {
			a.Logger.Error("failed to mark stock as delisted:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delist stock"})
			return
		}
		rowsAffected, _ := result.RowsAffected()
		a.Logger.Infof("Marked %d reward records as DELISTED for %s", rowsAffected, symbol)

		result, err = tx.Exec(`DELETE FROM price_ticks WHERE stock_symbol = $1`, symbol)
		if err != nil {
			a.Logger.Error("failed to remove price ticks:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove prices"})
			return
		}
		rowsAffected, _ = result.RowsAffected()
		a.Logger.Infof("Removed %d price records for %s", rowsAffected, symbol)

		txID := generateUUID()
		if _, err := tx.Exec(`
			INSERT INTO ledger_entries (id, tx_id, account, entry_type, stock_symbol)
			VALUES (uuid_generate_v4(), $1, 'corporate_action:delist', 'CREDIT', $2)
		`, txID, symbol); err != nil {
			a.Logger.Error("failed to insert delist ledger entry:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record ledger entry"})
			return
		}

		a.Logger.Infof("Applied DELIST on %s", symbol)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported action type"})
		return
	}

	if _, err := tx.Exec(`
		INSERT INTO corporate_actions (stock_symbol, action_type, parameter, effective_date)
		VALUES ($1, $2, jsonb_build_object('ratio', $3::numeric, 'new_symbol', $4::text), $5)
	`, symbol, action, ratio, newSymbol, time.Now().UTC()); err != nil {
		a.Logger.Error("failed to insert corporate action:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record corporate action"})
		return
	}

	if err := tx.Commit(); err != nil {
		a.Logger.Error("commit error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction commit failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Corporate action applied successfully",
		"action":  action,
		"symbol":  symbol,
		"ratio":   ratio,
		"new":     newSymbol,
	})
}
