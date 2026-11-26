CREATE TABLE stock_items (
    id BIGSERIAL PRIMARY KEY,
    sku TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    unit TEXT NOT NULL DEFAULT 'шт',
    qty NUMERIC(14,3) NOT NULL DEFAULT 0 CHECK (qty >= 0),
    min_qty NUMERIC(14,3) NOT NULL DEFAULT 0 CHECK (min_qty >= 0),
    buy_price NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK (buy_price >= 0),
    sell_price NUMERIC(14,2) NOT NULL DEFAULT 0 CHECK (sell_price >= 0),
    location TEXT NOT NULL DEFAULT '',
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE stock_movements (
    id BIGSERIAL PRIMARY KEY,
    item_id BIGINT NOT NULL REFERENCES stock_items(id),
    kind TEXT NOT NULL CHECK (kind IN ('in','out','adjust')),
    qty_delta NUMERIC(14,3) NOT NULL CHECK (qty_delta <> 0),
    qty_after NUMERIC(14,3) NOT NULL CHECK (qty_after >= 0),
    unit_price NUMERIC(14,2),
    reason TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX stock_movements_item_created_idx ON stock_movements(item_id, created_at DESC);
CREATE INDEX stock_movements_created_idx ON stock_movements(created_at DESC);
