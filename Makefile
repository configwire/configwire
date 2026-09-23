.PHONY: serve migrate test lint e2e

serve:
	go run . serve

migrate:
	go run . migrate $(ARGS)

test:
	go build ./... && go vet ./... && go test ./...

lint:
	test -z "$$(gofmt -l .)" && go vet ./...

e2e:
	bash scripts/e2e.sh
