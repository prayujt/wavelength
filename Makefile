.PHONY: run build test check fmt smoke
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
	gofmt -w cmd internal

smoke: build
	python3 scripts/smoke.py
