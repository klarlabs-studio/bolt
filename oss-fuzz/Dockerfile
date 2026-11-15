# OSS-Fuzz Go base image. Pinned to gcr.io/oss-fuzz-base/base-builder-go
# per the OSS-Fuzz Go integration guide:
#   https://google.github.io/oss-fuzz/getting-started/new-project-guide/go-lang/
FROM gcr.io/oss-fuzz-base/base-builder-go
LABEL org.opencontainers.image.authors="klarlabs-studio https://github.com/klarlabs-studio/bolt"

RUN git clone --depth 1 https://github.com/klarlabs-studio/bolt $SRC/bolt

WORKDIR $SRC/bolt

COPY build.sh $SRC/
