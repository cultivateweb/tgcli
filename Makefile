BINARY := tgcli
PKG     := ./cmd/tgcli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run test vet fmt tidy clean install

build: ## Собрать бинарник в ./bin
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

run: ## Запустить (переменные: ARGS="send --to @user привет")
	go run $(PKG) $(ARGS)

test: ## Прогнать тесты
	go test ./...

vet: ## Статический анализ
	go vet ./...

fmt: ## Форматирование
	go fmt ./...

tidy: ## Привести go.mod/go.sum в порядок
	go mod tidy

install: ## Установить в $GOBIN
	go install -ldflags "$(LDFLAGS)" $(PKG)

clean: ## Удалить артефакты сборки
	rm -rf bin
