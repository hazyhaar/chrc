BIN_DIR=bin
BINARY=$(BIN_DIR)/chrc

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -v -o $(BINARY) ./cmd/chrc/

test:
	go test -race -count=1 -timeout 120s ./...

clean:
	rm -rf $(BIN_DIR)

.PHONY: build test clean
