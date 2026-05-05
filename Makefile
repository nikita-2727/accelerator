create:
	migrate create -ext sql -dir migrations -seq init
up:
	migrate -path migrations -database "postgres://postgres:1423qewr@localhost:5432/accelerator?sslmode=disable" up 1
down:
	migrate -path migrations -database "postgres://postgres:1423qewr@localhost:5432/accelerator?sslmode=disable" down 1
force:
	migrate -path migrations -database "postgres://postgres:1423qewr@localhost:5432/accelerator?sslmode=disable" force 3
info: 
	migrate -path migrations -database "postgres://postgres:1423qewr@localhost:5432/accelerator?sslmode=disable" version