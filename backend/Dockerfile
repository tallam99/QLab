# syntax=docker/dockerfile:1

# --- build stage: compile a static binary -------------------------------------
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO off + static linking so the binary runs on the distroless static image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime stage: minimal, non-root ----------------------------------------
# distroless/static = libc-less base with CA certs and tzdata, no shell/package
# manager. No frontend is embedded — the PWA ships separately via Firebase
# Hosting (see docs/decisions/0001).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
# Documents the server's default listen port (config default, used locally).
# Cloud Run ignores EXPOSE and injects its own PORT, which the server respects.
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/server"]
