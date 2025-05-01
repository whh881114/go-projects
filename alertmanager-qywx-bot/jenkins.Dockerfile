FROM harbor.idc.roywong.work/gcr.io/distroless/static:nonroot

WORKDIR /app
COPY alertmanager-qywx-bot .

CMD ["./alertmanager-qywx-bot"]

