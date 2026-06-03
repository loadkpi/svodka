.DEFAULT_GOAL := help
.PHONY: help tidy vet build run login

help:
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

tidy: ## go mod tidy
	go mod tidy

vet: ## go vet ./...
	go vet ./...

build:
	go build -o bin/svodka ./api/cmd/svodka
	go build -o bin/login ./api/cmd/login

run: ## go run svodka (требует M1)
	go run ./api/cmd/svodka

login: ## go run login (требует M2)
	go run ./api/cmd/login
