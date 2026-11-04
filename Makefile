BIN := bin/gamehost

.PHONY: build test lint fmt run clean

build:
	go build -o $(BIN) ./cmd/gamehost

test:
	go test -race ./...

lint:
	go vet ./...

fmt:
	gofmt -l -w .

run:
	go run ./cmd/gamehost version

clean:
	rm -rf bin
