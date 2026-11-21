package server

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"ata-energo/docs"
	"github.com/gofiber/fiber/v2"
)

func TestSwaggerCoversHTTPRoutes(t *testing.T) {
	var spec struct {
		BasePath string
		Paths    map[string]map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &spec); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	registerRoutes(app, Services{})
	documented := make(map[string]bool)
	for path, methods := range spec.Paths {
		for method := range methods {
			key := strings.ToUpper(method) + " " + spec.BasePath + strings.TrimRight(path, "/")
			documented[key] = true
		}
	}
	param := regexp.MustCompile(`:(\w+)`)
	for _, route := range app.GetRoutes(true) {
		if !strings.HasPrefix(route.Path, "/api/") || route.Method == fiber.MethodHead {
			continue
		}
		path := param.ReplaceAllString(strings.TrimRight(route.Path, "/"), "{$1}")
		key := route.Method + " " + path
		if !documented[key] {
			t.Errorf("route missing from Swagger: %s", key)
		}
		delete(documented, key)
	}
	for key := range documented {
		t.Errorf("Swagger operation has no route: %s", key)
	}
}
