.DEFAULT_GOAL := build
PORT ?= 4174

.PHONY: build serve

build:
	mkdir -p dist
	cp index.html style.css copy.js dist/
	rsync -a --delete assets/ dist/assets/

serve: build
	python3 -m http.server $(PORT) --bind 127.0.0.1 --directory dist
