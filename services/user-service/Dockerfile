# Build: docker build -t user-service services/user-service

FROM golang:1.27 AS build
WORKDIR /src
# Dependencies first, so code changes do not re-download modules.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Distroless: no shell or package manager, just CA certificates and the binary. The service writes no files,
# so it runs as any UID, including the random non-root UID OpenShift assigns.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/server"]
