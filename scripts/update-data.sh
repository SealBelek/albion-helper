#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."

BASE_URL="https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/formatted"

echo "Downloading world.json..."
curl -fsL -o data/world.json.new "$BASE_URL/world.json"

echo "Downloading items.json (~24MB, may take ~80s)..."
curl -fsL -o data/items.json.new "$BASE_URL/items.json"

for f in data/world.json.new data/items.json.new; do
	if [ ! -s "$f" ]; then
		echo "ERROR: $f is empty, download failed"
		rm -f data/world.json.new data/items.json.new
		exit 1
	fi
done

mv data/world.json data/world.json.bak
mv data/world.json.new data/world.json
mv data/items.json data/items.json.bak
mv data/items.json.new data/items.json

rm -f db/items.db db/items.db-shm db/items.db-wal

go build -o albion-helper ./cmd/

echo "Done. Run 'make run' to start with fresh data."