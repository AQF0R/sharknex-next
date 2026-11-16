.PHONY: dev dev-api dev-web build check migrate console-install console-build console-check pi-install pi-check
dev:
	node scripts/dev.mjs
dev-api:
	go run ./cmd/engine --mode dev
dev-web:
	npm --prefix web run dev
console-install:
	npm --prefix web ci
console-build:
	npm --prefix web run build
console-check:
	npm --prefix web run lint
	npm --prefix web run type-check
build:
	go build -trimpath -ldflags "-w -s" -o .dev/engine ./cmd/engine
check:
	go vet ./...
	go build -trimpath -ldflags "-w -s" ./...
pi-install:
	npm --prefix pi-adapter ci
pi-check:
	npm --prefix pi-adapter run check
migrate:
	go run ./cmd/engine --migrate
