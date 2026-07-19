.PHONY: run build tidy test clean

run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -rf bin
