FROM alpine
# kbd adds openvt
RUN apk add bash iproute2 python3 curl

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

CMD /build_script.sh