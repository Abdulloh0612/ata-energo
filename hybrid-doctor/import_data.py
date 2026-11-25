"""Make a local Hybrid Doctor data directory from an existing SQLite database."""
import argparse
import os
from pathlib import Path
import shutil
import sqlite3
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("database", type=Path)
    parser.add_argument("--photos", type=Path)
    parser.add_argument("--destination", type=Path, default=Path(__file__).resolve().parent / "data")
    args = parser.parse_args()
    destination = args.destination.resolve()
    if destination.exists():
        parser.error("destination already exists")
    with tempfile.TemporaryDirectory(dir=destination.parent) as temp:
        staging = Path(temp) / "data"
        staging.mkdir(mode=0o700)
        with sqlite3.connect(args.database.resolve().as_uri() + "?mode=ro", uri=True) as source:
            with sqlite3.connect(staging / "hybrid.db") as target:
                source.backup(target)
                target.execute("DELETE FROM sessions")
                if target.execute("PRAGMA integrity_check").fetchone() != ("ok",):
                    raise RuntimeError("SQLite integrity check failed")
                references = target.execute("SELECT file, thumb FROM photos").fetchall()
        photos = staging / "photos"
        if args.photos:
            shutil.copytree(args.photos, photos)
        missing = sum(not (photos / name).is_file() for row in references for name in row if name)
        if missing and args.photos:
            parser.error(f"{missing} photo files referenced by SQLite are missing")
        if missing:
            print(f"Missing photo files: {missing}")
        os.rename(staging, destination)


if __name__ == "__main__":
    main()
