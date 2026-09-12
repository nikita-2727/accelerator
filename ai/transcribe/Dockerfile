FROM python:3.12-slim


WORKDIR /app

RUN python3.12 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"


COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .