package model

import "time"

// User — пользователь сервиса
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Password  string    `json:"-"` // никогда не сериализуется в JSON
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ToResponse маппит User в публичное представление без пароля
func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		CreatedAt: u.CreatedAt,
	}
}

// Link — сокращенная ссылка
type Link struct {
	ID          int64      `json:"id"`
	ShortCode   string     `json:"short_code"`
	OriginalURL string     `json:"original_url"`
	UserID      int64      `json:"user_id"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"` // nil — ссылка бессрочная
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ToResponse маппит Link в публичное представление
func (l *Link) ToResponse() LinkResponse {
	return LinkResponse{
		ID:          l.ID,
		ShortCode:   l.ShortCode,
		OriginalURL: l.OriginalURL,
		ExpiresAt:   l.ExpiresAt,
		CreatedAt:   l.CreatedAt,
	}
}

// Click — факт одного перехода по короткой ссылке
type Click struct {
	ID        int64     `json:"id"`
	LinkID    int64     `json:"link_id"`
	ClickedAt time.Time `json:"clicked_at"`
}

// UserCreateRequest — тело запроса на регистрацию
type UserCreateRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// UserLoginRequest — тело запроса на вход
type UserLoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// UserResponse — публичное представление пользователя, без пароля
type UserResponse struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// LinkCreateRequest — тело запроса на создание короткой ссылки
type LinkCreateRequest struct {
	OriginalURL string     `json:"original_url" validate:"required,url"`
	CustomAlias string     `json:"custom_alias,omitempty" validate:"omitempty,alphanum,min=4,max=32"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// LinkResponse — публичное представление ссылки
type LinkResponse struct {
	ID          int64      `json:"id"`
	ShortCode   string     `json:"short_code"`
	OriginalURL string     `json:"original_url"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// LinkStatsResponse — агрегированная статистика переходов по ссылке
type LinkStatsResponse struct {
	LinkID     int64  `json:"link_id"`
	ShortCode  string `json:"short_code"`
	ClickCount int64  `json:"click_count"`
}
