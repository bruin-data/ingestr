FROM python:3.11-slim

WORKDIR /app

COPY ./requirements.txt /app/requirements.txt
COPY ./requirements_arm64.txt /app/requirements_arm64.txt
RUN if [ "$(uname -m)" = "aarch64" ]; then \
    cp /app/requirements_arm64.txt /app/requirements.txt; \
fi

# Setup dependencies for pyodbc
RUN \
  export ACCEPT_EULA='Y' && \
  # Install build dependencies
  apt-get update && \
  apt-get install -y curl gcc libpq-dev build-essential unixodbc-dev g++ apt-transport-https

RUN \ 
  # Install pyodbc db drivers for MSSQL and PostgreSQL
  curl -sSL https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor > /usr/share/keyrings/microsoft-prod.gpg && \
  curl -sSL https://packages.microsoft.com/config/debian/12/prod.list | tee /etc/apt/sources.list.d/mssql-release.list
  
RUN \
  # install the rest of them
  apt-get update && \
  ACCEPT_EULA=Y apt-get install -y msodbcsql18 odbc-postgresql && \
  # Update odbcinst.ini to make sure full path to driver is listed, and set CommLog to 0. i.e disables any communication logs to be written to files
  sed 's/Driver=psql/Driver=\/usr\/lib\/x86_64-linux-gnu\/odbc\/psql/;s/CommLog=1/CommLog=0/' /etc/odbcinst.ini > /tmp/temp.ini && \
  mv -f /tmp/temp.ini /etc/odbcinst.ini

ENV VIRTUAL_ENV=/usr/local
ADD --chmod=755 https://astral.sh/uv/install.sh /install.sh
RUN /install.sh && rm /install.sh

RUN $HOME/.local/bin/uv pip install --system --no-cache -r requirements.txt

COPY . /app
RUN if [ "$(uname -m)" = "aarch64" ]; then \
    cp /app/requirements_arm64.txt /app/requirements.txt; \
fi

RUN $HOME/.local/bin/uv pip install --system . && $HOME/.local/bin/uv pip install --system pyodbc

ENTRYPOINT ["ingestr"]