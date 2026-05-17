.PHONY: build test lint docker clean

build:
	go build -o ./bin/analyzer ./cmd/analyzer

test:
	go test ./... -race -count=1

lint:
	golangci-lint run

docker:
	docker build -t releaseguard-analyzer:latest -f deploy/Dockerfile.analyzer .

docker-indexer:
	docker build -t releaseguard-indexer:latest -f deploy/Dockerfile.indexer .

clean:
	rm -rf ./bin

smoke:
	go test ./cmd/analyzer/ -run TestE2E -v
