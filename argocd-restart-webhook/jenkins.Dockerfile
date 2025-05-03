FROM harbor.idc.roywong.work/gcr.io/distroless/static:nonroot

WORKDIR /app
COPY argocd-restart-webhook .

ENTRYPOINT ["./argocd-restart-webhook"]