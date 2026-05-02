package tools

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/tcolgate/mp3"
)

// вычисляет длительность MP3-файла в секундах
// Возвращает длительность и возможную ошибку
func GetDurationFromMP3(r io.Reader) (int, error) {
	decoder := mp3.NewDecoder(r)
	var total float64
	var frame mp3.Frame
	var skipped int

	for {
		if err := decoder.Decode(&frame, &skipped); err != nil {
			if err == io.EOF {
				break // файл закончился
			}
			return 0, err
		}
		total += frame.Duration().Seconds()
	}
	return int(math.Ceil(total)), nil
}

// пиздец как будто на ассемблере пишу, что за говно
// вычисляет длительность WAV-файла в секундах
// Возвращает длительность и возможную ошибку
func GetDurationFromWAV(r io.Reader) (int, error) {
	// 	Чтобы вычислить общую длительность в секундах, нужны всего четыре параметра из заголовка fmt:
	// SampleRate (uint32) — частота дискретизации (количество отсчётов в секунду).
	// NumChannels (uint16) — количество аудиоканалов (1 для моно, 2 для стерео).
	// BitsPerSample (uint16) — битность одного отсчёта (например, 16 бит).
	// DataSize (uint32) — размер блока с данными в байтах.

	// 1. Пропускаем RIFF-заголовок (12 байт):
	//    ChunkID (4 байта: "RIFF") -> пропускаем
	//    ChunkSize (4 байта) -> пропускаем
	//    Format (4 байта: "WAVE") -> пропускаем
	_, err := io.CopyN(io.Discard, r, 12)
	if err != nil {
		return 0, fmt.Errorf("failed to skip RIFF header: %w", err)
	}

	// 2. Ищем подблок "fmt ".
	for {
		// Читаем 4 байта — идентификатор подблока.
		var chunkID [4]byte
		if _, err := io.ReadFull(r, chunkID[:]); err != nil {
			return 0, fmt.Errorf("failed to read chunk ID: %w", err)
		}

		// Читаем 4 байта — размер подблока.
		var chunkSize uint32
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			return 0, fmt.Errorf("failed to read chunk size: %w", err)
		}

		// Если нашли "fmt ", останавливаемся.
		if string(chunkID[:]) == "fmt " {
			break
		}
		// Пропускаем содержимое чужих блоков.
		if _, err := io.CopyN(io.Discard, r, int64(chunkSize)); err != nil {
			return 0, fmt.Errorf("failed to skip unknown chunk: %w", err)
		}
	}

	// 3. Парсим заголовок "fmt " (минимум 16 байт).
	var audioFormat, numChannels, bitsPerSample uint16
	var sampleRate, byteRate uint32
	var blockAlign uint16

	if err := binary.Read(r, binary.LittleEndian, &audioFormat); err != nil {
		return 0, fmt.Errorf("failed to read audio format: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &numChannels); err != nil {
		return 0, fmt.Errorf("failed to read number of channels: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &sampleRate); err != nil {
		return 0, fmt.Errorf("failed to read sample rate: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &byteRate); err != nil {
		return 0, fmt.Errorf("failed to read byte rate: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &blockAlign); err != nil {
		return 0, fmt.Errorf("failed to read block align: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &bitsPerSample); err != nil {
		return 0, fmt.Errorf("failed to read bits per sample: %w", err)
	}

	_ = audioFormat // Можно использовать для валидации (1 = PCM).
	_ = byteRate    // Может понадобиться для других целей.

	// 4. Ищем подблок "data".
	for {
		// Читаем 4 байта — идентификатор подблока.
		var chunkID [4]byte
		if _, err := io.ReadFull(r, chunkID[:]); err != nil {
			return 0, fmt.Errorf("failed to read chunk ID: %w", err)
		}

		if string(chunkID[:]) == "data" {
			var dataSize uint32
			if err := binary.Read(r, binary.LittleEndian, &dataSize); err != nil {
				return 0, fmt.Errorf("failed to read data chunk size: %w", err)
			}

			// 5. Вычисляем длительность.
			bytesPerSample := int64(bitsPerSample) / 8
			totalSamples := int64(dataSize) / (int64(numChannels) * bytesPerSample)
			duration := float64(totalSamples) / float64(sampleRate)
			return int(math.Ceil(duration)), nil
		}

		// Пропускаем содержимое чужих блоков.
		var chunkSize uint32
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			return 0, fmt.Errorf("failed to read chunk size: %w", err)
		}
		if _, err := io.CopyN(io.Discard, r, int64(chunkSize)); err != nil {
			return 0, fmt.Errorf("failed to skip chunk: %w", err)
		}
	}
}




// readLittleEndianInt64 читает 8-байтовое целое число в формате Little Endian.
// используется для чтения granule position из заголовка OggS.
func readLittleEndianInt64(data []byte) (int64, error) {
	if len(data) < 8 {
		return 0, fmt.Errorf("недостаточно данных для чтения 8-байтового целого")
	}
	var value int64
	err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &value)
	return value, err
}

// readLittleEndianInt32 читает 4-байтовое целое число в формате Little Endian.
// используется для чтения sample rate из заголовка Vorbis.
func readLittleEndianInt32(data []byte) (int32, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("недостаточно данных для чтения 4-байтового целого")
	}
	var value int32
	err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &value)
	return value, err
}

// GetDurationFromOGG рассчитывает длительность OGG Vorbis/Opus файла из io.Reader.
func GetDurationFromOGG(r io.Reader) (int, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("ошибка чтения OGG потока: %w", err)
	}

	// --- Извлечение частоты дискретизации (Sample Rate) ---
	var sampleRate int32
	// Для Opus частота всегда 48000 Гц. Мы попытаемся найти заголовок Vorbis,
	// а если не получится — будем считать, что это Opus.
	foundVorbis := false
	oggMagic := []byte("OggS")
	vorbisMagic := []byte("vorbis")

	for i := 0; i < len(data)-len(vorbisMagic); i++ {
		if bytes.HasPrefix(data[i:], vorbisMagic) {
			// Найден магический маркер "vorbis" внутри идентификационного пакета
			if i+16 > len(data) {
				break
			}
			rate, err := readLittleEndianInt32(data[i+11 : i+15])
			if err == nil && rate > 0 {
				sampleRate = rate
				foundVorbis = true
			}
			break
		}
	}

	if !foundVorbis {
		// Если маркер Vorbis не найден, предполагаем Opus
		sampleRate = 48000
	}

	// --- Извлечение общего количества сэмплов (Granule Position) ---
	var granulePosition int64
	// Ищем последний заголовок "OggS" в данных
	for i := len(data) - len(oggMagic) - 14; i >= 0; i-- {
		if bytes.HasPrefix(data[i:], oggMagic) {
			if i+14 > len(data) {
				continue
			}
			pos, err := readLittleEndianInt64(data[i+6 : i+14])
			if err == nil && pos > 0 {
				granulePosition = pos
			}
			break
		}
	}

	if granulePosition <= 0 || sampleRate <= 0 {
		return 0, fmt.Errorf("не удалось найти необходимую информацию в OGG файле")
	}

	// Расчет длительности
	duration := float64(granulePosition) / float64(sampleRate)
	return int(math.Ceil(duration)), nil
}






func GetDurationFromAAC(reader io.Reader) (int, error) {
    data, err := io.ReadAll(reader)
    if err != nil {
        return 0, err
    }
    if len(data) < 7 {
        return 0, fmt.Errorf("некорректный AAC файл: недостаточно данных")
    }

    var sampleRate int
    var totalSamples int64
    frameCount := 0

    pos := 0
    for pos+7 <= len(data) {
        syncWord := (uint16(data[pos]) << 4) | (uint16(data[pos+1]) >> 4)
        if syncWord != 0xFFF {
            break
        }


        // Извлекаем частоту дискретизации
        samplingFreqIndex := (data[pos+2] >> 2) & 0xF
        if frameCount == 0 {
            switch samplingFreqIndex {
            case 0:
                sampleRate = 96000
            case 1:
                sampleRate = 88200
            case 2:
                sampleRate = 64000
            case 3:
                sampleRate = 48000
            case 4:
                sampleRate = 44100
            case 5:
                sampleRate = 32000
            case 6:
                sampleRate = 24000
            case 7:
                sampleRate = 22050
            case 8:
                sampleRate = 16000
            case 9:
                sampleRate = 12000
            case 10:
                sampleRate = 11025
            case 11:
                sampleRate = 8000
            case 12:
                sampleRate = 7350
            default:
                sampleRate = 44100 // Значение по умолчанию
            }
        }

        // Извлекаем длину фрейма
        frameLength := uint16(data[pos+3]&0x3)<<11 | uint16(data[pos+4])<<3 | uint16(data[pos+5])>>5

        if frameLength < 7 {
            break
        }

        totalSamples += 1024 // Каждый AAC фрейм содержит 1024 сэмпла
        frameCount++

        pos += int(frameLength)
        if pos >= len(data) {
            break
        }
    }

    if frameCount == 0 || sampleRate == 0 {
        return 0, fmt.Errorf("не удалось определить длительность: не найдено ни одного фрейма")
    }

    duration := float64(totalSamples) / float64(sampleRate)
    return int(math.Ceil(duration)), nil
}




func GetDurationFromFLAC(reader io.Reader) (int, error) {
    // Читаем заголовок "fLaC"
    header := make([]byte, 4)
    if _, err := io.ReadFull(reader, header); err != nil {
        return 0, fmt.Errorf("ошибка чтения FLAC заголовка: %w", err)
    }
    if string(header) != "fLaC" {
        return 0, fmt.Errorf("некорректный FLAC файл: заголовок не fLaC")
    }

    // Читаем первый метаданных-блок (всегда StreamInfo)
    metaHeader := make([]byte, 4)
    if _, err := io.ReadFull(reader, metaHeader); err != nil {
        return 0, fmt.Errorf("ошибка чтения заголовка метаданных: %w", err)
    }

    blockType := metaHeader[0] & 0x7F
    if blockType != 0 {
        return 0, fmt.Errorf("первый блок не StreamInfo")
    }

    blockLength := int(metaHeader[1])<<16 | int(metaHeader[2])<<8 | int(metaHeader[3])
    if blockLength < 34 { // StreamInfo всегда 34 байта
        return 0, fmt.Errorf("блок StreamInfo поврежден")
    }

    // Читаем 18 байт StreamInfo — этого хватит для sample rate и total samples
    streamInfo := make([]byte, 18)
    if _, err := io.ReadFull(reader, streamInfo); err != nil {
        return 0, fmt.Errorf("ошибка чтения StreamInfo: %w", err)
    }

    // Частота дискретизации (20 бит, big‑endian)
    // Старшие 8 бит в streamInfo[10], средние 8 бит в streamInfo[11],
    // младшие 4 бита в streamInfo[12] (старшие биты байта)
    sampleRate := int(streamInfo[10])<<12 | int(streamInfo[11])<<4 | int(streamInfo[12])>>4

    // Общее количество сэмплов (36 бит)
    // Старшие 4 бита — в младших битах streamInfo[13],
    // остальные 32 бита — в streamInfo[14..17] (big‑endian)
    totalSamples := int64(streamInfo[13]&0x0F) << 32
    totalSamples |= int64(streamInfo[14]) << 24
    totalSamples |= int64(streamInfo[15]) << 16
    totalSamples |= int64(streamInfo[16]) << 8
    totalSamples |= int64(streamInfo[17])

    if sampleRate == 0 {
        return 0, fmt.Errorf("некорректная частота дискретизации")
    }

    duration := float64(totalSamples) / float64(sampleRate)
    return int(math.Ceil(duration)), nil
}