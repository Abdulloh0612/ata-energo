// Тестовые данные (аналог python manage.py seed_data):
//
//	admin / admin123        (роль admin)
//	ithead / ithead123      (роль it_head)
//	dispatcher / disp123    (роль dispatcher)
//	client1 / client123, client2 / client123 (клиенты)
//
// У каждого клиента модем (SN 2710000001, 2710000002 ...). Устройства модема
// (инвертор, панели, батарея, сеть, нагрузка, генератор) создаются автоматически.
// Генератор есть только у клиентов с нечётным номером.
//
// Можно запускать много раз — дубликаты не создаются.
//
//	go run ./cmd/seeddata/ -clients 2
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"ata-energo/db"
	"ata-energo/db/repositories"
	"ata-energo/pkg/fakestation"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/joho/godotenv"
)

func main() {
	clientsN := flag.Int("clients", 2, "Сколько клиентов создать")
	flag.Parse()
	_ = godotenv.Load()

	ctx := context.Background()
	pool := db.GetPoolInstance(ctx, os.Getenv("DATABASE_URL"), "5", "1", 1)
	defer pool.Close()

	users := repositories.NewUserRepo(pool)
	clients := repositories.NewClientRepo(pool)
	modems := repositories.NewModemRepo(pool)
	devices := repositories.NewDeviceRepo(pool)
	modemSvc := services.NewModemService(modems, devices)

	makeUser := func(username, password, role, firstName, position string) *types.User {
		u, err := users.GetByUsername(ctx, username)
		if err != nil {
			log.Fatal(err)
		}
		if u != nil {
			return u
		}
		hash, err := services.HashPassword(password)
		if err != nil {
			log.Fatal(err)
		}
		u = &types.User{Username: username, PasswordHash: hash, Role: role, FirstName: firstName,
			Position: position, IsActive: true, IsSuperuser: role == types.RoleAdmin}
		if err := users.Create(ctx, u); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  + пользователь %s / %s (%s)\n", username, password, role)
		return u
	}

	fmt.Println("Создаю тестовые данные...")
	makeUser("admin", "admin123", types.RoleAdmin, "Админ", "Администратор")
	makeUser("ithead", "ithead123", types.RoleITHead, "Руководитель", "Руководитель IT")
	makeUser("dispatcher", "disp123", types.RoleDispatcher, "Диспетчер", "Диспетчер")

	cities := []string{"г. Бишкек, ул. Солнечная 1", "г. Ош, ул. Ленина 25", "г. Каракол, ул. Мира 7",
		"г. Талас, ул. Победы 12", "г. Нарын, ул. Центральная 3"}

	for i := 1; i <= *clientsN; i++ {
		u := makeUser(fmt.Sprintf("client%d", i), "client123", types.RoleUser, fmt.Sprintf("Клиент %d", i), "")
		// клиент этого пользователя
		u, _ = users.GetByID(ctx, u.ID)
		var client *types.Client
		if u.ClientID != nil {
			client, _ = clients.GetByID(ctx, *u.ClientID)
		}
		if client == nil {
			client = &types.Client{UserID: &u.ID, Name: fmt.Sprintf("Клиент %d", i),
				Phone: fmt.Sprintf("+996 700 00 00 0%d", i%10), Address: cities[(i-1)%len(cities)]}
			if err := clients.Create(ctx, client); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("  + клиент %s\n", client.Name)
		}

		serial := fakestation.SeedModemSerial(i)
		m, err := modems.GetBySerial(ctx, serial)
		if err != nil {
			log.Fatal(err)
		}
		created := false
		if m == nil {
			m = &types.Modem{ClientID: client.ID, Serial: serial, Name: "Инвертор 8 кВт",
				HasGenerator: fakestation.SeedHasGenerator(i)}
			if err := modems.Create(ctx, m); err != nil {
				log.Fatal(err)
			}
			created = true
		}
		if err := modemSvc.SyncModemDevices(ctx, m); err != nil {
			log.Fatal(err)
		}
		if created {
			gen := "нет"
			if m.HasGenerator {
				gen = "есть"
			}
			fmt.Printf("    + модем %s (генератор: %s)\n", m.Serial, gen)
		}
	}
	fmt.Println("Готово!")
}
