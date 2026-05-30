FROM nvidia/cuda:12.8.0-cudnn-devel-ubuntu24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1

RUN apt-get update && apt-get install -y \
    python3.12 \
    python3.12-venv \
    python3.12-dev \
    python3-pip \
    ffmpeg \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Меняем рабочую директорию на ту, что в твоем композе
WORKDIR /app

# создаем новое виртуальное окружение
RUN python3.12 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Ставим ночную сборку PyTorch под свежую архитектуру GPU
# RUN pip install --pre torch torchvision torchaudio --index-url https://download.pytorch.org/whl/nightly/cu128

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Скачиваем веса модели прямо в папку /app/models на этапе сборки
RUN mkdir -p /app/models && \
    python3.12 -c "from audio_separator.separator import Separator; Separator(model_file_dir='/app/models').download_model_files('model_bs_roformer_ep_317_sdr_12.9755.ckpt')"

COPY . .