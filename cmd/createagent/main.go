// Создаёт локального агента и печатает его токен и команду запуска
// (аналог python manage.py create_agent "Офис"). То же самое можно сделать на сайте:
// «Поиск инверторов» → «Добавить агента».
//
//	go run ./cmd/createagent/ -server http://127.0.0.1:8000 "Офис"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"ata-energo/db"
	"ata-energo/services"
	"ata-energo/types"

	"ata-energo/db/repositories"

	"github.com/joho/godotenv"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8000", "Адрес сайта для агента")
	flag.Parse()
	name := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if name == "" {
		log.Fatal(`usage: createagent [-server URL] "Название агента"`)
	}
	_ = godotenv.Load()

	ctx := context.Background()
	pool := db.GetPoolInstance(ctx, os.Getenv("DATABASE_URL"), "2", "1", 1)
	defer pool.Close()

	token, hash, hint, err := services.NewAgentToken()
	if err != nil {
		log.Fatal(err)
	}
	a := &types.Agent{Name: name, TokenHash: hash, TokenHint: hint, IsActive: true}
	if err := repositories.NewAgentRepo(pool).Create(ctx, a); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Агент «%s» создан. Токен (сохраните, он показывается 1 раз):\n%s\n", a.Name, token)
	fmt.Println("Запуск агента:")
	fmt.Println(services.AgentCommand(*server, token))
}
