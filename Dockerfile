FROM golang:1.27 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

ARG VERSION

COPY cmd cmd
COPY *.go .
COPY static static
COPY migrations migrations
COPY Makefile .

RUN VERSION=$VERSION make bin/app

FROM alpine:3.23.0
COPY --from=builder /src/bin/app /app

ENTRYPOINT [ "/app" ]
