import os
import sys
import json
import uuid
import warnings
import requests
import torch
import gc          # Сборщик мусора
import asyncio     # Асинхронность
from pydub import AudioSegment
from contextlib import asynccontextmanager
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from time import sleep

warnings.filterwarnings("ignore")

# # ---------- Переменные окружения для кеша моделей ----------
# # Берем пути напрямую из твоего Dockerfile
# os.environ.setdefault("HF_HOME", "/app/models/huggingface")
# os.environ.setdefault("MODELSCOPE_CACHE", "/app/models/modelscope")

# # ---------- Импорт модели ----------
# from qwen_asr import Qwen3ASRModel

# MODEL_ID = "Qwen/Qwen3-ASR-1.7B"

# ---------- Жизненный цикл ----------
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Убираем загрузку модели при старте
    print("[INFO] Сервер транскрипции запущен. VRAM свободна. Ожидание запросов...")
    yield

app = FastAPI(lifespan=lifespan)

# ---------- Тело запроса ----------
class TranscribeRequest(BaseModel):
    input_url: str         # Presigned GET на JSON диаризации
    denoised_url: str      # Presigned GET на аудио (denoised.wav)
    output_url: str        # Presigned PUT для результата (JSON с текстом)

@app.get("/ready")
async def check_ready():
    return {"status": "ready"}

# ---------- Обработчик (работает с VRAM) ----------
def run_transcription(input_url: str, denoised_url: str, output_url: str, task_id: str):
    print(input_url)
    sleep(10)
    print(denoised_url)
    print(output_url)

    # # Временные файлы
    # audio_path = f"/tmp/{task_id}_audio.wav"
    # diar_json_path = f"/tmp/{task_id}_diar.json"
    # result_path = f"/tmp/{task_id}_result.json"
    # temp_chunk = f"/tmp/{task_id}_chunk.wav"
    
    # asr_model = None

    # try:
    #     # 1. Скачиваем аудио
    #     print(f"[INFO] Скачивание аудио {denoised_url}")
    #     r = requests.get(denoised_url, stream=True)
    #     r.raise_for_status()
    #     with open(audio_path, 'wb') as f:
    #         for chunk in r.iter_content(chunk_size=8192):
    #             f.write(chunk)

    #     # 2. Скачиваем JSON диаризации
    #     print(f"[INFO] Скачивание диаризации {input_url}")
    #     r = requests.get(input_url)
    #     r.raise_for_status()
    #     with open(diar_json_path, 'wb') as f:
    #         f.write(r.content)

    #     # 3. Читаем сегменты
    #     with open(diar_json_path, 'r', encoding='utf-8') as f:
    #         segments = json.load(f)

    #     # 4. ЗАГРУЗКА МОДЕЛИ В VRAM
    #     print(f"[INFO] Загрузка модели ASR {MODEL_ID} в VRAM...")
    #     kwargs = {"device_map": "cuda"} if torch.cuda.is_available() else {"device_map": "cpu"}
    #     asr_model = Qwen3ASRModel.from_pretrained(MODEL_ID, **kwargs)

    #     # Настройка greedy decoding
    #     if hasattr(asr_model, "model") and hasattr(asr_model.model, "generation_config"):
    #         asr_model.model.generation_config.temperature = 0.2
    #         asr_model.model.generation_config.do_sample = False
    #         asr_model.model.generation_config.repetition_penalty = 1.2
    #         print("[INFO] Greedy search + penalty настроены")

    #     # 5. Обработка аудио
    #     full_audio = AudioSegment.from_file(audio_path)

    #     if torch.cuda.is_available():
    #         torch.cuda.reset_peak_memory_stats()

    #     print(f"[INFO] Транскрибация {len(segments)} сегментов...")

    #     for i, segment in enumerate(segments):
    #         start_ms = int(segment["start"] * 1000)
    #         end_ms = int(segment["end"] * 1000)

    #         if end_ms <= start_ms:
    #             segment["text"] = ""
    #             continue

    #         chunk = full_audio[start_ms:end_ms]
    #         duration_ms = len(chunk)

    #         # Увеличиваем короткие чанки до 500 мс
    #         if duration_ms < 500:
    #             chunk = chunk + AudioSegment.silent(
    #                 duration=500 - duration_ms,
    #                 frame_rate=chunk.frame_rate
    #             )
    #         full_text = ""
    #         MAX_CHUNK_MS = 30000  # 30 секунд
    #         for offset_ms in range(0, max(len(chunk), 1), MAX_CHUNK_MS):
    #             sub_chunk = chunk[offset_ms : offset_ms + MAX_CHUNK_MS]
    #             sub_chunk.export(temp_chunk, format="wav")

    #             try:
    #                 results = asr_model.transcribe(audio=temp_chunk)
    #                 full_text += results[0].text + " "
    #             except Exception as e:
    #                 print(f"\n[ERROR] Ошибка на сегменте {i}: {e}")

    #         segment["text"] = full_text.strip()

    #         vram_mb = torch.cuda.max_memory_allocated() / (1024 * 1024) if torch.cuda.is_available() else 0
    #         sys.stdout.write(f"\rОбработано: {i+1}/{len(segments)} | Макс VRAM: {vram_mb:.0f} MB   ")
    #         sys.stdout.flush()

    #     # 6. Сохранение и загрузка результата
    #     with open(result_path, 'w', encoding='utf-8') as f:
    #         json.dump(segments, f, ensure_ascii=False, indent=2)

    #     print(f"\n[INFO] Загрузка результата в {output_url}")
    #     with open(result_path, 'rb') as fout:
    #         resp = requests.put(output_url, data=fout)
    #         resp.raise_for_status()

    #     print("[SUCCESS] Транскрипция завершена успешно")

    # finally:
    #     # ---------- ВЫГРУЗКА ИЗ VRAM И ОЧИСТКА ----------
    #     print("\n[INFO] Выгрузка модели ASR из VRAM...")
    #     if asr_model is not None:
    #         del asr_model
    #         gc.collect()
    #         if torch.cuda.is_available():
    #             torch.cuda.empty_cache()
    #         print("[INFO] VRAM успешно освобождена.")

    #     # Удаляем временные файлы
    #     for f in [audio_path, diar_json_path, result_path, temp_chunk]:
    #         if os.path.exists(f):
    #             os.remove(f)

@app.post("/transcribe")
async def transcribe(req: TranscribeRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном потоке, дожидаясь полного завершения
        await asyncio.to_thread(run_transcription, req.input_url, req.denoised_url, req.output_url, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))