FROM nvidia/cuda:12.8.0-cudnn-devel-ubuntu24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV HF_HOME=/app/models/huggingface
ENV TORCH_HOME=/app/models/torch

RUN apt-get update && \
    apt-get install -y python3.12 python3-pip python3.12-dev python3.12-venv ffmpeg libsndfile1 curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Виртуальное окружение
RUN python3.12 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# PyTorch с CUDA 12.8
RUN pip install --pre torch torchaudio --index-url https://download.pytorch.org/whl/nightly/cu128

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .
