FROM alpine:3.20

RUN apk add --no-cache bash openssh \
    && adduser -D -s /bin/bash hop \
    && passwd -d hop \
    && mkdir -p /home/hop/.ssh /keys \
    && chown -R hop:hop /home/hop

COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh

EXPOSE 2222
ENTRYPOINT ["/entrypoint.sh"]
