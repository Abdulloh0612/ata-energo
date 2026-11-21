package services

import (
	"fmt"
	"strings"
)

// FieldErrors — ошибки по полям, как в Django REST Framework: {"login_username": ["Такой логин уже занят."]}.
// Фронтенд показывает их рядом с полями формы.
type FieldErrors map[string][]string

func (e FieldErrors) Error() string {
	var parts []string
	for k, v := range e {
		parts = append(parts, fmt.Sprintf("%s: %s", k, strings.Join(v, " ")))
	}
	return strings.Join(parts, "; ")
}

// Field — одна ошибка поля.
func Field(name, msg string) FieldErrors {
	return FieldErrors{name: {msg}}
}

// DetailError — ошибка с HTTP-статусом и текстом для человека ({"detail": "..."}).
type DetailError struct {
	Status int
	Detail string
}

func (e *DetailError) Error() string { return e.Detail }

func Detail(status int, msg string) *DetailError {
	return &DetailError{Status: status, Detail: msg}
}
