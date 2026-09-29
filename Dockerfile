FROM registry.hub.docker.com/library/golang:1.27.1 as build

COPY . /build
RUN apt-get update; apt-get dist-upgrade -y; apt-get install -y curl jq; cd /build &&\
    go get . &&\
    go install go.elastic.co/go-licence-detector/...@latest &&\
    go list -m -json all | go-licence-detector -includeIndirect -rules=./license-rules.json -noticeTemplate=NOTICE.tpl -noticeOut=./cmd/NOTICE &&\
    cp LICENSE cmd/ &&\
    CGO_ENABLED=0 go build -o gitgut . &&\
    echo "nobody:x:65534:65534:nobody:/_break:/sbin/nologin" > /nobody

FROM scratch

COPY --from=build /nobody /etc/passwd
COPY --from=build --chown=65534:65534 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /build/gitgut /bin/gitgut

USER 65534

CMD ["/bin/gitgut"]