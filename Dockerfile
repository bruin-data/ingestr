FROM python:3.11-slim-trixie

# Guidelines that have been followed.
# - https://hynek.me/articles/docker-uv/

# Install the `uv` package manager.
# Security-conscious organizations should package/review uv themselves.
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# - Tell uv to byte-compile packages for faster application startups.
# - Silence uv complaining about not being able to use hard links.
# - Prevent uv from accidentally downloading isolated Python builds.
# - Install packages into the system Python environment.
ENV \
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    UV_SYSTEM_PYTHON=1

WORKDIR /app


# Install all prerequisites.

RUN \
  export ACCEPT_EULA='Y' && \
  # Install build dependencies
  apt-get update && \
  apt-get install -y curl gcc gpg libpq-dev build-essential unixodbc-dev g++ apt-transport-https git

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


# Install application.

# Copy sources and activate platform-specific requirements file.
COPY . /app
RUN if [ "$(uname -m)" = "aarch64" ]; then \
    cp /app/requirements_arm64.txt /app/requirements.txt; \
fi

# Generate version file
RUN make write-build-info

# Install all required packages and the application.
RUN uv pip install --requirement requirements.txt pyodbc .


# Ready.
ENTRYPOINT ["ingestr"]
