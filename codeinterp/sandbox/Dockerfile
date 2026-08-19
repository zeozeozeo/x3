FROM oven/bun:debian AS bun

FROM python:3.13-slim-bookworm

RUN apt-get update \
    && apt-get install --no-install-recommends -y bash lua5.4 perl php-cli ruby \
    && rm -rf /var/lib/apt/lists/*

COPY --from=bun /usr/local/bin/bun /usr/local/bin/bun

# The runtime cache has no value in these one-shot containers and would consume
# the small writable tmpfs.
ENV BUN_RUNTIME_TRANSPILER_CACHE_PATH=0

COPY requirements.txt /opt/x3/requirements.txt
RUN pip install --no-cache-dir --requirement /opt/x3/requirements.txt

COPY runner.py /opt/x3/runner.py

RUN chmod 0555 /opt/x3/runner.py

# runner.py is a small trusted supervisor. It starts as root inside the gVisor
# sandbox, then irreversibly drops only the submitted program to 65534:65534.
USER 0:0
WORKDIR /work
