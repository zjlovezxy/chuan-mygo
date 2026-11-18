# Builds the signalling server and the browser bundle into one small image.
FROM golang:1.27-alpine AS build

WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/chuan-server ./cmd/chuan-server \
 && GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w" -o /out/web/app.wasm ./cmd/chuan \
 && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /out/web/ \
 && cp web/index.html web/bridge.js /out/web/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/chuan-server /usr/local/bin/chuan-server
COPY --from=build /out/web /srv/web
EXPOSE 8080
ENTRYPOINT ["chuan-server", "-addr", ":8080", "-static", "/srv/web"]
