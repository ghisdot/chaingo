# ChainGO — image du nœud (API + P2P + site embarqué)
# Build :  docker build -t chaingo .
# Run   :  docker run -d -p 8545:8545 -p 9000:9000 -v chaingo-data:/data chaingo node start --dev --datadir /data --web /web

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /chaingo ./cmd/chaingo

FROM alpine:3.21
RUN adduser -D chaingo
COPY --from=build /chaingo /usr/local/bin/chaingo
COPY web /web
USER chaingo
VOLUME /data
EXPOSE 8545 9000
ENTRYPOINT ["chaingo"]
CMD ["node", "start", "--datadir", "/data", "--api", ":8545", "--p2p", ":9000", "--web", "/web"]
