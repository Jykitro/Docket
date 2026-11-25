.DEFAULT_GOAL := build
PORT ?= 4174
# Astro asks to send anonymous usage data; the landing opts out.
export ASTRO_TELEMETRY_DISABLED := 1

.PHONY: build serve

node_modules: package.json bun.lock
	bun install --frozen-lockfile
	touch node_modules

build: node_modules
	bun run build

serve: node_modules
	bun run dev --port $(PORT)
