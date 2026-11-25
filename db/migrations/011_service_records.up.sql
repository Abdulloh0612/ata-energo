CREATE TABLE service_records (
    collection TEXT NOT NULL,
    source_id TEXT NOT NULL,
    data JSONB NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    PRIMARY KEY (collection, source_id)
);

CREATE TABLE service_record_files (
    path TEXT PRIMARY KEY,
    content BYTEA NOT NULL
);
