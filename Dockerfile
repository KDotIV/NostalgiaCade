# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26.11
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ncade-api ./cmd/ncade-api
COPY internal/apihost ./internal/apihost
COPY internal/catalogapi ./internal/catalogapi
COPY internal/emucatalog ./internal/emucatalog
COPY internal/postgres ./internal/postgres
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/ncade-api ./cmd/ncade-api

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ncade-api /ncade-api
USER 65532:65532
ENV NCADE_API_LISTEN=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/ncade-api"]
