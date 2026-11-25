"""Emit a transactional PostgreSQL import for a Hybrid Doctor or legacy CRM snapshot."""

import argparse
import json
from pathlib import Path
import sqlite3
import sys

HYBRID = {
    "users": "id", "sessions": None, "settings": "key", "work_categories": "id",
    "clients": "id", "cars": "id", "orders": "id", "parts": "id",
    "order_lines": "id", "assignments": "id", "payments": "id", "photos": "id",
    "stock_moves": "id", "events": "id",
}
CRM = {
    "meta": "k", "users": "login", "orders": "id", "stock": "id",
    "clients": "num", "projects": "id", "inventory": "id", "people": "id",
    "monitors": "id", "sessions": None,
}
SECRET_FIELDS = {"pass_hash", "pw", "token", "csrf", "share_token"}


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("database", type=Path)
    parser.add_argument("--source", choices=["hybrid", "crm", "crm-older"], required=True)
    parser.add_argument("--photos", type=Path)
    args = parser.parse_args()
    tables = HYBRID if args.source == "hybrid" else CRM
    prefix = "" if args.source == "hybrid" else args.source + "/"
    statements = ["BEGIN;", "SET LOCAL standard_conforming_strings = on;"]
    with sqlite3.connect(args.database.resolve().as_uri() + "?mode=ro", uri=True) as db:
        db.row_factory = sqlite3.Row
        for table, key in tables.items():
            if not db.execute(
                "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)
            ).fetchone():
                continue
            for index, row in enumerate(db.execute(f"SELECT * FROM {table} ORDER BY rowid"), 1):
                record = {name: row[name] for name in row.keys() if name not in SECRET_FIELDS}
                if table == "photos" and args.photos:
                    root = args.photos.resolve()
                    for name in ("file", "thumb"):
                        if not row[name]:
                            continue
                        path = (root / row[name]).resolve()
                        if not path.is_relative_to(root):
                            raise ValueError("Photo path is outside the snapshot directory")
                        content = path.read_bytes()
                        statements.append(
                            "INSERT INTO service_record_files(path, content) VALUES "
                            f"({literal(row[name])}, decode('{content.hex()}', 'hex')) "
                            "ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content;"
                        )
                data = json.dumps(record, ensure_ascii=True, allow_nan=False)
                source_id = str(row[key]) if key else str(index)
                statements.append(
                    "INSERT INTO service_records(collection, source_id, data) VALUES "
                    f"({literal(prefix + table)}, {literal(source_id)}, {literal(data)}::jsonb) "
                    "ON CONFLICT (collection, source_id) DO UPDATE SET data=EXCLUDED.data;"
                )
    statements.append("COMMIT;")
    sys.stdout.write("\n".join(statements) + "\n")


if __name__ == "__main__":
    main()
