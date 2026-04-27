.PHONY: check-size build clean build-marc build-marc-server

# Local-arch builds with the same ldflags GoReleaser uses for releases.
# CGO_ENABLED=0 for client (matches .goreleaser.yaml marc build),
# CGO_ENABLED=1 for server (matches .goreleaser.yaml marc-server build).
# Stripped (-s -w) per release config.

MARC_LIMIT_MB := 15
MARCSERVER_LIMIT_MB := 35

build: build-marc build-marc-server

build-marc:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o ./dist/marc ./cmd/marc

build-marc-server:
	CGO_ENABLED=1 go build -ldflags="-s -w" -o ./dist/marc-server ./cmd/marc-server

check-size: build
	@./scripts/check-size.sh ./dist/marc $(MARC_LIMIT_MB) marc
	@./scripts/check-size.sh ./dist/marc-server $(MARCSERVER_LIMIT_MB) marc-server

clean:
	rm -rf ./dist
