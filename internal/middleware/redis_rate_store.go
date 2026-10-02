package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// проверка на этапе компиляции: RedisRateStore действительно реализует RateStore
var _ RateStore = (*RedisRateStore)(nil)

// redisKeyPrefix отделяет ключи лимитера от всего остального, что может лежать в том же Redis
const redisKeyPrefix = "ratelimit:"

// allowScript увеличивает счетчик и решает, укладывается ли запрос в лимит, одной атомарной операцией
//
// Почему Lua, а не INCR и EXPIRE двумя командами: между ними соединение может оборваться,
// и ключ останется без TTL — счетчик будет расти вечно, а пользователь окажется заблокирован навсегда
// Redis выполняет скрипт целиком, не вклиниваясь командами других клиентов, поэтому INCR
// и установка TTL не разделяются
//
// KEYS[1] — ключ счетчика, ARGV[1] — окно в миллисекундах, ARGV[2] — лимит
// Возвращает 1, если запрос разрешен, и 0, если лимит исчерпан
//
// TTL ставится только при первом запросе окна (current == 1), поэтому последующие запросы,
// в том числе отклоненные, окно не продлевают: оно всегда заканчивается через window после первого запроса
//
// PEXPIRE (миллисекунды), а не EXPIRE (секунды): окно короче секунды при округлении до секунд
// стало бы нулем, и Redis сразу удалил бы ключ — лимит перестал бы работать
var allowScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
	redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
if current > tonumber(ARGV[2]) then
	return 0
end
return 1
`)

// RedisRateStore хранит счетчики в Redis, поэтому лимит общий для всех инстансов сервиса:
// пользователь получает limit запросов за окно суммарно, а не limit на каждый инстанс
//
// Отличие от MemoryRateStore сделано осознанно: там одно окно на все ключи, сбрасываемое целиком,
// здесь TTL стоит на каждом ключе отдельно — окно пользователя начинается с его первого запроса
// Для лимита вроде "10 в минуту" разница несущественна: memory проще, redis точнее по границам окна
//
// Отклоненные запросы тоже увеличивают счетчик (INCR идет до проверки лимита), в отличие от
// MemoryRateStore - на решение это не влияет: после превышения ответ всегда "нельзя" до конца окна
type RedisRateStore struct {
	client redis.Scripter
}

// NewRedisRateStore создает хранилище поверх клиента Redis
//
// Принимает redis.Scripter, а не *redis.Client: хранилищу нужно только выполнять скрипты,
// и этому интерфейсу удовлетворяют и одиночный клиент, и кластерный
// Жизненным циклом клиента (подключение, Close) владеет вызывающий код, а не хранилище
func NewRedisRateStore(client redis.Scripter) *RedisRateStore {
	return &RedisRateStore{client: client}
}

// Allow учитывает запрос по ключу key и сообщает, укладывается ли он в лимит окна
//
// Fail-closed: при недоступности Redis возвращается ошибка, а не разрешение
// Откат на in-memory счетчик здесь недопустим: при N инстансах он молча превратил бы общий лимит
// в N лимитов именно в тот момент, когда система нездорова
// Что делать с ошибкой (503), решает RateLimiter
func (s *RedisRateStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	windowMs := window.Milliseconds()
	if windowMs < 1 {
		return false, fmt.Errorf("redis rate store: окно %v короче миллисекунды", window)
	}

	result, err := allowScript.Run(ctx, s.client, []string{redisKeyPrefix + key}, windowMs, limit).Int()
	if err != nil {
		return false, fmt.Errorf("redis rate store: выполнение скрипта для ключа %q: %w", key, err)
	}

	switch result {
	case 1:
		return true, nil
	case 0:
		return false, nil
	default:
		return false, fmt.Errorf("redis rate store: неожиданный ответ скрипта: %d", result)
	}
}
