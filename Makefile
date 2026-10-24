.PHONY: run build test check fmt smoke generate
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
