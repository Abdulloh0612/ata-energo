package services

import (
	"context"
	"net/http"
	"strings"

	"ata-energo/db/repositories"
	"ata-energo/types"
)

// UserService — CRUD пользователей (/api/users/, только admin и it_head).
type UserService struct {
	users      repositories.UserRepository
	roleExists func(context.Context, string) (bool, error)
}

func NewUserService(users repositories.UserRepository, roleExists func(context.Context, string) (bool, error)) *UserService {
	return &UserService{users: users, roleExists: roleExists}
}

func (s *UserService) List(ctx context.Context, f types.UserFilter) ([]types.UserView, error) {
	list, err := s.users.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]types.UserView, len(list))
	for i, u := range list {
		out[i] = types.NewUserView(u)
	}
	return out, nil
}

func (s *UserService) Get(ctx context.Context, id int64) (*types.UserView, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil || u == nil {
		return nil, err
	}
	v := types.NewUserView(*u)
	return &v, nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// apply переносит переданные поля в пользователя и проверяет их.
func (s *UserService) apply(ctx context.Context, u *types.User, w types.UserWrite, creating bool) error {
	errs := FieldErrors{}
	if w.Username != nil {
		name := strings.TrimSpace(*w.Username)
		if name == "" {
			errs["username"] = []string{"Обязательное поле."}
		} else if taken, err := s.users.UsernameTaken(ctx, name, u.ID); err != nil {
			return err
		} else if taken {
			errs["username"] = []string{"Пользователь с таким именем уже существует."}
		}
		u.Username = name
	} else if creating {
		errs["username"] = []string{"Обязательное поле."}
	}
	if w.Password != nil && *w.Password != "" {
		if len([]rune(*w.Password)) < 6 {
			errs["password"] = []string{"Убедитесь, что это значение содержит не менее 6 символов."}
		} else {
			hash, err := HashPassword(*w.Password)
			if err != nil {
				return err
			}
			u.PasswordHash = hash
		}
	} else if creating {
		errs["password"] = []string{"Укажите пароль (минимум 6 символов)."}
	}
	if w.Role != nil {
		exists, err := s.roleExists(ctx, *w.Role)
		if err != nil {
			return err
		}
		if !exists {
			errs["role"] = []string{"Значения «" + *w.Role + "» нет среди допустимых вариантов."}
		}
		u.Role = *w.Role
	}
	if w.FirstName != nil {
		u.FirstName = *w.FirstName
	}
	if w.LastName != nil {
		u.LastName = *w.LastName
	}
	if w.Email != nil {
		u.Email = *w.Email
	}
	if w.Phone != nil {
		u.Phone = *w.Phone
	}
	if w.Position != nil {
		u.Position = *w.Position
	}
	if w.IsActive != nil {
		u.IsActive = *w.IsActive
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (s *UserService) Create(ctx context.Context, w types.UserWrite) (*types.UserView, error) {
	u := &types.User{Role: types.RoleUser, IsActive: true}
	if err := s.apply(ctx, u, w, true); err != nil {
		return nil, err
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	return s.Get(ctx, u.ID)
}

func (s *UserService) Update(ctx context.Context, id int64, w types.UserWrite) (*types.UserView, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil || u == nil {
		return nil, err
	}
	if err := s.apply(ctx, u, w, false); err != nil {
		return nil, err
	}
	if err := s.users.Update(ctx, u); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete удаляет пользователя. Удалить самого себя нельзя.
func (s *UserService) Delete(ctx context.Context, id, currentUserID int64) error {
	if id == currentUserID {
		return Detail(http.StatusBadRequest, "Нельзя удалить самого себя.")
	}
	return s.users.Delete(ctx, id)
}
