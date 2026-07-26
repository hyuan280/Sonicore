FROM golang:1.26-alpine AS go-builder

ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o sonicore ./cmd/sonicore

FROM node:22-alpine AS web-builder

RUN npm config set registry https://registry.npmmirror.com

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM alpine:3.20 AS ffplay-builder

RUN apk add --no-cache build-base sdl2-dev ffmpeg-dev yasm wget xz

WORKDIR /build
RUN wget -q https://ffmpeg.org/releases/ffmpeg-6.1.1.tar.xz && \
    tar xf ffmpeg-6.1.1.tar.xz && \
    cd ffmpeg-6.1.1 && \
    ./configure --enable-ffplay --enable-sdl2 --disable-doc && \
    make -j$(nproc)

FROM alpine:3.20

RUN apk add --no-cache ca-certificates ffmpeg pulseaudio-utils tzdata

RUN addgroup -g 1000 sonicore && adduser -u 1000 -G sonicore -D -h /opt/sonicore sonicore

WORKDIR /opt/sonicore
RUN mkdir -p bin web music data/images data/cache && chown -R sonicore:sonicore /opt/sonicore

COPY --from=go-builder /build/sonicore bin/sonicore
COPY --from=web-builder /web/dist web/
COPY --from=ffplay-builder /build/ffmpeg-6.1.1/ffplay /usr/local/bin/ffplay

ENV SONICORE_SERVER_HOST=0.0.0.0
ENV SONICORE_SERVER_WEB_DIR=/opt/sonicore/web
ENV SONICORE_DATA_MUSIC_DIR=/opt/sonicore/music
ENV SONICORE_DATA_DATA_DIR=/opt/sonicore/data
ENV SONICORE_DATA_IMAGES_DIR=/opt/sonicore/data/images
ENV SONICORE_DATA_CACHE_DIR=/opt/sonicore/data/cache

USER sonicore
EXPOSE 4530

ENTRYPOINT ["bin/sonicore"]
