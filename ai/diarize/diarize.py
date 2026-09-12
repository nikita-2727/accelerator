import uuid
import json
import requests
import asyncio     # Для асинхронности
from fastapi import FastAPI, HTTPException
from contextlib import asynccontextmanager
from pydantic import BaseModel
import random

import os
from time import sleep

# ---------- Жизненный цикл ----------
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Убираем загрузку модели при старте
    print("[INFO] Мок-сервер диаризации запущен. Ожидание запросов...")
    yield

app = FastAPI(lifespan=lifespan)



class DiarizeRequest(BaseModel):
    input_url: str   # Presigned GET на аудиофайл
    output_url: str  # Presigned PUT для JSON-результата

# ---------- Эндпоинт проверки готовности ----------
@app.get("/ready")
async def check_ready():
    return {"status": "ready"}

# ---------- Логика обработки (с работой в VRAM) ----------
def run_diarization(input_url: str, output_url: str, task_id: str):

    local_json = f"/tmp/{task_id}_diarization.json"
    print(f"[INFO] НАЧАЛО ДИАРИЗАЦИИ...")

    # Генерируем случайные сегменты диаризации
    speakers = ["SPEAKER_00", "SPEAKER_01", "SPEAKER_02"]
    merged_results = []
    current_time = 0.0

    for _ in range(random.randint(5, 15)):
        duration = round(random.uniform(1.5, 8.0), 2)
        speaker = random.choice(speakers)
        merged_results.append({
            "start": round(current_time, 2),
            "end": round(current_time + duration, 2),
            "speaker": speaker
        })
        current_time += duration

    # ЖДЕМ
    sleep(10)

    # Сохраняем результат в JSON и загружаем обратно
    with open(local_json, 'w', encoding='utf-8') as f:
        json.dump(merged_results, f, ensure_ascii=False, indent=2)

    print(f"[INFO] Загрузка результата по presigned PUT...")
    with open(local_json, 'rb') as fout:
        resp = requests.put(output_url, data=fout)
        resp.raise_for_status()
    print("[SUCCESS] Диаризация завершена успешно")


    # Удаляем временные файлы
    if os.path.exists(local_json):
        os.remove(local_json)


@app.post("/diarize")
async def diarize(request: DiarizeRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном процессе, дожидаясь полного завершения
        await asyncio.to_thread(run_diarization, request.input_url, request.output_url, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))