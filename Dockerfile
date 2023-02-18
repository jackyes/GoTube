FROM debian:latest
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y keyboard-configuration golang ffmpeg firejail whois
WORKDIR /app
COPY . .
RUN go build GoTube.go
EXPOSE 8085
CMD ["./GoTube"]
