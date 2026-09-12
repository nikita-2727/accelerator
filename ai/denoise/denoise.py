import uuid
import requests
import asyncio
from fastapi import FastAPI, HTTPException
from contextlib import asynccontextmanager
from pydantic import BaseModel

import os
from time import sleep

# ---------- Жизненный цикл ----------
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Убираем загрузку модели при старте
    print("[INFO] Мок-сервер денойзинга запущен. Ожидание запросов...")
    yield

app = FastAPI(lifespan=lifespan)



class ProcessRequest(BaseModel):
    input_url: str
    output_url: str

@app.get("/ready")
async def check_ready():
    return {"status": "ready"}

def process_denoise(input_url: str, output_url: str, task_id: str):
    # ПОКА СОХРАНЯЕМ ТОТ ЖЕ ФАЙЛ, ЧТО И ПЕРЕДАЛИ

    local_input = f"/tmp/{task_id}_input.wav"

    print(f"[INFO] Скачивание аудио {input_url}")
    r = requests.get(input_url, stream=True)
    r.raise_for_status()
    with open(local_input, 'wb') as f:
        for chunk in r.iter_content(chunk_size=8192):
            f.write(chunk)

    # ЖДЕМ
    sleep(10)

    print("[INFO] Отправка готового файла в хранилище...")
    with open(local_input, 'rb') as fout:
        resp = requests.put(output_url, data=fout)
        resp.raise_for_status()
    print("[SUCCESS] Денойзинг завершён.")

    # Удаляем временные файлы
    if os.path.exists(local_input):
        os.remove(local_input)


@app.post("/denoise")
async def denoise(request: ProcessRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном процессе, дожидаясь полного завершения
        await asyncio.to_thread(process_denoise, request.input_url, request.output_url, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))