FROM nvidia/cuda:12.8.0-cudnn-devel-ubuntu24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1

RUN apt-get update && apt-get install -y \
    python3.12 python3-pip python3.12-dev python3.12-venv \
    cmake build-essential curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Виртуальное окружение
RUN python3.12 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Установка PyTorch (опционально, для унификации)
# RUN pip install --pre torch torchvision torchaudio --index-url https://download.pytorch.org/whl/nightly/cu128

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Сборка llama-cpp-python с поддержкой CUDA
# ENV CMAKE_ARGS="-DGGML_CUDA=on"
# RUN pip install --no-cache-dir llama-cpp-python

COPY . .

CMD ["uvicorn", "summarize:app", "--host", "0.0.0.0", "--port", "${SUMMARY_PORT}"]