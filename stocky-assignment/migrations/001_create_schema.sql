-- migrations/001_create_schema.sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  email TEXT UNIQUE,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rewards (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id UUID NOT NULL REFERENCES users(id),
  stock_symbol TEXT NOT NULL,
  quantity NUMERIC(18,6) NOT NULL,
  rewarded_at TIMESTAMP WITH TIME ZONE NOT NULL,
  idempotency_key TEXT,
  notes TEXT,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
  UNIQUE (idempotency_key)
);

CREATE TABLE IF NOT EXISTS price_ticks (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  stock_symbol TEXT NOT NULL,
  price_inr NUMERIC(18,4) NOT NULL,
  fetched_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ledger_entries (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tx_id UUID NOT NULL, 
  account TEXT NOT NULL,
  entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT','CREDIT')),
  amount_inr NUMERIC(18,4) NULL,
  stock_symbol TEXT NULL,
  stock_quantity NUMERIC(18,6) NULL,
  ref_id UUID NULL,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);

CREATE TABLE IF NOT EXISTS corporate_actions (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  stock_symbol TEXT NOT NULL,
  action_type TEXT NOT NULL,
  parameter JSONB NOT NULL,
  effective_date TIMESTAMP WITH TIME ZONE NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reward_adjustments (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  reward_id UUID NOT NULL REFERENCES rewards(id),
  adjustment_quantity NUMERIC(18,6) NOT NULL,
  reason TEXT,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);
