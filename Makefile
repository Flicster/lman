VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

ifeq ($(shell go env GOOS),darwin)
DESKTOP_ENV := CGO_LDFLAGS="-framework UniformTypeIdentifiers"
endif

.PHONY: all cli desktop app test lint

all: cli desktop

# lman: the terminal commands.
cli:
	go build -ldflags "$(LDFLAGS)" -o bin/lman ./cmd/lman

# lman-desktop: the window as a plain binary, without the Wails CLI.
desktop:
	$(DESKTOP_ENV) go build -tags desktop,production -ldflags "$(LDFLAGS)" -o bin/lman-desktop .

# Application bundle: Lman.app on macOS, lman-desktop.exe on Windows, lman-desktop on Linux.
# Needs `go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0`.
app:
	wails build -ldflags "$(LDFLAGS)"

test:
	go test -race ./internal/...

lint:
	go vet ./...
	staticcheck ./...
