.PHONY: all test test-unit test-acceptance docs transcripts gendocs checkdocs check-determinism serve build clean

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

# ADR 0027 D5: the same binary must produce byte-identical transcripts on every run.
check-determinism:
	go test ./acceptance -count=1 -timeout 120s
	cat site/data/transcripts/*.json | shasum -a 256 > .transcripts.sha
	go test ./acceptance -count=1 -timeout 120s
	cat site/data/transcripts/*.json | shasum -a 256 | cmp - .transcripts.sha
	rm -f .transcripts.sha
