.PHONY: all build run lint tidy clean db-reset

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

db-reset:
	rm -f $(DB) $(DB)-shm $(DB)-wal
