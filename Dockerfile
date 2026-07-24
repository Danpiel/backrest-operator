# Go operator (+ export-proxy for PVCRestore export Jobs)
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/operator ./cmd/operator \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/export-proxy ./cmd/export-proxy

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/operator /operator
COPY --from=build /out/export-proxy /export-proxy
USER 65532:65532
EXPOSE 8080 8081 9443
ENTRYPOINT ["/operator"]
