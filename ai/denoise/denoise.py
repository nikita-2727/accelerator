import os
import uuid
import requests
import shutil
import gc
import torch
import asyncio
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from contextlib import asynccontextmanager
from audio_separator.separator import Separator

# Возвращаем пути как в твоем compose
models_dir = "/app/models"
ram_disk = "/dev/shm"

@asynccontextmanager
async def lifespan(app: FastAPI):
    os.makedirs(models_dir, exist_ok=True)
    os.makedirs(ram_disk, exist_ok=True)
    print("[INFO] Сервер запущен. VRAM свободна. Ожидание запросов от Go-бэкенда...")
    yield

app = FastAPI(lifespan=lifespan)

class ProcessRequest(BaseModel):
    input_url: str
    output_url: str

@app.get("/ready")
async def check_ready():
    return {"status": "ready"}

def process_denoise(input_url: str, output_url: str, task_id: str):
    local_input = f"/tmp/{task_id}_input.wav"
    local_output_tmp = None
    local_output_final = f"/tmp/{task_id}_clean.wav"
    
    separator = None

    try:
        print(f"[INFO] Скачивание аудио {input_url}")
        r = requests.get(input_url, stream=True)
        r.raise_for_status()
        with open(local_input, 'wb') as f:
            for chunk in r.iter_content(chunk_size=8192):
                f.write(chunk)

        print("[INFO] Инициализация движка и загрузка модели в VRAM...")
        separator = Separator(
            output_dir=ram_disk,
            output_format="WAV",
            output_single_stem="vocals", 
            use_autocast=True,
            model_file_dir=models_dir,
            mdxc_params={
                "segment_size": 512,
                "overlap": 2,
                "batch_size": 4
            }
        )
        separator.load_model(model_filename="model_bs_roformer_ep_317_sdr_12.9755.ckpt")
        print("[INFO] Модель в памяти. Идет обработка файла...")
        
        generated_files = separator.separate(local_input)

        for file in generated_files:
            if "Vocals" in file or "vocals" in file:
                local_output_tmp = os.path.join(separator.output_dir, file)
                break

        if not local_output_tmp or not os.path.exists(local_output_tmp):
            raise RuntimeError("Модель не вернула файл с вокалом.")

        shutil.move(local_output_tmp, local_output_final)

        print("[INFO] Отправка готового файла в хранилище...")
        with open(local_output_final, 'rb') as fout:
            resp = requests.put(output_url, data=fout)
            resp.raise_for_status()
        print("[SUCCESS] Денойзинг завершён.")

    finally:
        print("[INFO] Выгрузка модели из VRAM...")
        if separator is not None:
            del separator
            gc.collect()
            if torch.cuda.is_available():
                torch.cuda.empty_cache()
            print("[INFO] VRAM успешно освобождена.")

        for f in [local_input, local_output_final]:
            if f and os.path.exists(f):
                os.remove(f)
        if local_output_tmp and os.path.exists(local_output_tmp):
            os.remove(local_output_tmp)

@app.post("/denoise")
async def denoise(request: ProcessRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном процессе, дожидаясь полного завершения
        await asyncio.to_thread(process_denoise, request.input_url, request.output_url, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))