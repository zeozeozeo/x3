FROM python:3.13-slim-bookworm

RUN apt-get update \
    && apt-get install --no-install-recommends -y lua5.4 nodejs \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /opt/x3/requirements.txt
RUN pip install --no-cache-dir --requirement /opt/x3/requirements.txt

COPY runner.py /opt/x3/runner.py

RUN chmod 0555 /opt/x3/runner.py

USER 65534:65534
WORKDIR /work
