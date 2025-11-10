
package main

import (
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	_ "github.com/lib/pq"
)

func main() {
	logger := logrus.New()
	logger.Out = os.Stdout

	if err := godotenv.Load(); err != nil {
		logger.Infof(".env not loaded: %v; trying .env.example", err)
		if err2 := godotenv.Load(".env.example"); err2 == nil {
			logger.Info("loaded .env.example")
		} else {
			logger.Debugf(".env.example not loaded: %v", err2)
		}
	}

	db, err := sqlx.Connect("postgres", getPostgresDSN())
	if err != nil {
		logger.Fatal("db connect:", err)
	}
	defer db.Close()

	app := &App{
		DB:     db,
		Logger: logger,
	}

	intervalMin := 60
	go startPriceFetcher(app, time.Duration(intervalMin)*time.Minute)

	r := gin.Default()

	api := r.Group("/api")
	{
		api.POST("/reward", app.PostRewardHandler)
		api.GET("/today-stocks/:userId", app.GetTodayStocksHandler)
		api.GET("/historical-inr/:userId", app.GetHistoricalINRHandler)
		api.GET("/stats/:userId", app.GetStatsHandler)
		api.GET("/portfolio/:userId", app.GetPortfolioHandler)
		api.POST("/corporate-action", app.CorporateActionHandler)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Infof("starting server on %s", port)
	if err := r.Run(":" + port); err != nil {
		logger.Fatal(err)
	}
}
