import os
import json
import uuid
import requests
import gc          # Сборщик мусора
import asyncio     # Асинхронность
import glob        # Для поиска .gguf файлов
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from contextlib import asynccontextmanager
# from llama_cpp import Llama

from time import sleep

# # ---------- Вспомогательные функции ----------
# def prepare_transcript(json_filepath):
#     """Склеивает реплики одного спикера из сырого JSON стенограммы."""
#     with open(json_filepath, 'r', encoding='utf-8') as f:
#         data = json.load(f)

#     transcript_lines = []
#     current_speaker = None
#     current_text = []

#     for entry in data:
#         speaker = entry.get("speaker", "UNKNOWN")
#         text = entry.get("text", "").strip()

#         if not text or "[ОШИБКА ПОДКЛЮЧЕНИЯ" in text:
#             continue

#         if speaker == current_speaker:
#             current_text.append(text)
#         else:
#             if current_speaker is not None:
#                 transcript_lines.append(f"{current_speaker}: {' '.join(current_text)}")
#             current_speaker = speaker
#             current_text = [text]

#     if current_speaker is not None:
#         transcript_lines.append(f"{current_speaker}: {' '.join(current_text)}")

#     return "\n".join(transcript_lines)

# ---------- Жизненный цикл ----------
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Модель больше не грузим при старте
    print("[INFO] Сервер саммаризации запущен. VRAM свободна. Ожидание запросов...")
    yield

app = FastAPI(lifespan=lifespan)

# ---------- Модель запроса ----------
class SummarizeRequest(BaseModel):
    input_url: str   # Presigned GET на JSON стенограммы
    prompt: str      # Системный промпт (инструкция для LLM)
    output_url: str  # Presigned PUT для итогового JSON-отчёта

@app.get("/ready")
async def check_ready():
    return {"status": "ready"}

# ---------- Логика обработки (с работой в VRAM) ----------
def run_summarization(input_url: str, output_url: str, prompt: str, task_id: str):
    print(input_url)
    sleep(10)
    print(output_url)
    print(prompt)

    # local_input = f"/tmp/{task_id}_transcript.json"
    # local_output = f"/tmp/{task_id}_summary.json"
    
    # llm = None

    # try:
    #     # 1. Скачиваем стенограмму
    #     print(f"[INFO] Скачивание стенограммы {input_url}")
    #     r = requests.get(input_url, stream=True)
    #     r.raise_for_status()
    #     with open(local_input, 'wb') as f:
    #         for chunk in r.iter_content(chunk_size=8192):
    #             f.write(chunk)

    #     # 2. Подготовка текста
    #     transcript_text = prepare_transcript(local_input)
    #     print("[INFO] Текст подготовлен.")

    #     # 3. Динамический поиск модели .gguf (заменяет логику start.sh)
    #     model_path = os.environ.get("LOCAL_MODEL_PATH", "/app/models")
        
    #     # Если путь это папка или не указан точный .gguf файл — ищем сами
    #     if os.path.isdir(model_path) or not model_path.endswith(".gguf"):
    #         gguf_files = glob.glob("/app/models/*.gguf")
    #         if not gguf_files:
    #             raise RuntimeError(f"ОШИБКА: Ни один файл .gguf не найден в папке /app/models!")
    #         model_path = gguf_files[0]
    #         print(f"[INFO] Автоматически найдена модель: {model_path}")

    #     # 4. Загрузка LLM в VRAM
    #     print(f"[INFO] Загрузка LLM из {model_path} в VRAM...")
    #     llm = Llama(
    #         model_path=model_path,
    #         n_ctx=16384,
    #         n_gpu_layers=-1,      # Все слои отправляем на GPU
    #         flash_attn=True,
    #         chat_format="chatml",
    #         verbose=False
    #     )
    #     print("[INFO] Модель в памяти. Генерация отчета...")

    #     # 5. Инференс
    #     output = llm.create_chat_completion(
    #         messages=[
    #             {"role": "system", "content": prompt},
    #             {"role": "user", "content": f"Транскрипция:\n{transcript_text}"}
    #         ],
    #         max_tokens=4096,
    #         temperature=0.2
    #     )
    #     report_text = output['choices'][0]['message']['content']
    #     # 6. Сохранение результата в JSON
    #     result_data = {
    #         "status": "success",
    #         "analysis_report": report_text
    #     }
    #     with open(local_output, 'w', encoding='utf-8') as f:
    #         json.dump(result_data, f, ensure_ascii=False, indent=2)

    #     # 7. Загрузка результата
    #     print(f"[INFO] Отправка саммари...")
    #     with open(local_output, 'rb') as fout:
    #         resp = requests.put(output_url, data=fout)
    #         resp.raise_for_status()
    #     print("[SUCCESS] Суммаризация завершена.")

    # finally:
    #     # ---------- ВЫГРУЗКА ИЗ VRAM И ОЧИСТКА ----------
    #     print("[INFO] Выгрузка LLM из VRAM...")
    #     if llm is not None:
    #         del llm          # Удаляем ссылку на объект
    #         gc.collect()     # Сборщик мусора триггерит освобождение контекста llama.cpp в VRAM
    #         print("[INFO] VRAM успешно освобождена.")

    #     # Удаление временных файлов
    #     for f in [local_input, local_output]:
    #         if os.path.exists(f):
    #             os.remove(f)

@app.post("/summarize")
async def summarize(request: SummarizeRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном потоке, дожидаясь полного завершения
        await asyncio.to_thread(run_summarization, request.input_url, request.output_url, request.prompt, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))