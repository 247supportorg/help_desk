BINARY_NAME := help-desk
BIN_DIR := bin

.PHONY: build run clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/server

run:
	go run ./cmd/server

clean:
	rm -rf $(BIN_DIR)
