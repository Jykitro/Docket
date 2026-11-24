ARG POSTGRES_BASE=postgres:18.7-alpine
FROM ${POSTGRES_BASE}
RUN apk add --no-cache --upgrade 'libxml2>=2.13.9-r3' 'libcrypto3>=3.5.9-r1' 'libssl3>=3.5.9-r1' su-exec \
    && grep -q 'exec gosu postgres ' /usr/local/bin/docker-entrypoint.sh \
    && sed -i 's/exec gosu postgres /exec su-exec postgres /' /usr/local/bin/docker-entrypoint.sh \
    && rm /usr/local/bin/gosu
COPY init-role.sql /docker-entrypoint-initdb.d/10-docket-role.sql
