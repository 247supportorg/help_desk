BINARY_NAME := help-desk
BIN_DIR := bin

.PHONY: build run run-postgres run-mariadb install-ubuntu install-ubuntu-remote clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/server

run:
	go run ./cmd/server

# These targets load DB_DSN from a local env file (ignored by git) so that
# real credentials never get hardcoded in the Makefile. Create help-desk.env
# with your own values, or override DB_DSN on the command line.
ifneq (,$(wildcard ./help-desk.env))
include ./help-desk.env
export
endif

run-postgres:
	@test -n "$(DB_DSN)" || (echo "DB_DSN is not set. Create help-desk.env or export DB_DSN=..." && exit 1)
	STORE_BACKEND=postgres DB_DSN="$(DB_DSN)" go run ./cmd/server

run-mariadb:
	@test -n "$(DB_DSN)" || (echo "DB_DSN is not set. Create help-desk.env or export DB_DSN=..." && exit 1)
	STORE_BACKEND=mariadb DB_DSN="$(DB_DSN)" go run ./cmd/server

install-ubuntu:
	./scripts/install.sh

install-ubuntu-remote:
	@if [ -z "$(REMOTE)" ]; then echo "Usage: make install-ubuntu-remote REMOTE=user@host [SSH_PORT=22]"; exit 1; fi
	./scripts/install.sh --remote $(REMOTE) --ssh-port $(if $(SSH_PORT),$(SSH_PORT),22)

clean:
	rm -rf $(BIN_DIR)
