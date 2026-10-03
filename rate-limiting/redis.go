package ratelimit

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const bucketScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then
  tokens = limit
  ts = now_ms
end
local elapsed = now_ms - ts
if elapsed < 0 then
  elapsed = 0
end
local refill = elapsed * (limit / window_ms)
tokens = math.min(limit, tokens + refill)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HSET', key, 'tokens', tostring(tokens), 'ts', tostring(now_ms))
redis.call('PEXPIRE', key, window_ms)
return allowed
`

type RedisStore struct {
	addr    string
	timeout time.Duration
	now     func() time.Time
	mu      sync.Mutex
	conn    net.Conn
	reader  *bufio.Reader
}

func NewRedisStore(addr string, timeout time.Duration) *RedisStore {
	if timeout <= 0 {
		timeout = 50 * time.Millisecond
	}
	return &RedisStore{addr: addr, timeout: timeout, now: time.Now}
}

func (s *RedisStore) Take(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if limit < 1 || window <= 0 {
		return false, errBadLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(ctx); err != nil {
		return false, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(s.timeout)
	}
	if err := s.conn.SetDeadline(deadline); err != nil {
		s.close()
		return false, err
	}
	args := []string{
		"EVAL", bucketScript, "1", key,
		strconv.Itoa(limit),
		strconv.FormatInt(window.Milliseconds(), 10),
		strconv.FormatInt(s.now().UnixMilli(), 10),
	}
	if _, err := s.conn.Write(encodeRESP(args)); err != nil {
		s.close()
		return false, err
	}
	allowed, err := readInteger(s.reader)
	if err != nil {
		s.close()
		return false, err
	}
	return allowed == 1, nil
}

func (s *RedisStore) ensure(ctx context.Context) error {
	if s.conn != nil {
		return nil
	}
	dialer := net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	s.conn = conn
	s.reader = bufio.NewReader(conn)
	return nil
}

func (s *RedisStore) close() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = nil
	s.reader = nil
}

func encodeRESP(args []string) []byte {
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(strconv.Itoa(len(args)))
	b.WriteString("\r\n")
	for _, arg := range args {
		b.WriteString("$")
		b.WriteString(strconv.Itoa(len(arg)))
		b.WriteString("\r\n")
		b.WriteString(arg)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func readInteger(reader *bufio.Reader) (int64, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	if len(line) < 3 {
		return 0, errors.New("short redis reply")
	}
	switch line[0] {
	case ':':
		return strconv.ParseInt(strings.TrimSpace(line[1:]), 10, 64)
	case '-':
		return 0, errors.New("redis rejected the bucket script")
	default:
		return 0, errors.New("unexpected redis reply")
	}
}
