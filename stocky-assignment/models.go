package main

import (
	"github.com/shopspring/decimal"
)

type RewardRequest struct {
	UserID         string          `json:"user_id" binding:"required,uuid"`
	StockSymbol    string          `json:"stock_symbol" binding:"required"`
	Quantity       decimal.Decimal `json:"quantity" binding:"required"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Notes          string          `json:"notes,omitempty"`
}

type CorporateActionRequest struct {
	Action     string  `json:"action" binding:"required"`          
	Symbol     string  `json:"symbol" binding:"required"`
	NewSymbol  string  `json:"new_symbol"`                         
	Ratio      float64 `json:"ratio"`                              
	Effective  string  `json:"effective_date"`                     
}
