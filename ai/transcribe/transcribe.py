import os
import json
import uuid
import requests
import asyncio     # Асинхронность
from contextlib import asynccontextmanager
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
import random
from time import sleep


# ---------- Жизненный цикл ----------
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Убираем загрузку модели при старте
    print("[INFO] Мок-сервер транскрипции запущен. Ожидание запросов...")
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



# ---------- Моковые фразы ----------
MOCK_PHRASES = [
    "Коллеги, давайте начнём совещание.",
    "У нас сегодня три основных вопроса.",
    "По первому пункту есть замечания?",
    "Я предлагаю перенести обсуждение на завтра.",
    "Согласен, это разумное решение.",
    "Нужно уточнить сроки у заказчика.",
    "Давайте зафиксируем это в протоколе.",
    "Какой бюджет мы можем выделить?",
    "Я подготовлю отчёт к концу недели.",
    "Спасибо всем, встреча окончена.",
    "Обсудим детали после обеда.",
    "Есть ли вопросы по текущей задаче?",
]

# ---------- Обработчик (работает с VRAM) ----------
def run_transcription(input_url: str, denoised_url: str, output_url: str, task_id: str):
    # Временные файлы
    diar_json_path = f"/tmp/{task_id}_diar.json"
    result_path = f"/tmp/{task_id}_result.json"
    
    # Скачиваем JSON диаризации
    print(f"[INFO] Скачивание диаризации {input_url}")
    r = requests.get(input_url)
    r.raise_for_status()
    with open(diar_json_path, 'wb') as f:
        f.write(r.content)

    # РАНДОМНО ПОДСТАВЛЯЕМ ФРАЗЫ НА ОСНОВЕ ДИАРИЗАЦИИ
    # Читаем сегменты
    with open(diar_json_path, 'r', encoding='utf-8') as f:
        segments = json.load(f)

    # Каждому сегменту присваиваем случайный текст
    for segment in segments:
        segment["text"] = random.choice(MOCK_PHRASES)

    sleep(10)

    # Сохранение и загрузка результата
    with open(result_path, 'w', encoding='utf-8') as f:
        json.dump(segments, f, ensure_ascii=False, indent=2)

    print(f"\n[INFO] Загрузка результата в {output_url}")
    with open(result_path, 'rb') as fout:
        resp = requests.put(output_url, data=fout)
        resp.raise_for_status()

    print("[SUCCESS] Транскрипция завершена успешно")


    # Удаляем временные файлы
    for f in [diar_json_path, result_path]:
        if os.path.exists(f):
            os.remove(f)

@app.post("/transcribe")
async def transcribe(req: TranscribeRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном потоке, дожидаясь полного завершения
        await asyncio.to_thread(run_transcription, req.input_url, req.denoised_url, req.output_url, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))