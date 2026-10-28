package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisIncrScript increments a fixed-window counter and reports both the new
// count and the time left in the window, in a single round trip.
//
// The counting has to be atomic. Doing GET, then INCR, then EXPIRE from Go would
// let two replicas interleave between the steps: both could read the old count
// and both decide the request fits, which is exactly the failure a distributed
// limit exists to prevent. A Lua script runs as one unit on the server, so the
// read-modify-write cannot be split.
//
// The first INCR on a key leaves it with no expiry. The script sets the TTL only
// on that first call, so the window is fixed from the first request rather than
// extended by each subsequent one — extending it would turn the fixed window
// into a sliding one and let a steady stream of traffic keep a key alive past
// the point where it should have reset.
var redisIncrScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
return {count, ttl}
`)

// RedisStore keeps counters in a shared Redis instance so that every API replica
// counts against the same budget.
//
// It is the reason more than one replica can be run without multiplying the
// configured limits by the replica count.
type RedisStore struct {
	client *redis.Client
}

// RedisOptions configures the connection.
type RedisOptions struct {
	// Addr is host:port of the Redis server.
	Addr string
	// Password is optional.
	Password string
	// DB is the Redis database index.
	DB int
	// DialTimeout bounds establishing a connection. It must be short: this sits
	// in the request path, and a hanging dial would hang the request with it.
	DialTimeout time.Duration
	// ReadTimeout and WriteTimeout bound a single command. They are deliberately
	// shorter than a typical client timeout so a slow store degrades to the
	// policy's fail mode instead of holding the request open.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (o RedisOptions) withDefaults() RedisOptions {
	if o.DialTimeout <= 0 {
		o.DialTimeout = 2 * time.Second
	}
	if o.ReadTimeout <= 0 {
		o.ReadTimeout = 500 * time.Millisecond
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 500 * time.Millisecond
	}
	return o
}

// NewRedisStore connects to Redis and verifies the connection with a PING.
//
// The ping matters: without it, a bad address is not discovered until the first
// request that needs a counter, and it is reported as a rate-limit store failure
// in a log rather than as a startup error next to the setting that caused it.
func NewRedisStore(ctx context.Context, opts RedisOptions) (*RedisStore, error) {
	opts = opts.withDefaults()
	if opts.Addr == "" {
		return nil, fmt.Errorf("ratelimit: redis address is empty")
	}

	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
	})

	pingCtx, cancel := context.WithTimeout(ctx, opts.DialTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ratelimit: cannot reach redis at %s: %w", opts.Addr, err)
	}

	return &RedisStore{client: client}, nil
}

// Increment runs the atomic counter script.
func (s *RedisStore) Increment(ctx context.Context, key string, windowLen time.Duration, _ time.Time) (int, time.Duration, error) {
	// The window length travels to Redis in milliseconds; passing it per call
	// rather than baking it into the key keeps one key per subject, so a policy
	// change does not leave the old counters unreachable and unexpired.
	//
	// A sub-millisecond window would round to 0, and PEXPIRE 0 deletes the key, so
	// every request would read a count of 1 and the limit would never be reached.
	// Refusing is better than a limit that silently does nothing.
	ms := windowLen.Milliseconds()
	if ms < 1 {
		return 0, 0, fmt.Errorf("ratelimit: window %s is shorter than one millisecond", windowLen)
	}

	res, err := redisIncrScript.Run(ctx, s.client, []string{key}, ms).Result()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	values, ok := res.([]any)
	if !ok || len(values) != 2 {
		return 0, 0, fmt.Errorf("%w: unexpected script reply %v", ErrUnavailable, res)
	}

	count, ok := values[0].(int64)
	if !ok {
		return 0, 0, fmt.Errorf("%w: unexpected count %v", ErrUnavailable, values[0])
	}
	ttlMillis, ok := values[1].(int64)
	if !ok {
		return 0, 0, fmt.Errorf("%w: unexpected ttl %v", ErrUnavailable, values[1])
	}

	// PTTL returns -1 when the key has no expiry and -2 when it does not exist.
	// Neither should happen after a successful INCR, but treating them as "reset
	// now" keeps the limiter from holding a request back on a value it cannot
	// interpret.
	resetIn := time.Duration(ttlMillis) * time.Millisecond
	if ttlMillis < 0 {
		resetIn = windowLen
	}

	return int(count), resetIn, nil
}

// Close releases the connection pool.
func (s *RedisStore) Close() error { return s.client.Close() }

// String names the store for logs and diagnostics.
func (s *RedisStore) String() string { return "redis" }

// Client exposes the underlying client, so a caller can build a health probe
// without this package growing a method for each check.
func (s *RedisStore) Client() *redis.Client { return s.client }
