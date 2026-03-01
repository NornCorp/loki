FROM alpine:3.21 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
ARG TARGETOS
ARG TARGETARCH
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY build/${TARGETOS}/${TARGETARCH}/polymorph /usr/local/bin/polymorph
ENTRYPOINT ["polymorph"]
