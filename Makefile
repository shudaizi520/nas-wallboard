GO ?= go
NODE ?= node
NPX ?= npx
DOTNET ?= dotnet
IMAGE ?= nas-wallboard:local
VERSION ?= dev
REPOSITORY ?=
DESKTOP_OUTPUT ?= web/downloads

.PHONY: test go-test web-test windows-test desktop-publish vet build image compose-check

test: go-test web-test

go-test:
	$(GO) test ./...

web-test:
	$(NODE) --test web/*.test.mjs

windows-test:
	$(DOTNET) test windows/NASWallboard.Desktop.sln -c Release

desktop-publish:
	$(DOTNET) publish windows/NASWallboard.Desktop/NASWallboard.Desktop.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true -p:WallboardReleaseVersion=$(if $(filter v%,$(VERSION)),$(VERSION),) -o $(DESKTOP_OUTPUT)

vet:
	$(GO) vet ./...

build:
	$(GO) build -trimpath -ldflags="-s -w -buildid= -X main.version=$(VERSION) -X main.repository=$(REPOSITORY)" -o wallboard ./cmd/wallboard

image: desktop-publish
	docker build --build-arg VERSION=$(VERSION) --build-arg REPOSITORY=$(REPOSITORY) --tag $(IMAGE) .

compose-check:
	docker compose -f compose.yaml config --quiet
	docker compose -f compose.dev.yaml config --quiet
