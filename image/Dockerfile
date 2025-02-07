FROM alpine
# kbd adds openvt
RUN apk add bash sysbench iproute2

COPY build_script.sh /

COPY overlay-init /sbin/overlay-init

RUN addgroup \
    --gid "1000" \
    "user" \
    &&  adduser \
    --disabled-password \
    --gecos "" \
    --home "/home/user" \
    --ingroup "user" \
    --uid "1000" \
    user
# --no-create-home \

RUN echo "ttyS0::respawn:/bin/bash" >> /etc/inittab

CMD /build_script.sh