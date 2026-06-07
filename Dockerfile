# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# The engine core is pure Go (CGO disabled). When the `lame` (CGo) and `video`
# (ffmpeg) transformers land, this stage gains lame-dev and the final image gains
# the ffmpeg/lame runtime.
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /out/bumblebee ./cmd/bumblebee

# Run stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates
WORKDIR /app
RUN addgroup -S bumblebee && adduser -S -G bumblebee bumblebee
COPY --from=build /out/bumblebee /usr/local/bin/bumblebee
COPY examples/passthrough.yaml /app/config.yaml
RUN chown -R bumblebee:bumblebee /app
USER bumblebee
EXPOSE 8080
ENTRYPOINT ["bumblebee", "-config", "/app/config.yaml"]
