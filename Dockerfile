# Build stage
FROM golang:1.26-alpine AS build
RUN apk --no-cache add gcc musl-dev lame-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# lame is CGo (libmp3lame); image/passthrough are pure-Go.
RUN CGO_ENABLED=1 go build -ldflags "-X main.version=${VERSION}" -o /out/bumblebee ./cmd/bumblebee

# Run stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates lame-libs ffmpeg
WORKDIR /app
RUN addgroup -S bumblebee && adduser -S -G bumblebee bumblebee
COPY --from=build /out/bumblebee /usr/local/bin/bumblebee
COPY examples/passthrough.yaml /app/config.yaml
RUN chown -R bumblebee:bumblebee /app
USER bumblebee
EXPOSE 8080
ENTRYPOINT ["bumblebee", "-config", "/app/config.yaml"]
