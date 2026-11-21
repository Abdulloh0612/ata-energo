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
	"time"

	"ata-energo/db"
	"ata-energo/db/repositories"
	"ata-energo/pkg/fakestation"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/joho/godotenv"
)

func main() {
	clientsN := flag.Int("clients", 25, "Сколько клиентов создать")
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
	manager := makeUser("crm-manager", "manager123", types.RoleCRMManager, "Айжан", "Менеджер")
	technician := makeUser("crm-tech", "technician123", types.RoleTechnician, "Бакыт", "Инженер")
	makeUser("crm-observer", "observer123", types.RoleObserver, "Алина", "Наблюдатель")

	cities := []string{"г. Бишкек, ул. Солнечная 1", "г. Ош, ул. Ленина 25", "г. Каракол, ул. Мира 7",
		"г. Талас, ул. Победы 12", "г. Нарын, ул. Центральная 3"}

	names := []string{"Айбек Токтобаев", "Элмира Исакова", "ОсОО «Ала-Тоо Сервис»", "Аскар Жумаев", "Мээрим Садыкова",
		"Нурлан Асанов", "Гульнара Ташиева", "ОсОО «Береке Плюс»", "Бакыт уулу Азамат", "Айгуль Мамбетова",
		"ОсОО «Ош Агро»", "Кубат Ибраимов", "Чолпон Сариева", "Азамат Кожоев", "Айпери Омурова",
		"Самат Абдраев", "ОсОО «Ак-Булак»", "Зарина Орозова", "Алмаз Турсунов", "Нурия Абдуллаева",
		"ОсОО «Южный склад»", "Марат Кадыров", "Айдана Сулайманова", "Темир Султанов", "ОсОО «Достук Энерго»"}
	var seededClients []int64
	for i := 1; i <= *clientsN; i++ {
		u := makeUser(fmt.Sprintf("client%d", i), "client123", types.RoleUser, fmt.Sprintf("Клиент %d", i), "")
		// клиент этого пользователя
		u, _ = users.GetByID(ctx, u.ID)
		var client *types.Client
		if u.ClientID != nil {
			client, _ = clients.GetByID(ctx, *u.ClientID)
		}
		if client == nil {
			name := names[(i-1)%len(names)]
			if i > len(names) {
				name = fmt.Sprintf("%s · объект %d", name, i)
			}
			client = &types.Client{UserID: &u.ID, Name: name,
				Phone: fmt.Sprintf("+996 700 00 00 0%d", i%10), Address: cities[(i-1)%len(cities)]}
			if err := clients.Create(ctx, client); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("  + клиент %s\n", client.Name)
		}

		seededClients = append(seededClients, client.ID)

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
	if len(seededClients) > 0 {
		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Fatal(err)
		}
		defer tx.Rollback(ctx)
		titles := []string{"Проверить снижение выработки", "Диагностика связи инвертора", "Плановое обслуживание станции", "Проверить заряд аккумулятора", "Осмотр после отключения сети"}
		assignees := []int64{technician.ID, manager.ID}
		for i := range 42 {
			kind := "requests"
			if i >= 30 {
				kind = "tasks"
			}
			status := []string{"open", "progress", "closed"}[i%3]
			priority := []string{"normal", "high", "low"}[i%3]
			clientID := seededClients[i%len(seededClients)]
			title := fmt.Sprintf("%s · объект %02d", titles[i%len(titles)], i+1)
			dueAt := time.Now().AddDate(0, 0, i%11-5)
			_, err = tx.Exec(ctx, `INSERT INTO crm_work (demo_key,kind,title,description,status,priority,client_id,device_id,assignee_id,created_by,due_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,(SELECT id FROM devices WHERE client_id=$7 AND device_type='inverter' ORDER BY id LIMIT 1),$8,$9,$10)
				ON CONFLICT (demo_key) DO NOTHING`, fmt.Sprintf("ata-crm-%d", i), kind, title,
				"Сверить последние показания и согласовать дальнейшие действия с клиентом.", status, priority, clientID, assignees[i%len(assignees)], manager.ID, dueAt)
			if err != nil {
				log.Fatal(err)
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm_equipment (device_id,model,nominal_watts,installed_on,serviced_on,service_due_on,warranty_until,maintenance)
			SELECT d.id,d.name,8000,CURRENT_DATE-180,CURRENT_DATE-60,CURRENT_DATE+(d.id%90)::int-25,CURRENT_DATE+(d.id%400)::int-60,d.id%11=0
			FROM devices d WHERE d.client_id=ANY($1) AND d.device_type='inverter' ON CONFLICT DO NOTHING`, seededClients)
		if err != nil {
			log.Fatal(err)
		}
		for i, site := range services.DemoEybondSites() {
			var owner *int64
			if i%4 != 0 {
				owner = &seededClients[i%len(seededClients)]
			}
			if _, err = tx.Exec(ctx, `INSERT INTO provider_sites(provider_code,external_id,client_id,name,address,timezone,status)
				VALUES('eybond',$1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, site.ExternalID, owner, site.Name, site.Address, site.Timezone, site.Status); err != nil {
				log.Fatal(err)
			}
		}
		if err = tx.Commit(ctx); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println("Готово!")
}
