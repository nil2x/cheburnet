FROM --platform=$BUILDPLATFORM golang:1.26.1 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .

ARG TARGETOS TARGETARCH VERSION COMMIT
RUN LDFLAGS="\
    -X github.com/nil2x/cheburnet/internal/config.version=${VERSION} \
    -X github.com/nil2x/cheburnet/internal/config.commit=${COMMIT}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "${LDFLAGS}" -o /src/cheburnet .

FROM debian:13.2-slim

RUN apt update && apt install -y ca-certificates zbar-tools

RUN mkdir -p /usr/local/bin /usr/local/etc/cheburnet /var/log/cheburnet
RUN touch /usr/local/etc/cheburnet/config.json /var/log/cheburnet/output.log

COPY --from=build /src/cheburnet /usr/local/bin/cheburnet

EXPOSE 1080

ENTRYPOINT ["/usr/local/bin/cheburnet"]
CMD ["-config", "/usr/local/etc/cheburnet/config.json"]
