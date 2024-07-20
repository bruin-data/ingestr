FROM python:3.11-slim

WORKDIR /app

COPY ./requirements.txt /app/requirements.txt

# Setup dependencies for pyodbc
RUN \
  export ACCEPT_EULA='Y' && \
  export MYSQL_CONNECTOR='mysql-connector-odbc-8.0.33-linux-glibc2.28-x86-64bit' && \
  export MYSQL_CONNECTOR_CHECKSUM='41d03d5df0c631f8071cc697f7714620' && \
  # Install build dependencies
  apt-get update && \
  apt-get install -y curl gcc libpq-dev build-essential unixodbc-dev g++ apt-transport-https && \
  # Install pyodbc db drivers for MSSQL, PG and MySQL
  curl -sSL https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor > /usr/share/keyrings/microsoft-prod.gpg && \
  curl -sSL https://packages.microsoft.com/config/debian/12/prod.list | tee /etc/apt/sources.list.d/mssql-release.list && \
  # install the mysql connector
  curl -L -o ${MYSQL_CONNECTOR}.tar.gz https://dev.mysql.com/get/Downloads/Connector-ODBC/8.0/${MYSQL_CONNECTOR}.tar.gz && \
  echo "${MYSQL_CONNECTOR_CHECKSUM} ${MYSQL_CONNECTOR}.tar.gz" | md5sum -c - && \
  gunzip ${MYSQL_CONNECTOR}.tar.gz && tar xvf ${MYSQL_CONNECTOR}.tar && \
  cp -r ${MYSQL_CONNECTOR}/bin/* /usr/local/bin && cp -r ${MYSQL_CONNECTOR}/lib/* /usr/local/lib && \
  myodbc-installer -a -d -n "MySQL ODBC 8.0.33 Driver" -t "Driver=/usr/local/lib/libmyodbc8w.so" && \
  myodbc-installer -a -d -n "MySQL ODBC 8.0.33" -t "Driver=/usr/local/lib/libmyodbc8a.so" && \
  # install the rest of them
  apt-get update && \
  ACCEPT_EULA=Y apt-get install -y msodbcsql17 msodbcsql18 odbc-postgresql && \
  # Update odbcinst.ini to make sure full path to driver is listed, and set CommLog to 0. i.e disables any communication logs to be written to files
  sed 's/Driver=psql/Driver=\/usr\/lib\/x86_64-linux-gnu\/odbc\/psql/;s/CommLog=1/CommLog=0/' /etc/odbcinst.ini > /tmp/temp.ini && \
  mv -f /tmp/temp.ini /etc/odbcinst.ini && \
  # Cleanup build dependencies
  rm -rf ${MYSQL_CONNECTOR}*


ENV VIRTUAL_ENV=/usr/local
ADD --chmod=755 https://astral.sh/uv/install.sh /install.sh
RUN /install.sh && rm /install.sh

RUN /root/.cargo/bin/uv pip install --system --no-cache -r requirements.txt

COPY . /app

RUN pip3 install -e .

ENTRYPOINT ["ingestr"]