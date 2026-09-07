FROM mariadb:12.3.3@sha256:dd9b303aed4f4890ed09f766d8ca9ddfd176c0c6f6267feff53b3192ec65a979

COPY my.cnf /etc/mysql/conf.d/my.cnf

ARG INNODB_BUFFER_POOL_SIZE="256MB"

RUN echo "[mysqld]" >> /etc/mysql/conf.d/my.cnf && \
	echo "innodb_buffer_pool_size  = ${INNODB_BUFFER_POOL_SIZE}" >> /etc/mysql/conf.d/my.cnf

EXPOSE 3306
