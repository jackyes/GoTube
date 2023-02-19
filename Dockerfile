FROM golang:1.20.1-bullseye as builder
WORKDIR /app
COPY . .
RUN go mod download
RUN go build GoTube.go
FROM debian:stable-slim
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y ffmpeg firejail && rm -rf /var/lib/apt/lists/*
COPY . .
COPY --from=builder /app/GoTube /app/GoTube
EXPOSE 8085
CMD ["./app/GoTube"]
