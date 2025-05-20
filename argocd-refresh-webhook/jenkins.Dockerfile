FROM harbor.idc.roywong.work/gcr.io/distroless/static:nonroot

WORKDIR /app
COPY argocd-refresh-webhook .

ENTRYPOINT ["./argocd-refresh-webhook"]