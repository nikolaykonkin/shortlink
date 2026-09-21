package cache

import "github.com/nikolaykonkin/shortlink/internal/model"

// Cacher описывает кэш ссылок по короткому коду
type Cacher interface {
	Get(shortCode string) (*model.Link, bool)
	Set(shortCode string, link *model.Link)
	Delete(shortCode string)
}
