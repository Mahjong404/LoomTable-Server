FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/loomtable-server ./cmd/loomtable-server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/loomtable-migrate ./cmd/loomtable-migrate

FROM alpine:3.20

RUN addgroup -S loomtable && adduser -S -G loomtable loomtable
WORKDIR /app
COPY --from=build /out/loomtable-server /app/loomtable-server
COPY --from=build /out/loomtable-migrate /app/loomtable-migrate
COPY migrations /app/migrations
RUN mkdir -p /var/lib/loomtable/attachments && chown -R loomtable:loomtable /app /var/lib/loomtable
USER loomtable
EXPOSE 31201
ENTRYPOINT ["/app/loomtable-server"]
