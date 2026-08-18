FROM python:3.13-slim-bookworm

RUN apt-get update \
    && apt-get install --no-install-recommends -y lua5.4 nodejs \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /opt/x3/requirements.txt
RUN pip install --no-cache-dir --requirement /opt/x3/requirements.txt

COPY runner.py /opt/x3/runner.py

RUN chmod 0555 /opt/x3/runner.py

# runner.py is a small trusted supervisor. It starts as root inside the gVisor
# sandbox, then irreversibly drops only the submitted program to 65534:65534.
USER 0:0
WORKDIR /work
