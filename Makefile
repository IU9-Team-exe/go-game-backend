NAME := team_exe
MAIN := cmd/main.go

PKG := `go list -mod=mod -f {{.Dir}} ./...`

RUNFLAGS := --config .env

all: build
init: mod-tidy install-swag install-gci install-lint

run: build
	@echo "Starting app..."
	./bin/$(NAME) $(RUNFLAGS) poll

.PHONY: build
build:
	@mkdir -p bin
	@go build -mod=mod -o bin/$(NAME) $(MAIN)

mod-tidy:
	go mod tidy

mod-download:
	go mod download all

install-swag:
	go install github.com/swaggo/swag/cmd/swag@latest

install-gci:
	go install github.com/daixiang0/gci@latest

install-lint:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin latest

pre-commit: lint test

fmt:
	go fmt ./...
	# gci write -s standard -s default -s "Prefix(github.com/re-tofl/tofl-gpt-chat)" -s blank -s dot $(PKG)

lint: fmt
	golangci-lint run -E wsl -E bodyclose -E errname

.PHONY: test
test:
	go test ./... -count=1 -p=1

.PHONY: cover
cover:
	mkdir -p .coverage
	go test ./... -count=1 -p=1 -coverprofile .coverage/cover.out
	go tool cover -html=.coverage/cover.out -o .coverage/cover.html
