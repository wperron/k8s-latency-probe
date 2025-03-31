FROM golang:1.24-bullseye AS builder
WORKDIR /app
COPY . .
RUN mkdir ./bin; go build -o ./bin/probe ./main.go

FROM ubuntu:24.04
WORKDIR /probe
COPY --from=builder /app/bin/probe /usr/local/bin/probe
RUN apt update -y; apt install -y ca-certificates ; apt-get clean
ENTRYPOINT ["/usr/local/bin/probe"]
