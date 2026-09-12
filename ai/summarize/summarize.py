import os
import json
import uuid
import requests
import asyncio     # Асинхронность
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from contextlib import asynccontextmanager
from time import sleep


# ---------- Вспомогательные функции ----------
def prepare_transcript(json_filepath):
    """Склеивает реплики одного спикера из сырого JSON стенограммы."""
    with open(json_filepath, 'r', encoding='utf-8') as f:
        data = json.load(f)

    transcript_lines = []
    current_speaker = None
    current_text = []

    for entry in data:
        speaker = entry.get("speaker", "UNKNOWN")
        text = entry.get("text", "").strip()

        if not text or "[ОШИБКА ПОДКЛЮЧЕНИЯ" in text:
            continue

        if speaker == current_speaker:
            current_text.append(text)
        else:
            if current_speaker is not None:
                transcript_lines.append(f"{current_speaker}: {' '.join(current_text)}")
            current_speaker = speaker
            current_text = [text]

    if current_speaker is not None:
        transcript_lines.append(f"{current_speaker}: {' '.join(current_text)}")

    return "\n".join(transcript_lines)


# ---------- Шаблон мок-отчёта ----------
def generate_mock_report(transcript_text: str, prompt: str) -> str:
    """
    Генерирует псевдо-отчёт в стиле реальной LLM.
    Возвращает текст в формате Executive Summary / Decisions / Action Items.
    """
    # Считаем количество реплик и уникальных спикеров
    lines = [l for l in transcript_text.split("\n") if l.strip()]
    speakers = set()
    for line in lines:
        if ":" in line:
            speakers.add(line.split(":", 1)[0])

    num_replies = len(lines)
    num_speakers = len(speakers) if speakers else 1

    report = f"""1. Executive Summary:
        В ходе совещания с участием {num_speakers} спикер(ов) было зафиксировано {num_replies} реплик. 
        Основное обсуждение касалось текущих рабочих задач, сроков их выполнения и распределения ответственности между участниками. 
        Участники согласовали ключевые направления работы и обсудили вопросы, требующие дополнительной проработки. 
        Отмечена необходимость уточнения ряда деталей у внешних заказчиков и подготовки промежуточных отчётов.

        2. Decisions Made:
        - Согласовано продолжение работы по текущему проекту в установленные сроки.
        - Принято решение зафиксировать обсуждаемые договорённости в протоколе совещания.
        - Одобрено выделение дополнительных ресурсов на приоритетные задачи.
        - Утверждён план подготовки отчётности к концу недели.

        3. Action Items:
        - Подготовить итоговый отчёт по текущей задаче — ответственный: SPEAKER_00, срок: до конца недели.
        - Уточнить сроки и требования у заказчика — ответственный: SPEAKER_01, срок: в течение 2 рабочих дней.
        - Подготовить предложения по бюджету — ответственный: SPEAKER_02, срок: до следующего совещания.
        - Сформировать протокол встречи и разослать участникам — ответственный: SPEAKER_00, срок: сегодня.

        Примечание: Данный отчёт сгенерирован автоматически в режиме мок-тестирования.
        """
    return report.strip()




# ---------- Жизненный цикл ----------
@asynccontextmanager
async def lifespan(app: FastAPI):
    # Модель больше не грузим при старте
    print("[INFO] Мок-сервер саммаризации запущен. Ожидание запросов...")
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
    local_input = f"/tmp/{task_id}_transcript.json"
    local_output = f"/tmp/{task_id}_summary.json"

    # ПРОСТО РАНДОМНЫЙ ОТЧЕТ ЗАКИДЫВАЕМ ШАБЛОННЫЙ
    
    # 1. Скачиваем стенограмму
    print(f"[INFO] Скачивание стенограммы {input_url}")
    r = requests.get(input_url, stream=True)
    r.raise_for_status()
    with open(local_input, 'wb') as f:
        for chunk in r.iter_content(chunk_size=8192):
            f.write(chunk)

    # 2. Подготовка текста
    transcript_text = prepare_transcript(local_input)
    print("[INFO] Текст подготовлен.")

    # 3. Генерация мок-отчёта
    print(f"[INFO] Генерация мок-отчёта (промпт: {prompt[:80]}...)")
    report_text = generate_mock_report(transcript_text, prompt)

    # 4. Сохранение результата в JSON
    result_data = {
        "status": "success",
        "analysis_report": report_text
    }
    with open(local_output, 'w', encoding='utf-8') as f:
        json.dump(result_data, f, ensure_ascii=False, indent=2)

    # ЖДЕМ 
    sleep(10)

    # 5. Загрузка результата
    print(f"[INFO] Отправка саммари...")
    with open(local_output, 'rb') as fout:
        resp = requests.put(output_url, data=fout)
        resp.raise_for_status()
    print("[SUCCESS] Суммаризация завершена.")


    # Удаление временных файлов
    for f in [local_input, local_output]:
        if os.path.exists(f):
            os.remove(f)

@app.post("/summarize")
async def summarize(request: SummarizeRequest):
    task_id = str(uuid.uuid4())[:8]
    try:
        # Запускаем в отдельном потоке, дожидаясь полного завершения
        await asyncio.to_thread(run_summarization, request.input_url, request.output_url, request.prompt, task_id)
        return {"status": "ok"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))