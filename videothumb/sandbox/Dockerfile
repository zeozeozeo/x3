# ffmpeg sandbox for video thumbnail extraction. Built alongside the code
# interpreter sandbox and run under gVisor (runsc):
#   docker build -t x3-ffmpeg-sandbox:latest videothumb/sandbox
FROM alpine:3.20

RUN apk add --no-cache ffmpeg

# Thumbnails only ever read a video from stdin and write one JPEG frame to
# stdout; no network, no mounts, read-only root filesystem at runtime.
USER 0:0
WORKDIR /tmp
