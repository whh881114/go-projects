FROM harbor.idc.roywong.work/gcr.io/distroless/static:nonroot

WORKDIR /app
COPY docker-image-tag .

CMD ["./docker-image-tag"]

