FROM harbor.idc.roywong.work/gcr.io/distroless/static:nonroot

WORKDIR /app
COPY docker-credentials-distributor .

CMD ["./docker-credentials-distributor"]

