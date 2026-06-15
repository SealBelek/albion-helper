.PHONY: all build run lint tidy clean db-init db-reset

BINARY := albion-helper
CMD    := ./cmd/
DB     := db/items.db

all: build

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

lint:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

db-init:
	python3 scripts/init-db.py
	python3 scripts/prices-db.py
	python3 scripts/world-db.py

db-reset:
	rm -f $(DB)
	$(MAKE) db-init
