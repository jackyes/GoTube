FROM golang:1.20.1-bullseye as builder
WORKDIR /go/src/app
COPY . .

# Install build dependencies
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        git \
        build-essential \
        pkg-config \
        autoconf \
        libtool \
        libssl-dev \
        zlib1g-dev \
        libbz2-dev \
    && rm -rf /var/lib/apt/lists/*

# Build and install GPAC
RUN git clone https://github.com/gpac/gpac.git \
    && cd gpac \
    && ./configure \
    && make -j $(nproc) && make install \
    && cd .. \
    && rm -rf gpac

# Download Go modules and build the binary
RUN go mod download \
    && CGO_ENABLED=1 go build -ldflags="-s -w" -o /go/bin/GoTube

# Stage 2: runtime
FROM debian:stable-slim
COPY --from=builder /usr/local/lib /usr/local/lib
COPY --from=builder /usr/local/bin/MP4Box /usr/local/bin/
COPY --from=builder /go/bin/GoTube /usr/local/bin/GoTube
COPY . .
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y ffmpeg \
    && rm -rf /var/lib/apt/lists/*
EXPOSE 8085
ENTRYPOINT ["GoTube"]
