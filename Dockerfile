# Stage 1: builder
FROM golang:1.21.3-bullseye AS builder
WORKDIR /go/src/app
COPY . .

# Install build dependencies and build GPAC
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git \
        build-essential \
        pkg-config \
        autoconf \
        libtool \
        libssl-dev \
        zlib1g-dev \
        libbz2-dev && \
    git clone https://github.com/gpac/gpac.git && \
    cd gpac && \
    ./configure && \
    make -j $(nproc) && make install && \
    cd .. && \
    rm -rf gpac && \
    rm -rf /var/lib/apt/lists/*

# Download Go modules and build the binary
RUN go get -u -v all
RUN go mod download && \
    CGO_ENABLED=1 go build -ldflags="-s -w" -o /go/bin/GoTube

# Stage 2: runtime
FROM debian:stable-slim

# Create a dedicated/non-privileged user to run the app.
RUN addgroup gotube && \
    useradd -r -g gotube -d /home/gotube -s /sbin/nologin -c "GoTube User" gotube && \
    mkdir /uploads /converted /pages /static && \
    chown -R gotube:gotube /uploads /converted /pages /static

COPY --from=builder /usr/local/lib /usr/local/lib
COPY --from=builder /usr/local/bin/MP4Box /usr/local/bin/
COPY --from=builder /go/bin/GoTube /usr/local/bin/GoTube
COPY . .
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && \
    apt-get install -y ffmpeg && \
    rm -rf /var/lib/apt/lists/*

USER gotube
EXPOSE 8085

ENTRYPOINT ["GoTube"]
