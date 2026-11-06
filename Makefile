.DEFAULT_GOAL := help
IMAGE := docket:local
NAME  := docket

.PHONY: help install web build test typecheck knip check dev-api dev-web image rebuild stop logs clean

help: ## список целей
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

install: ## зависимости фронта (bun install --frozen-lockfile)
	cd web && bun install --frozen-lockfile

web: ## фронт: tsc + vite build -> internal/web/dist
	cd web && bun run build

build: web ## бинарь bin/docket с вшитым UI
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/docket ./cmd/docket

test: ## тесты бэкенда (HTTP-тесты открывают порт на localhost)
	go test ./...

typecheck: ## tsc --noEmit
	cd web && bun run typecheck

knip: ## неиспользуемый код и зависимости фронта
	cd web && bun run knip

check: typecheck knip test ## все проверки

dev-api: ## бэкенд на :7777 с отдельной dev-базой
	DOCKET_DB=./data/dev.db DOCKET_BACKUP_DIR=./data/backups go run ./cmd/docket

dev-web: ## Vite с hot reload (проксирует /api на :7777)
	cd web && bun run dev

.env:
	cp .env.example .env

image: ## собрать docker-образ docket:local
	docker build -t $(IMAGE) .

# Сначала собирается образ, и только потом удаляется старый контейнер: если сборка упала, старый продолжает работать.
rebuild: .env image ## пересобрать образ и пересоздать контейнер (данные в ./data остаются)
	-docker rm -f $(NAME)
	mkdir -p data
	docker run -d --name $(NAME) --env-file .env -p 127.0.0.1:7777:7777 -v $(CURDIR)/data:/data --restart unless-stopped $(IMAGE)

stop: ## остановить и удалить контейнер
	docker rm -f $(NAME)

logs: ## логи контейнера (Ctrl+C - выйти)
	docker logs -f $(NAME)

clean: ## удалить bin/ и собранный фронт (.gitkeep остаётся)
	rm -rf bin
	find internal/web/dist -mindepth 1 ! -name .gitkeep -delete
