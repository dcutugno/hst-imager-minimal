GOEXE := $(shell go env GOEXE)
BIN := .bin/hst-imager-go$(GOEXE)

.PHONY: build test verify-prototype verify-parity verify clean

build:
	mkdir -p .bin
	go build -o $(BIN) .

test:
	go test ./...

verify-prototype:
	./scripts/verify-go-prototype.sh

verify-parity:
	./scripts/verify-go-legacy-parity.sh

verify: test verify-prototype verify-parity

clean:
	rm -rf .bin
