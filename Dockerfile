# ChainGO — image du nœud (API + P2P + site embarqué)
# Build :  docker build -t chaingo .
# Run   :  docker run -d -p 8545:8545 -p 9000:9000 -v chaingo-data:/data chaingo
#          (lance un nœud testnet public par défaut — voir CMD ci-dessous)

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
# Testnet public par défaut. Pour rejoindre un réseau existant, surcharger :
#   docker run ... chaingo node start --genesis-url https://<seed>/v1/genesis --peers <seed>:9000 --web /web
CMD ["node", "start", "--testnet", "--datadir", "/data", "--api", ":8545", "--p2p", ":9000", "--web", "/web"]
