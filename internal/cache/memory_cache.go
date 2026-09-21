package cache

import (
	"sync"

	"github.com/nikolaykonkin/shortlink/internal/model"
)

// MemoryCache — потокобезопасный кэш на map с sync.RWMutex: чтения на редиректе
// многократно превышают записи, и RWMutex пускает параллельные чтения
// без взаимной блокировки, в отличие от sync.Map, рассчитанного на другой паттерн доступа
type MemoryCache struct {
	mu    sync.RWMutex
	links map[string]*model.Link
}

// NewMemoryCache создает пустой кэш
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{links: make(map[string]*model.Link)}
}

func (c *MemoryCache) Get(shortCode string) (*model.Link, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	link, ok := c.links[shortCode]

	return link, ok
}

func (c *MemoryCache) Set(shortCode string, link *model.Link) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.links[shortCode] = link
}

func (c *MemoryCache) Delete(shortCode string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.links, shortCode)
}

var _ Cacher = (*MemoryCache)(nil)
