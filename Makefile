.PHONY: serve migrate test lint e2e build build-no-ui

serve:
	go run . serve

build:
	go build ./...

build-no-ui:
	go build -tags no_ui ./...

migrate:
	go run . migrate $(ARGS)

test:
	go build ./... && go vet ./... && go test ./...

lint:
	test -z "$$(gofmt -l .)" && go vet ./...

e2e:
	bash scripts/e2e.sh
