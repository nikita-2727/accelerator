package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// оборачивает официальный minio.Client и добавляет удобные методы
type MinIOClient struct {
	Client       *minio.Client
	PublicClient *minio.Client
	Bucket       string
}

// создаёт новый экземпляр клиента и проверяет/создаёт бакет
func NewMinIOClient(endpoint, accessKey, secretKey, bucket string, useSSL bool, publicEndpoint string) (*MinIOClient, error) {
	// передаем сюда название и порт контейнера, в котором запущен minio в одной сети
	client, err := minio.New(endpoint, &minio.Options{
		// передаем при создании наши секретные ключи из конфига
		// access - имя поьзователя, secret - пароль
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""),
		// используем https или http
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client init: %w", err)
	}

	// проверяем существование бакета с переданным названием
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket exists check: %w", err)
	}
	// если нет, то создаем его
	if !exists {
		// для minio регион не важен, но должен быть задан - us-east-1 - значение по умолчанию
		err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"})
		if err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}

	// Создаём публичного клиента для генерации presigned URL
	// Убираем префикс https://, если есть, т.к. minio.New ожидает хост:порт
	publicHost := strings.TrimPrefix(publicEndpoint, "https://")
	publicHost = strings.TrimPrefix(publicHost, "http://")

	publicClient, err := minio.New(publicHost, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: true, // потому что используем https
		Region: "us-east-1",
	})
	if err != nil {
		return nil, fmt.Errorf("public minio client init: %w", err)
	}

	return &MinIOClient{
		Client:       client,
		PublicClient: publicClient,
		Bucket:       bucket,
	}, nil

}

// загружает файл в указанный objectKey
// Принимает:
// ctx - из хедера
// objectKey - путь до файла в бакете
// reader - тело файла (из multipart.FormFile)
// size - размер
// contentType - MIME-тип
func (m *MinIOClient) UploadFile(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) (*minio.UploadInfo, error) {
	// настройки загрузки, передаем MIME тип файла
	opts := minio.PutObjectOptions{
		ContentType: contentType,
	}
	// кладем объект в бакет по названию бакета по нужному пути, передавая сам ридер, его размер и тип данных
	info, err := m.Client.PutObject(ctx, m.Bucket, objectKey, reader, size, opts)
	if err != nil {
		return nil, fmt.Errorf("upload file: %w", err)
	}
	return &info, nil
}

// генерирует временную ссылку для скачивания/просмотра объекта
// objectKey - путь к файлу (например, "uploads/.../audio.wav")
// expiry - время жизни ссылки (например, 1 час)
func (m *MinIOClient) GetPresignedGetLocalURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	reqParams := make(url.Values)
	presignedURL, err := m.Client.PresignedGetObject(ctx, m.Bucket, objectKey, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("presigned get: %w", err)
	}
	return presignedURL.String(), nil
}

// передаем новый хост из конфига
func (m *MinIOClient) GetPresignedGetPublicURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	reqParams := make(url.Values)
	presignedURL, err := m.PublicClient.PresignedGetObject(ctx, m.Bucket, objectKey, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("presigned get: %w", err)
	}

	return presignedURL.String(), nil
}

// генерирует временную ссылку для загрузки объекта
// Используется Python-воркерами, чтобы сохранить результат обработки
// expiry - время жизни ссылки (например, 1 час)
func (m *MinIOClient) GetPresignedPutURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	presignedURL, err := m.Client.PresignedPutObject(ctx, m.Bucket, objectKey, expiry)
	if err != nil {
		return "", fmt.Errorf("presigned put: %w", err)
	}
	return presignedURL.String(), nil
}

// DeleteFile удаляет объект из хранилища
func (m *MinIOClient) DeleteFile(ctx context.Context, objectKey string) error {
	err := m.Client.RemoveObject(ctx, m.Bucket, objectKey, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}
