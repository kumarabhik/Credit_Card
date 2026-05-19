CREATE TABLE IF NOT EXISTS accounts (
  account_id TEXT PRIMARY KEY,
  currency TEXT NOT NULL,
  available_minor BIGINT NOT NULL,
  posted_minor BIGINT NOT NULL,
  account_status TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO accounts (account_id, currency, available_minor, posted_minor, account_status)
VALUES ('acct_demo_card', 'USD', 10000, 1000, 'ACTIVE')
ON CONFLICT (account_id) DO NOTHING;
