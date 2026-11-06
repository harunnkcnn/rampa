VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/harunnkcnn/rampa/app.Version=$(VERSION)

.PHONY: run test build dist demo

run:
	go run ./cmd/rampa -db dev.db

test:
	go vet ./... && go test -race ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o rampa ./cmd/rampa

demo:
	go run ./cmd/rampa-demo -db demo.db && go run ./cmd/rampa -db demo.db

dist:
	@mkdir -p dist
	for target in windows/amd64 linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/rampa-$(VERSION)-$$os-$$arch$$ext ./cmd/rampa; \
	done
