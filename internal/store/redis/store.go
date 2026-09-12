package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func init() {
	store.Register(conn.DriverRedis, New)
}

// RedisSource implements store.DataSource for Redis.
type RedisSource struct {
	client   *goredis.Client
	readOnly bool
	dbIndex  int
}

// New creates and connects a RedisSource.
func New(cfg conn.ConnectionConfig) (store.DataSource, error) {
	addr := cfg.Host
	if addr == "" {
		addr = "localhost"
	}
	port := cfg.Port
	if port <= 0 {
		port = 6379
	}
	addr = fmt.Sprintf("%s:%d", addr, port)

	dbIdx := 0
	if cfg.Database != "" {
		if idx, err := strconv.Atoi(cfg.Database); err == nil && idx >= 0 {
			dbIdx = idx
		}
	}

	opts := &goredis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       dbIdx,
	}
	if cfg.User != "" {
		opts.Username = cfg.User
	}
	if cfg.SSLMode != "" && cfg.SSLMode != "disable" {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: cfg.SSLMode == "skip-verify" || cfg.SSLMode == "insecure",
		}
	}

	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis connect: %w", err)
	}

	return &RedisSource{
		client:   client,
		readOnly: cfg.ReadOnly,
		dbIndex:  dbIdx,
	}, nil
}

func (s *RedisSource) Driver() conn.Driver {
	return conn.DriverRedis
}

func (s *RedisSource) Version(ctx context.Context) (string, error) {
	info, err := s.client.Info(ctx, "server").Result()
	if err != nil {
		return "Redis", nil
	}
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "redis_version:") {
			return "Redis " + strings.TrimPrefix(line, "redis_version:"), nil
		}
	}
	return "Redis", nil
}

func (s *RedisSource) Close() error {
	return s.client.Close()
}

func (s *RedisSource) ReadOnly() bool {
	return s.readOnly
}

func (s *RedisSource) Catalog() store.CatalogDescriptor {
	return store.CatalogDescriptor{
		Title:    "KEYS",
		ItemNoun: "key",
		ListObjects: func(ctx context.Context) ([]store.CatalogItem, error) {
			var keys []string
			var cursor uint64
			for {
				var res []string
				var err error
				res, cursor, err = s.client.Scan(ctx, cursor, "*", 200).Result()
				if err != nil {
					return nil, fmt.Errorf("scan keys: %w", err)
				}
				keys = append(keys, res...)
				if cursor == 0 || len(keys) >= 1000 {
					break
				}
			}

			items := make([]store.CatalogItem, len(keys))
			for i, k := range keys {
				kType, _ := s.client.Type(ctx, k).Result()
				items[i] = store.CatalogItem{
					Name:  k,
					Badge: kType,
				}
			}
			return items, nil
		},
	}
}

func (s *RedisSource) Browse(ctx context.Context, req store.BrowseRequest) (store.BrowseResponse, error) {
	if req.ObjectName == "" {
		return store.BrowseResponse{}, errors.New("no key specified")
	}

	key := req.ObjectName
	kType, err := s.client.Type(ctx, key).Result()
	if err != nil {
		return store.BrowseResponse{}, fmt.Errorf("type: %w", err)
	}
	if kType == "none" {
		return store.BrowseResponse{}, fmt.Errorf("key %q does not exist", key)
	}

	ttlDur, _ := s.client.TTL(ctx, key).Result()
	ttlStr := formatTTL(ttlDur)

	var entries []store.KVEntry
	var total int64
	var nextCursor string
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}

	switch kType {
	case "string":
		val, err := s.client.Get(ctx, key).Result()
		if err != nil && !errors.Is(err, goredis.Nil) {
			return store.BrowseResponse{}, err
		}
		entries = append(entries, store.KVEntry{
			Index: "0",
			Value: val,
		})
		total = 1

	case "hash":
		hlen, _ := s.client.HLen(ctx, key).Result()
		total = hlen
		var cur uint64
		if req.Cursor != "" {
			cur, _ = strconv.ParseUint(req.Cursor, 10, 64)
		}
		res, ncur, err := s.client.HScan(ctx, key, cur, "*", int64(pageSize)).Result()
		if err != nil {
			return store.BrowseResponse{}, err
		}
		for i := 0; i < len(res); i += 2 {
			val := ""
			if i+1 < len(res) {
				val = res[i+1]
			}
			entries = append(entries, store.KVEntry{
				Index: res[i],
				Value: val,
			})
		}
		if ncur != 0 {
			nextCursor = strconv.FormatUint(ncur, 10)
		}

	case "list":
		llen, _ := s.client.LLen(ctx, key).Result()
		total = llen
		start := int64(req.Page * pageSize)
		stop := start + int64(pageSize) - 1
		items, err := s.client.LRange(ctx, key, start, stop).Result()
		if err != nil {
			return store.BrowseResponse{}, err
		}
		for i, it := range items {
			entries = append(entries, store.KVEntry{
				Index: strconv.FormatInt(start+int64(i), 10),
				Value: it,
			})
		}
		if (start + int64(len(items))) < total {
			nextCursor = strconv.Itoa(req.Page + 1)
		}

	case "set":
		scard, _ := s.client.SCard(ctx, key).Result()
		total = scard
		var cur uint64
		if req.Cursor != "" {
			cur, _ = strconv.ParseUint(req.Cursor, 10, 64)
		}
		members, ncur, err := s.client.SScan(ctx, key, cur, "*", int64(pageSize)).Result()
		if err != nil {
			return store.BrowseResponse{}, err
		}
		for i, m := range members {
			entries = append(entries, store.KVEntry{
				Index: strconv.Itoa(i),
				Value: m,
			})
		}
		if ncur != 0 {
			nextCursor = strconv.FormatUint(ncur, 10)
		}

	case "zset":
		zcard, _ := s.client.ZCard(ctx, key).Result()
		total = zcard
		var cur uint64
		if req.Cursor != "" {
			cur, _ = strconv.ParseUint(req.Cursor, 10, 64)
		}
		res, ncur, err := s.client.ZScan(ctx, key, cur, "*", int64(pageSize)).Result()
		if err != nil {
			return store.BrowseResponse{}, err
		}
		for i := 0; i < len(res); i += 2 {
			score := ""
			if i+1 < len(res) {
				score = res[i+1]
			}
			entries = append(entries, store.KVEntry{
				Index: strconv.Itoa(i / 2),
				Value: res[i],
				Extra: score,
			})
		}
		if ncur != 0 {
			nextCursor = strconv.FormatUint(ncur, 10)
		}
	}

	metadata := map[string]string{
		"Type": kType,
		"TTL":  ttlStr,
	}

	kvData := &store.KeyValueData{
		Key:      key,
		Type:     kType,
		TTL:      ttlStr,
		Metadata: metadata,
		Entries:  entries,
	}

	return store.BrowseResponse{
		Data:       kvData,
		HasNext:    nextCursor != "",
		NextCursor: nextCursor,
		TotalCount: total,
	}, nil
}

func (s *RedisSource) Inspect(ctx context.Context, name string) (store.InspectionView, error) {
	kType, _ := s.client.Type(ctx, name).Result()
	ttlDur, _ := s.client.TTL(ctx, name).Result()
	ttlStr := formatTTL(ttlDur)

	var memUsage int64
	var length int64
	encoding, _ := s.client.ObjectEncoding(ctx, name).Result()

	if mu, err := s.client.MemoryUsage(ctx, name).Result(); err == nil {
		memUsage = mu
	}

	switch kType {
	case "string":
		length, _ = s.client.StrLen(ctx, name).Result()
	case "hash":
		length, _ = s.client.HLen(ctx, name).Result()
	case "list":
		length, _ = s.client.LLen(ctx, name).Result()
	case "set":
		length, _ = s.client.SCard(ctx, name).Result()
	case "zset":
		length, _ = s.client.ZCard(ctx, name).Result()
	}

	serverInfo := map[string]string{}
	if info, err := s.client.Info(ctx, "memory", "server", "clients").Result(); err == nil {
		for _, line := range strings.Split(info, "\r\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				switch k {
				case "redis_version", "connected_clients", "used_memory_human", "maxmemory_human", "uptime_in_days":
					serverInfo[k] = v
				}
			}
		}
	}

	return &store.KeyValueStructure{
		Key:        name,
		Type:       kType,
		TTL:        ttlStr,
		Encoding:   encoding,
		MemUsage:   memUsage,
		Length:     length,
		ServerInfo: serverInfo,
	}, nil
}

func (s *RedisSource) Query() store.QueryExecutor {
	return &RedisExecutor{source: s}
}

// RedisExecutor executes CLI commands like GET, SET, HGETALL, SCAN, etc.
type RedisExecutor struct {
	source *RedisSource
}

func (e *RedisExecutor) Language() string {
	return "Redis Commands (RESP)"
}

func (e *RedisExecutor) PromptTitle() string {
	return "REDIS CLI"
}

func (e *RedisExecutor) Placeholder() string {
	return "GET key / HGETALL key / SET key value"
}

var redisWriteCmds = map[string]bool{
	"SET": true, "SETEX": true, "SETNX": true, "MSET": true, "APPEND": true, "INCR": true, "DECR": true, "INCRBY": true,
	"HSET": true, "HMSET": true, "HDEL": true, "HINCRBY": true,
	"LPUSH": true, "RPUSH": true, "LPOP": true, "RPOP": true, "LSET": true, "LREM": true,
	"SADD": true, "SREM": true, "SPOP": true,
	"ZADD": true, "ZREM": true, "ZINCRBY": true, "ZREMRANGEBYSCORE": true,
	"DEL": true, "UNLINK": true, "EXPIRE": true, "PERSIST": true, "FLUSHDB": true, "FLUSHALL": true, "RENAME": true,
}

func (e *RedisExecutor) IsMutation(stmt string) bool {
	fields := strings.Fields(strings.TrimSpace(stmt))
	if len(fields) == 0 {
		return false
	}
	cmd := strings.ToUpper(fields[0])
	return redisWriteCmds[cmd]
}

func (e *RedisExecutor) SplitStatements(buffer string) []store.Statement {
	return SplitRedisCommands(buffer)
}

// SplitRedisCommands splits a multi-line or semicolon-separated Redis CLI buffer
// into individual statements, tracking each statement's start line (0-indexed).
// It respects single and double quotes (including multiline strings and escapes)
// and comments starting with '#', '//', or '--'.
func SplitRedisCommands(raw string) []store.Statement {
	var stmts []store.Statement
	var b strings.Builder
	line := 0
	startLine := 0
	started := false

	flush := func() {
		if t := strings.TrimSpace(b.String()); t != "" {
			stmts = append(stmts, store.Statement{Text: t, Line: startLine})
		}
		b.Reset()
		started = false
	}

	inQuote := false
	var quoteChar rune
	inLineComment := false
	runes := []rune(raw)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inLineComment {
			if r == '\n' {
				inLineComment = false
				line++
			}
			continue
		}

		if inQuote {
			b.WriteRune(r)
			if r == '\n' {
				line++
			} else if r == '\\' && i+1 < len(runes) {
				next := runes[i+1]
				b.WriteRune(next)
				if next == '\n' {
					line++
				}
				i++
			} else if r == quoteChar {
				inQuote = false
			}
			continue
		}

		// Check for line comments: #, //, --
		if r == '#' || (r == '/' && i+1 < len(runes) && runes[i+1] == '/') || (r == '-' && i+1 < len(runes) && runes[i+1] == '-') {
			if started {
				flush()
			}
			inLineComment = true
			if r != '#' {
				i++
			}
			continue
		}

		if r == '\n' {
			flush()
			line++
			continue
		}

		if r == ';' {
			flush()
			continue
		}

		if r == '"' || r == '\'' {
			inQuote = true
			quoteChar = r
			if !started {
				startLine = line
				started = true
			}
			b.WriteRune(r)
			continue
		}

		if !started && (r != ' ' && r != '\t' && r != '\r') {
			startLine = line
			started = true
		}
		if started {
			b.WriteRune(r)
		}
	}
	flush()
	return stmts
}

// Execute runs a Redis command. If a multi-statement buffer is provided,
// it executes the statement at line using SplitStatements / StatementAt.
func (e *RedisExecutor) Execute(ctx context.Context, buffer string, line, _maxRows int) (store.DataView, error) {
	trimmed := strings.TrimSpace(buffer)
	if trimmed == "" {
		return nil, errors.New("empty command")
	}

	stmts := e.SplitStatements(buffer)
	if len(stmts) == 0 {
		return nil, errors.New("empty command")
	}
	stmtIdx := 0
	if len(stmts) > 1 {
		stmtIdx = store.StatementAt(stmts, line)
	}
	targetCmd := stmts[stmtIdx].Text

	args, err := parseRedisCommandArgs(targetCmd)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}

	cmdName := strings.ToUpper(fmt.Sprint(args[0]))
	if e.source.readOnly && redisWriteCmds[cmdName] {
		return nil, fmt.Errorf("command %s is blocked in read-only mode", cmdName)
	}

	cmd := e.source.client.Do(ctx, args...)
	val, err := cmd.Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return &store.RawTextData{
				Title: "Result",
				Text:  "(nil)",
			}, nil
		}
		return nil, err
	}

	return formatRedisResult(val), nil
}

func parseRedisCommandArgs(raw string) ([]any, error) {
	var args []any
	var current strings.Builder
	inQuote := false
	var quoteChar rune
	inToken := false
	escaped := false
	runes := []rune(raw)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inQuote {
			if escaped {
				switch r {
				case 'n':
					current.WriteRune('\n')
				case 't':
					current.WriteRune('\t')
				case 'r':
					current.WriteRune('\r')
				case '\\':
					current.WriteRune('\\')
				case '"':
					current.WriteRune('"')
				case '\'':
					current.WriteRune('\'')
				default:
					current.WriteRune('\\')
					current.WriteRune(r)
				}
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quoteChar {
				inQuote = false
				continue
			}
			current.WriteRune(r)
			continue
		}

		// Outside quote
		switch {
		case r == '"' || r == '\'':
			inQuote = true
			quoteChar = r
			inToken = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inToken {
				args = append(args, current.String())
				current.Reset()
				inToken = false
			}
		default:
			inToken = true
			current.WriteRune(r)
		}
	}

	if inQuote {
		return nil, errors.New("unclosed quote in command")
	}

	if inToken {
		args = append(args, current.String())
	}

	return args, nil
}

func formatRedisResult(val any) store.DataView {
	switch v := val.(type) {
	case string:
		return &store.TabularData{
			Columns:   []string{"value"},
			Rows:      [][]string{{v}},
			Affected:  -1,
			TotalRows: -1,
		}
	case int64:
		return &store.TabularData{
			Columns:   []string{"result (integer)"},
			Rows:      [][]string{{strconv.FormatInt(v, 10)}},
			Affected:  -1,
			TotalRows: -1,
		}
	case []any:
		rows := make([][]string, len(v))
		for i, item := range v {
			rows[i] = []string{strconv.Itoa(i), fmt.Sprint(item)}
		}
		return &store.TabularData{
			Columns:   []string{"#", "value"},
			Rows:      rows,
			Affected:  -1,
			TotalRows: -1,
		}
	case map[string]string:
		var entries []store.KVEntry
		for k, val := range v {
			entries = append(entries, store.KVEntry{
				Index: k,
				Value: val,
			})
		}
		return &store.KeyValueData{
			Type:    "hash",
			TTL:     "-",
			Entries: entries,
		}
	case map[string]any:
		var entries []store.KVEntry
		for k, val := range v {
			entries = append(entries, store.KVEntry{
				Index: k,
				Value: fmt.Sprint(val),
			})
		}
		return &store.KeyValueData{
			Type:    "hash",
			TTL:     "-",
			Entries: entries,
		}
	case map[interface{}]interface{}:
		// go-redis Do returns HGETALL (and friends) as a map keyed by the
		// reply's native types.
		var entries []store.KVEntry
		for k, val := range v {
			entries = append(entries, store.KVEntry{
				Index: fmt.Sprint(k),
				Value: fmt.Sprint(val),
			})
		}
		return &store.KeyValueData{
			Type:    "hash",
			TTL:     "-",
			Entries: entries,
		}
	default:
		return &store.RawTextData{
			Title: "Result",
			Text:  fmt.Sprintf("%v", v),
		}
	}
}

func formatTTL(d time.Duration) string {
	switch {
	case d == -1:
		return "-1 (no expiry)"
	case d == -2:
		return "-2 (expired / does not exist)"
	case d < 0:
		return "-1"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
