import json
import sqlite3
import os
from datetime import datetime

def create_prices(db_file='./db/items.db'):
    """Create marketorders table and prices view."""

    if not os.path.exists(db_file):
        print(f"Error: {db_file} not found. Create items database first.")
        return

    conn = sqlite3.connect(db_file)
    c = conn.cursor()

    print("Creating marketorders table...")

    c.execute('''
        CREATE TABLE IF NOT EXISTS marketorders (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            item_id TEXT NOT NULL,
            city TEXT NOT NULL,
            quality_level INTEGER NOT NULL DEFAULT 0,
            price INTEGER NOT NULL,
            amount INTEGER NOT NULL DEFAULT 0,
            auction_type TEXT NOT NULL,
            created_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
    ''')

    c.execute('CREATE INDEX IF NOT EXISTS idx_mo_item ON marketorders(item_id, city, quality_level)')

    c.execute('DROP VIEW IF EXISTS prices')
    c.execute('''
        CREATE VIEW prices AS
        SELECT
            item_id,
            city,
            quality_level,
            MIN(CASE WHEN auction_type = 'request' THEN price END) AS sell_price_min,
            MAX(CASE WHEN auction_type = 'request' THEN price END) AS sell_price_max,
            MIN(CASE WHEN auction_type = 'offer'    THEN price END) AS buy_price_min,
            MAX(CASE WHEN auction_type = 'offer'    THEN price END) AS buy_price_max,
            MAX(created_at) AS updated_at
        FROM marketorders
        WHERE created_at > datetime('now', '-1 hour')
        GROUP BY item_id, city, quality_level
    ''')

    conn.close()
    print("marketorders table and prices view created.")

if __name__ == '__main__':
    create_prices()
