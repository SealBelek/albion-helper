import json
import sqlite3
import os

def create_world_table(json_file='data/world.json', db_file='db/items.db'):
    if not os.path.exists(json_file):
        print(f"Error: {json_file} not found.")
        return

    if not os.path.exists(db_file):
        print(f"Error: {db_file} not found. Create items database first.")
        return

    with open(json_file, 'r', encoding='utf-8') as f:
        locations = json.load(f)

    conn = sqlite3.connect(db_file)
    c = conn.cursor()

    print("Creating markets table...")

    c.execute('''
        CREATE TABLE IF NOT EXISTS markets (
            LocationIndex TEXT PRIMARY KEY,
            Name          TEXT NOT NULL
        )
    ''')

    c.execute('DELETE FROM markets')

    for loc in locations:
        c.execute(
            'INSERT INTO markets (LocationIndex, Name) VALUES (?, ?)',
            (loc['Index'], loc['UniqueName'])
        )

    conn.commit()
    conn.close()

    print(f"Done. {len(locations)} markets inserted into markets table.")

if __name__ == '__main__':
    create_world_table()
