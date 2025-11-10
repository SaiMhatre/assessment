Stocky Assignment (Go)

Running locally
1. Create Postgres DB named `assignment`.
2. Run migrations in `migrations/`.
3. Update `.env` and set DB creds.
4. go run .

Endpoints
- POST /api/reward
- GET /api/today-stocks/{userId}
- GET /api/historical-inr/{userId}
- GET /api/stats/{userId}
- GET /api/portfolio/{userId}
- POST /api/corporate-action

Notes
- Uses `shopspring/decimal` for precise decimal arithmetic.
- Ledger entries created atomically with rewards for double-entry accounting.

API Functioning Explained

1) Starting the Service
When the service starts, it fetches the latest stock prices for all listed companies (e.g., RELIANCE, TCS, INFY).
These prices are stored in the price_ticks table.
The system uses a mock function (randomPriceForSymbol) that generates random INR prices for demonstration purposes.

2) Hourly Price Updates
A background job runs every hour to refresh stock prices for all companies.
Each update inserts new price records into the price_ticks table, ensuring we have a complete hourly price history.

3) Random Price Generation
The helper function randomPriceForSymbol() simulates market price movements by generating a random INR price for each symbol.
This acts as a placeholder for a real market data API (such as NSE/BSE feeds).

4) POST /api/reward
This endpoint records that a user has been rewarded with a certain number of shares.

Process Flow:
Validates all required request fields (user_id, stock_symbol, quantity, idempotency_key).
Inserts a new record into the rewards table.
The idempotency_key is unique and ensures the same reward isn’t processed multiple times (protects against duplicate or replay requests).

Creates three ledger entries in ledger_entries:
Stock Asset (DEBIT): Stocky credits the user with stock units (Stocky gains a stock asset).
Cash Payment (CREDIT): Stocky buys those shares from the market — cash outflow recorded.
Fees & Taxes (DEBIT): Brokerage, STT, and GST expenses incurred in the transaction.
This ensures accurate double-entry accounting and transparent internal tracking.

5) GET /api/today-stocks/{userId}
Fetches all the stocks rewarded to a specific user on the current day.

How it works:
Queries the rewards table for entries with the given user_id and rewarded_at between today’s start and end timestamps.
Groups results by stock_symbol and sums their quantities.
Returns each company’s stock symbol with total quantity earned today.

Example Output:
[
  {"stock_symbol": "RELIANCE", "quantity": "0.5"},
  {"stock_symbol": "TCS", "quantity": "1.0"}
]

6) GET /api/historical-inr/{userId}
This endpoint calculates the historical INR value of the user’s portfolio for the past 365 days.

How it works:
Generates a date series for the last 365 days.
For each day:
It checks how many shares the user owned up to that date from the rewards table.
It retrieves the end-of-day (EOD) stock prices from the price_ticks table.
Multiplies each stock’s quantity by its EOD price to find the total INR value for that day.
Groups by date and returns daily portfolio values.

Example:
Suppose:
On Nov 7, the user had 0.5 RELIANCE and 2.0 TCS
RELIANCE closed at ₹2600, TCS at ₹3100

Then:
Total INR (Nov 7) = (0.5 × 2600) + (2.0 × 3100) = 1300 + 6200 = ₹7500

Example Output:
[
  {"day": "2025-11-08", "inr_value": "3124.0000"},
  {"day": "2025-11-07", "inr_value": "7500.0000"}
]

7) GET /api/stats/{userId}
This API provides a summary of a user’s overall performance and holdings.

Step 1: It calculates total shares rewarded today for that user — grouped by stock symbol.
It queries the rewards table using the current date (rewarded_at >= today and < tomorrow).
This helps identify how many new shares were rewarded today per company.

Step 2: It calculates the current value of the user’s entire portfolio in INR.
It sums up all the stocks (regardless of date) grouped by stock_symbol.
For each symbol, it fetches the latest price using the helper function getLatestPriceForSymbol.
It multiplies the quantity × latest_price to get current INR value per stock.
Step 3: It computes the total INR value of all holdings and returns both:
Total shares rewarded today (today_totals)
Overall portfolio valuation (portfolio_value_inr)

Individual stock breakdown (per_stock)

Example Output:

{
  "today_totals": [
    {"stock_symbol": "RELIANCE", "quantity": "0.5"},
    {"stock_symbol": "TCS", "quantity": "1.0"}
  ],
  "portfolio_value_inr": "2548.2300",
  "per_stock": [
    {"stock": "RELIANCE", "quantity": "0.5", "price_inr": "2596.46", "value_inr": "1298.23"},
    {"stock": "TCS", "quantity": "1.0", "price_inr": "1250.00", "value_inr": "1250.00"}
  ]
}


This endpoint gives a real-time snapshot of a user’s financial standing within Stocky.

8) GET /api/portfolio/{userId}

This API gives a concise summary of all the stocks that a particular user currently holds — effectively a portfolio view.

Step 1: It queries the rewards table for the given user and aggregates all shares per stock_symbol.
Step 2: For each symbol, it fetches the latest stock price using getLatestPriceForSymbol.
Step 3: It multiplies the quantity with price to find the current value in INR of that stock.
Step 4: It sums up all the stock values to calculate the total portfolio INR value.
Step 5: Returns all these details as JSON.

Example Output:

{
  "total_value_inr": "2548.2300",
  "holdings": [
    {"stock": "RELIANCE", "quantity": "0.5", "price_inr": "2596.46", "value_inr": "1298.23"},
    {"stock": "TCS", "quantity": "1.0", "price_inr": "1250.00", "value_inr": "1250.00"}
  ]
}


Useful for showing the current market value of all stocks held by the user.
Used internally for showing user’s “dashboard” or “portfolio summary” in apps.

9) POST /api/corporate-action

This API handles major corporate events such as:

Stock Splits
Mergers
Delistings

It updates both the rewards and price_ticks tables accordingly, and records an entry in the ledger and corporate_actions table to ensure auditability.

a) Stock Split (action = SPLIT)
When a stock is split (e.g., 1:2 split), each existing share is divided into more shares, reducing price per share proportionally.

The API:
Multiplies quantity in rewards by the split ratio (e.g., ×2)
Divides price_inr in price_ticks by the split ratio (e.g., ÷2)
Adds an entry in ledger_entries to note the corporate event
Inserts a record in corporate_actions for history

Example:
If user had 1 share of RELIANCE @ ₹2700, and split is 2x →
Now user has 2 shares @ ₹1350 each (same total value).

Response:
{
  "message": "Corporate action applied successfully",
  "action": "SPLIT",
  "symbol": "RELIANCE",
  "ratio": 2.0
}

b) Merger (action = MERGER)
When two companies merge (e.g., INFY merges into TCS), users’ old stock symbols are replaced by the new one at a specified ratio.

The API:
Updates all rewards changing stock_symbol from old to new
Multiplies quantity by merger ratio
Deletes old price entries from price_ticks
Records a ledger_entry and corporate_action

Example:
If user had 10 INFY shares and ratio is 0.5 (INFY → TCS):
→ User now has 5 TCS shares.

c) Delist (action = DELIST)
Used when a company is removed from trading (delisted).

The API:
Marks all rewards for that symbol as “DELISTED”
Removes its prices from price_ticks
Logs a “CREDIT” entry in ledger_entries
Inserts a corporate action record for auditing

Response:
{
  "message": "Corporate action applied successfully",
  "action": "DELIST",
  "symbol": "XYZCORP"
}

10) getLatestPriceForSymbol (internal function)

This helper function fetches the most recent stock price from the price_ticks table.
If a price is available, it returns the latest price_inr for the given stock symbol.
If not found, it can optionally generate a mock price (via randomPriceForSymbol) and insert it into the table to keep prices fresh.
This ensures all APIs (/stats, /portfolio, /reward) always have an INR value for every stock, even if the price feed temporarily fails.

11) Duplicate reward events / replay attacks

rewards table has UNIQUE(idempotency_key) so if client provides unique idempotency_key per request, second attempt will be rejected as duplicate and the API will return a success/duplicate status.
Server should require clients to send Idempotency-Key header or payload idempotency_key. If not provided, duplicates could occur; consider generating server-side idempotency if you can detect duplicates by matching (user,stock,quantity,timestamp) hash.

12) Rounding errors in INR valuation

All monetary fields use NUMERIC(18,4) in DB and decimal.Decimal in Go to avoid float error. Use explicit Round when showing to UI. Ledger double-entry must balance — ensure rounding policy (e.g., always round fees to 2 or 4 decimals and record rounding differences in an rounding:gain_loss account). Used shopspring/decimal library for decimal calculation which gives accurate round off result.

13) Price API downtime or stale data

Price fetcher stores historical price_ticks. If the external price fetch fails, background job retries and server uses the latest cached price. Responses should include a price_timestamp and a stale flag if price older than e.g. 4 hours.

14) Adjustments / refunds of previously given rewards
reward_adjustments table supports corrections. To refund: insert a negative adjustment and create ledger reversal entries (debit/cargo reversal). Keep full audit trail.

15) Transaction safety / accounting integrity
All reward processing is wrapped in a DB transaction that inserts reward + related ledger entries atomically.

16) Idempotency and concurrency
Unique constraint on idempotency_key, and using DB transactions prevents double execution on retries. Use optimistic locking for more complex flows.

17) Scaling & partitioning
Read replicas for heavy read endpoints (stats, portfolio), caching layer (Redis) for price cache & portfolio valuations for frequent calls. Use batched price fetches and bulk updates. For very large numbers of users, aggregate valuations per user asynchronously and cache, computing deltas on reward events.
