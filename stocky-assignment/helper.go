package main

import (
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func generateUUID() string {
	return uuid.New().String()
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate")
}

func nilDecimalOr(d decimal.Decimal) interface{} {
	if d.IsZero() {
		return nil
	}
	return d
}
