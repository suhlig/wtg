.PHONY: all test test-unit test-acceptance docs transcripts gendocs checkdocs serve build clean

all: test build

build:
	go build -v ./...

test: test-unit test-acceptance

test-unit:
	go test -v ./internal/... ./tools/...

test-acceptance: transcripts

transcripts:
	go test -v ./acceptance -timeout 120s

gendocs:
	go run ./tools/gendocs site/content/reference

checkdocs:
	go run ./tools/checkdocs

docs: transcripts gendocs checkdocs

serve: docs
	zola --root site serve
