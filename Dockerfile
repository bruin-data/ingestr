FROM python:3.11-slim

WORKDIR /app

COPY ./requirements.txt /app/requirements.txt

RUN apt-get update && apt-get -y install libpq-dev gcc curl g++

ENV VIRTUAL_ENV=/usr/local
ADD --chmod=755 https://astral.sh/uv/install.sh /install.sh
RUN /install.sh && rm /install.sh

RUN /root/.cargo/bin/uv pip install --no-cache -r requirements.txt

COPY . /app

RUN pip3 install -e .

ENTRYPOINT ["ingestr"]