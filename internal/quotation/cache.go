package quotation

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache interface {
	Get(ctx context.Context, tenant string, request Request) (Response, int64, bool)
	Set(ctx context.Context, tenant string, request Request, response Response) error
}

type cacheValue struct {
	Response Response  `json:"response"`
	StoredAt time.Time `json:"stored_at"`
}

// Key isola a corretora e o risco normalizado. Placa entra em maiúsculas e sem espaços nas pontas.
// Motivo: a mesma placa não pode devolver à corretora A o prêmio da corretora B. Atende docs/enunciado.md, Entrega 2 item 2 (Cache): chave sem a corretora reprova.
func Key(tenant string, request Request) string {
	parts := []string{
		"quote",
		url.PathEscape(strings.TrimSpace(tenant)),
		url.PathEscape(strings.TrimSpace(request.Coverage)),
		url.PathEscape(strings.TrimSpace(request.Driver.Document)),
		url.PathEscape(strconv.Itoa(request.Driver.BirthYear)),
		url.PathEscape(strings.ToUpper(strings.TrimSpace(request.Vehicle.Plate))),
		url.PathEscape(strings.TrimSpace(request.Vehicle.Model)),
		url.PathEscape(strconv.Itoa(request.Vehicle.Year)),
		url.PathEscape(strconv.FormatInt(request.Vehicle.ValueCents, 10)),
	}
	return strings.Join(parts, ":")
}

func NewMemoryCache(ttl time.Duration, now func() time.Time) Cache {
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &memoryCache{ttl: ttl, now: now, items: map[string]cacheValue{}}
}

type memoryCache struct {
	mu    sync.Mutex
	items map[string]cacheValue
	ttl   time.Duration
	now   func() time.Time
}

func (c *memoryCache) Get(_ context.Context, tenant string, request Request) (Response, int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[Key(tenant, request)]
	if !ok {
		return Response{}, 0, false
	}
	age := c.now().Sub(item.StoredAt)
	// Motivo: idade acima do TTL não volta, mesmo que o mapa ainda tenha a entrada. O descarte do dado pessoal não espera o processo morrer. Atende docs/enunciado.md, Entrega 2 item 2 (TTL e descarte).
	if age > c.ttl {
		delete(c.items, Key(tenant, request))
		return Response{}, 0, false
	}
	return item.Response, int64(age / time.Second), true
}

func (c *memoryCache) Set(_ context.Context, tenant string, request Request, response Response) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[Key(tenant, request)] = cacheValue{Response: response, StoredAt: c.now()}
	return nil
}

func NewRedisCache(addr string, ttl time.Duration) Cache {
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	return &redisCache{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		ttl:    ttl,
		now:    time.Now,
	}
}

type redisCache struct {
	client *redis.Client
	ttl    time.Duration
	now    func() time.Time
}

func (c *redisCache) Get(ctx context.Context, tenant string, request Request) (Response, int64, bool) {
	raw, err := c.client.Get(ctx, Key(tenant, request)).Bytes()
	// Motivo: Redis fora ou chave ausente é miss, e a cotação segue para as parceiras. Atende docs/enunciado.md, Entrega 2 item 2: perder o cache não pode derrubar a cotação.
	if err != nil {
		return Response{}, 0, false
	}
	var item cacheValue
	if err := json.Unmarshal(raw, &item); err != nil {
		return Response{}, 0, false
	}
	age := c.now().Sub(item.StoredAt)
	// Motivo: a mesma regra do mapa. Entrada mais velha que o TTL não é servida, mesmo se o Redis ainda a devolver. Atende docs/enunciado.md, Entrega 2 item 2 (TTL).
	if age > c.ttl {
		return Response{}, 0, false
	}
	return item.Response, int64(age / time.Second), true
}

func (c *redisCache) Set(ctx context.Context, tenant string, request Request, response Response) error {
	body, err := json.Marshal(cacheValue{Response: response, StoredAt: c.now()})
	if err != nil {
		return err
	}
	// Motivo: o TTL do Redis é o prazo em que CPF e placa somem. Acima de 24 h o enunciado deixa de chamar isso de cache. Atende docs/enunciado.md, Entrega 2 item 2.
	err = c.client.Set(ctx, Key(tenant, request), body, c.ttl).Err()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}
