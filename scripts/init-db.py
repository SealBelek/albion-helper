import json
import sqlite3
import os

def create_database(json_file='./data/items.json', db_file='./db/items.db'):
    # Remove existing database if it exists
    if os.path.exists(db_file):
        os.remove(db_file)
        print(f"Removed existing {db_file}")
    
    conn = sqlite3.connect(db_file)
    conn.execute("PRAGMA foreign_keys = ON")
    c = conn.cursor()
    
    print("Creating tables...")
    
    # Create main items table
    c.execute('''
        CREATE TABLE items (
            UniqueName TEXT PRIMARY KEY,
            "Index" INTEGER,
            LocalizationNameVariable TEXT,
            LocalizationDescriptionVariable TEXT
        )
    ''')
    
    # Create localizations table
    c.execute('''
        CREATE TABLE item_localizations (
            UniqueName TEXT NOT NULL,
            Language TEXT NOT NULL,
            Name TEXT,
            Description TEXT,
            PRIMARY KEY (UniqueName, Language),
            FOREIGN KEY (UniqueName) REFERENCES items(UniqueName)
        )
    ''')
    
    # Create indexes for fast lookups
    c.execute('CREATE INDEX idx_loc_language ON item_localizations(Language)')
    c.execute('CREATE INDEX idx_loc_lang_name ON item_localizations(Language, Name)')
    
    # Create FTS table with unicode61 tokenizer for European languages
    c.execute('''
        CREATE VIRTUAL TABLE items_fts_european USING fts5(
            UniqueName UNINDEXED,
            Language,
            Name,
            tokenize='unicode61 remove_diacritics 0'
        )
    ''')
    
    # Try to create FTS table with trigram tokenizer for CJK languages
    try:
        c.execute('''
            CREATE VIRTUAL TABLE items_fts_cjk USING fts5(
                UniqueName UNINDEXED,
                Language,
                Name,
                tokenize='trigram'
            )
        ''')
        has_trigram = True
        print("Trigram tokenizer available for CJK languages")
    except sqlite3.OperationalError:
        print("Trigram tokenizer not available, using unicode61 for all languages")
        has_trigram = False
    
    # Load JSON data
    print(f"Loading data from {json_file}...")
    with open(json_file, 'r', encoding='utf-8') as f:
        items = json.load(f)
    
    print(f"Found {len(items)} items")
    
    # Language categories
    european_langs = {'EN-US', 'DE-DE', 'FR-FR', 'RU-RU', 'PL-PL', 'ES-ES', 
                      'PT-BR', 'IT-IT', 'ID-ID', 'TR-TR', 'AR-SA'}
    cjk_langs = {'ZH-CN', 'ZH-TW', 'KO-KR', 'JA-JP'}
    
    # Insert data
    print("Inserting items...")
    item_count = 0
    loc_count = 0
    
    for item in items:
        un = item['UniqueName']
        idx = int(item.get('Index', 0))
        lnv = item.get('LocalizationNameVariable')
        ldv = item.get('LocalizationDescriptionVariable')
        
        # Insert main item
        c.execute(
            'INSERT INTO items(UniqueName, "Index", LocalizationNameVariable, LocalizationDescriptionVariable) VALUES (?,?,?,?)',
            (un, idx, lnv, ldv)
        )
        item_count += 1
        
        # Handle localizations
        loc_names = item.get('LocalizedNames') or {}
        loc_descs = item.get('LocalizedDescriptions') or {}
        
        # Get all unique language codes
        languages = set()
        if isinstance(loc_names, dict):
            languages.update(loc_names.keys())
        if isinstance(loc_descs, dict):
            languages.update(loc_descs.keys())
        
        # Insert each language
        for lang in languages:
            name = loc_names.get(lang) if isinstance(loc_names, dict) else None
            desc = loc_descs.get(lang) if isinstance(loc_descs, dict) else None
            
            c.execute(
                'INSERT INTO item_localizations(UniqueName, Language, Name, Description) VALUES (?,?,?,?)',
                (un, lang, name, desc)
            )
            loc_count += 1
            
            # Insert into appropriate FTS table
            if name:
                if has_trigram and lang in cjk_langs:
                    c.execute(
                        'INSERT INTO items_fts_cjk(UniqueName, Language, Name) VALUES (?,?,?)',
                        (un, lang, name)
                    )
                else:
                    c.execute(
                        'INSERT INTO items_fts_european(UniqueName, Language, Name) VALUES (?,?,?)',
                        (un, lang, name)
                    )
        
        # Progress indicator
        if item_count % 500 == 0:
            print(f"  Processed {item_count} items...")
    
    print(f"Inserted {item_count} items and {loc_count} localizations")
    
    # Verify data
    print("\nVerifying database...")
    
    c.execute('SELECT COUNT(*) FROM items')
    item_count = c.fetchone()[0]
    print(f"Items table: {item_count} rows")
    
    c.execute('SELECT COUNT(*) FROM item_localizations')
    loc_count = c.fetchone()[0]
    print(f"Localizations table: {loc_count} rows")
    
    c.execute('SELECT COUNT(DISTINCT Language) FROM item_localizations')
    lang_count = c.fetchone()[0]
    print(f"Languages: {lang_count}")
    
    c.execute('SELECT DISTINCT Language FROM item_localizations ORDER BY Language')
    languages = c.fetchall()
    print("Available languages:")
    for lang in languages:
        c.execute('SELECT COUNT(*) FROM item_localizations WHERE Language = ?', (lang[0],))
        count = c.fetchone()[0]
        print(f"  {lang[0]}: {count} entries")
    
    # Create views for convenience
    print("\nCreating convenience views...")
    
    c.executescript('''
        -- View for English items
        CREATE VIEW IF NOT EXISTS v_items_en AS
        SELECT i.*, loc.Name, loc.Description
        FROM items i
        LEFT JOIN item_localizations loc ON i.UniqueName = loc.UniqueName AND loc.Language = 'EN-US';
        
        -- View for German items
        CREATE VIEW IF NOT EXISTS v_items_de AS
        SELECT i.*, loc.Name, loc.Description
        FROM items i
        LEFT JOIN item_localizations loc ON i.UniqueName = loc.UniqueName AND loc.Language = 'DE-DE';
        
        -- View for Russian items
        CREATE VIEW IF NOT EXISTS v_items_ru AS
        SELECT i.*, loc.Name, loc.Description
        FROM items i
        LEFT JOIN item_localizations loc ON i.UniqueName = loc.UniqueName AND loc.Language = 'RU-RU';
    ''')
    
    conn.commit()
    conn.close()
    
    print(f"\n✅ Database '{db_file}' created successfully!")
    print("\nUsage examples:")
    print("  sqlite3 items.db")
    print("  SELECT * FROM items WHERE UniqueName = 'UNIQUE_HIDEOUT';")
    print("  SELECT * FROM v_items_de WHERE Name LIKE '%Bau%';")
    print("  SELECT DISTINCT i.*, loc.Name, loc.Description")
    print("  FROM items i")
    print("  JOIN item_localizations loc ON i.UniqueName = loc.UniqueName")
    print("  WHERE loc.Language = 'RU-RU' AND loc.Name LIKE '%убежищ%';")

if __name__ == '__main__':
    create_database()
