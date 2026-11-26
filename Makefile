.DEFAULT_GOAL := help
IMAGE := plandillo:local
NAME  := plandillo

.PHONY: help install web build test test-postgres typecheck knip i18n check dev-api dev-web image rebuild stop logs clean

help: ## список целей
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

install: ## зависимости фронта (bun install --frozen-lockfile)
	cd web && bun install --frozen-lockfile

web: ## фронт: tsc + vite build -> internal/web/dist
	cd web && bun run build

build: web ## бинарь bin/plandillo с вшитым UI
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/plandillo ./cmd/plandillo
	ln -f bin/plandillo bin/docket

test: ## тесты бэкенда (HTTP-тесты открывают порт на localhost)
	go test ./...

test-postgres: ## тесты с PostgreSQL (нужен PLANDILLO_TEST_POSTGRES_DSN, отдельный тестовый сервер)
	@test -n "$$PLANDILLO_TEST_POSTGRES_DSN" || (echo "Set PLANDILLO_TEST_POSTGRES_DSN to a disposable PostgreSQL server (role needs CREATEDB)"; exit 1)
	go test ./...

typecheck: ## tsc --noEmit
	cd web && bun run typecheck

knip: ## неиспользуемый код и зависимости фронта
	cd web && bun run knip

i18n: ## у каждой русской строки фронта есть английский и украинский перевод
	cd web && bun run i18n

check: typecheck knip i18n test ## все проверки

dev-api: ## бэкенд на :7777 с отдельной dev-базой
	PLANDILLO_DB=./data/dev.db PLANDILLO_BACKUP_DIR=./data/backups go run ./cmd/plandillo

dev-web: ## Vite с hot reload (проксирует /api на :7777)
	cd web && bun run dev

.env:
	umask 077 && cp .env.example .env

image: ## собрать docker-образ plandillo:local
	docker build -t $(IMAGE) .

# Сначала собирается образ, и только потом удаляется старый контейнер: если сборка упала, старый продолжает работать.
rebuild: .env image ## пересобрать образ и пересоздать контейнер (данные в ./data остаются)
	-docker rm -f $(NAME) docket # docket: the container name before the rename
	mkdir -p -m 700 data
	docker run -d --name $(NAME) --user "$$(id -u):$$(id -g)" --env-file .env -p 127.0.0.1:7777:7777 -v $(CURDIR)/data:/data --restart unless-stopped $(IMAGE)

stop: ## остановить и удалить контейнер
	docker rm -f $(NAME)

logs: ## логи контейнера (Ctrl+C - выйти)
	docker logs -f $(NAME)

clean: ## удалить bin/ и собранный фронт (.gitkeep остаётся)
	rm -rf bin
	find internal/web/dist -mindepth 1 ! -name .gitkeep -delete
