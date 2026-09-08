VERSION ?= dev
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

dev:
	air

tw:
	tailwindcss -i ./static/css/input.css -o ./static/css/output.css -w

bin/app:
	CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o bin/app ./cmd

test:
	go test ./...
