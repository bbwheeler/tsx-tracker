CREATE TABLE IF NOT EXISTS companies (
    symbol        TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    exchange      TEXT NOT NULL DEFAULT 'TSX',
    sector        TEXT NOT NULL DEFAULT '',
    industry      TEXT NOT NULL DEFAULT '',
    ceo           TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    website       TEXT NOT NULL DEFAULT '',
    headquarters  TEXT NOT NULL DEFAULT '',
    employees     BIGINT NOT NULL DEFAULT 0,
    market_cap    DOUBLE PRECISION NOT NULL DEFAULT 0,
    price         DOUBLE PRECISION NOT NULL DEFAULT 0,
    currency      TEXT NOT NULL DEFAULT 'CAD',
    last_updated  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_companies_sector ON companies (sector);
CREATE INDEX IF NOT EXISTS idx_companies_last_updated ON companies (last_updated);
