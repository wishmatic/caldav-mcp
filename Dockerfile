# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/server ./cmd/server

# The cache lives on a volume, and the image runs as nonroot, so the directory
# has to exist with the right owner.
RUN mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /bin/server /server
COPY --from=build --chown=nonroot:nonroot /data /data

# Mount a volume here, or the cache is lost with the container.
VOLUME /data

EXPOSE 8080

USER nonroot:nonroot

# The server fetches on its own schedule, every SYNC_INTERVAL_SECONDS. Set that
# to 0 and run `/server sync` from a timer elsewhere to fetch on yours instead.
ENTRYPOINT ["/server"]
