BINARY     := picklock
MCP_BINARY := picklock-mcp
PKG        := ./cmd/picklock
MCP_PKG    := ./cmd/picklock-mcp

.PHONY: build build-mcp test lint clean demo demo-pg demo-mysql demo-maria demo-mssql demo-mongo demo-redis demo-cassandra demo-neo4j demo-all

build:
	go build -o bin/$(BINARY) $(PKG)

build-mcp:
	go build -o bin/$(MCP_BINARY) $(MCP_PKG)

# Network engine tests are skipped without env vars. See README.
test:
	go test ./...

lint:
	go vet ./...

# Creates the example database. Without a target it seeds SQLite (demo.db, no
# server needed); the network engines need their server running first
# (docker compose up -d). Usage: make demo && ./bin/picklock
demo:
	go run ./cmd/demo

demo-pg:
	go run ./cmd/demo --postgres

demo-mysql:
	go run ./cmd/demo --mysql

demo-maria:
	go run ./cmd/demo --mariadb

demo-mssql:
	go run ./cmd/demo --mssql

demo-mongo:
	go run ./cmd/demo --mongo

demo-redis:
	go run ./cmd/demo --redis

demo-cassandra:
	go run ./cmd/demo --cassandra

demo-neo4j:
	go run ./cmd/demo --neo4j

demo-all:
	go run ./cmd/demo --all

clean:
	rm -rf bin

# Multi-platform build.
#
# The SQLite driver is modernc.org/sqlite (pure Go), so cross-compiling works
# with CGO_ENABLED=0 on all platforms, no gcc required.
release:
	GOOS=linux    GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(BINARY)-linux-amd64   $(PKG)
	GOOS=darwin   GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(BINARY)-darwin-amd64  $(PKG)
	GOOS=darwin   GOARCH=arm64 CGO_ENABLED=0 go build -o bin/$(BINARY)-darwin-arm64  $(PKG)
	GOOS=windows  GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(BINARY)-windows-amd64.exe $(PKG)
