.PHONY: run build test check fmt smoke generate compatibility web generate-web
run:
	go run ./cmd/wavelength
build:
	go build -o bin/wavelength ./cmd/wavelength
test:
	go test -race ./...
check:
	go vet ./...
	go test -race ./...
fmt:
	gofmt -w cmd internal gen

smoke: build
	python3 scripts/smoke.py

generate:
	sh scripts/generate.sh

compatibility: build
	uv run scripts/client-compatibility.py
	uv run scripts/validate-subsonic.py

web:
	npm --prefix web ci
	npm --prefix web run check
	npm --prefix web run build

generate-web:
	npm --prefix web run generate
