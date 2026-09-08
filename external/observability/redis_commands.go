package observability

import (
	"errors"
	"maps"
	"strings"

	redis "github.com/go-redis/redis/v7"
)

const (
	maxRedisModuleCommands = 64
	maxRedisCommandLength  = 64
)

// WithRedisModuleCommands permits up to 64 additional static command names on
// a Redis hook. Names must be 1–64 ASCII bytes, begin with a letter, and contain
// only letters, digits, dots, underscores, or hyphens. Matching is lowercase.
// The reserved telemetry categories unknown and batch cannot be registered.
//
// Supply trusted startup constants, never request values. The option snapshots
// its input and each hook receives an independent immutable command set. When
// supplied more than once, the last option replaces the preceding set; an empty
// list clears it. This keeps the per-hook vocabulary bounded across options.
func WithRedisModuleCommands(names ...string) (RedisHookOption, error) {
	if len(names) > maxRedisModuleCommands {
		return nil, errors.New("Redis module commands cannot exceed 64 entries")
	}
	commands := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validRedisCommandToken(name) {
			return nil, errors.New("Redis module command names must be 1–64 byte ASCII tokens beginning with a letter")
		}
		name = strings.ToLower(name)
		if name == unknownRedisOperation || name == strings.ToLower(redisBatchOperation) {
			return nil, errors.New("Redis module command name is reserved for telemetry")
		}
		commands[name] = struct{}{}
	}
	return redisHookOptionFunc(func(config *redisHookConfig) {
		config.moduleCommands = maps.Clone(commands)
	}), nil
}

// operationName always uses finite membership, not merely a character or
// length check. Even syntactically plausible custom names can contain secrets.
func (hook *redisHook) operationName(command redis.Cmder) string {
	if command == nil {
		return unknownRedisOperation
	}
	name := strings.TrimSpace(command.Name())
	if !validRedisCommandToken(name) {
		return unknownRedisOperation
	}
	name = strings.ToLower(name)
	if knownRedisCoreCommand(name) {
		return name
	}
	if _, registered := hook.moduleCommands[name]; registered {
		return name
	}
	return unknownRedisOperation
}

func validRedisCommandToken(name string) bool {
	if len(name) == 0 || len(name) > maxRedisCommandLength || !redisCommandLetter(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !redisCommandLetter(character) && (character < '0' || character > '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func redisCommandLetter(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
}

// knownRedisCoreCommand is a fixed vocabulary, including the core operations
// issued by go-redis v7. Subcommands stay grouped under their core command;
// argument inspection and runtime Redis COMMAND discovery are unnecessary.
func knownRedisCoreCommand(name string) bool {
	switch name {
	case "acl", "append", "asking", "auth", "bgrewriteaof", "bgsave",
		"bitcount", "bitfield", "bitfield_ro", "bitop", "bitpos",
		"blmove", "blmpop", "blpop", "brpop", "brpoplpush", "bzmpop", "bzpopmax", "bzpopmin",
		"client", "cluster", "command", "config", "copy", "dbsize", "debug", "decr", "decrby",
		"del", "discard", "dump", "echo", "eval", "eval_ro", "evalsha", "evalsha_ro", "exec",
		"exists", "expire", "expireat", "expiretime", "failover", "fcall", "fcall_ro",
		"flushall", "flushdb", "function", "geoadd", "geodist", "geohash", "geopos",
		"georadius", "georadius_ro", "georadiusbymember", "georadiusbymember_ro", "geosearch", "geosearchstore",
		"get", "getbit", "getdel", "getex", "getrange", "getset", "hdel", "hello", "hexists",
		"hget", "hgetall", "hincrby", "hincrbyfloat", "hkeys", "hlen", "hmget", "hmset",
		"hrandfield", "hscan", "hset", "hsetnx", "hstrlen", "hvals", "incr", "incrby",
		"incrbyfloat", "info", "keys", "lastsave", "latency", "lcs", "lindex", "linsert",
		"llen", "lmove", "lmpop", "lolwut", "lpop", "lpos", "lpush", "lpushx", "lrange",
		"lrem", "lset", "ltrim", "memory", "mget", "migrate", "module", "monitor", "move",
		"mset", "msetnx", "multi", "object", "persist", "pexpire", "pexpireat", "pexpiretime",
		"pfadd", "pfcount", "pfdebug", "pfmerge", "pfselftest", "ping", "psetex", "psubscribe",
		"psync", "pttl", "publish", "pubsub", "punsubscribe", "quit", "randomkey", "readonly",
		"readwrite", "rename", "renamenx", "replconf", "replicaof", "reset", "restore", "restore-asking",
		"role", "rpop", "rpoplpush", "rpush", "rpushx", "sadd", "save", "scan", "scard", "script",
		"sdiff", "sdiffstore", "select", "sentinel", "set", "setbit", "setex", "setnx", "setrange",
		"shutdown", "sinter", "sintercard", "sinterstore", "sismember", "slaveof", "slowlog", "smembers",
		"smismember", "smove", "sort", "sort_ro", "spop", "spublish", "srandmember", "srem", "sscan",
		"ssubscribe", "strlen", "subscribe", "substr", "sunion", "sunionstore", "sunsubscribe", "swapdb",
		"sync", "time", "touch", "ttl", "type", "unlink", "unsubscribe", "unwatch", "wait", "waitaof",
		"watch", "xack", "xadd", "xautoclaim", "xclaim", "xdel", "xgroup", "xinfo", "xlen",
		"xpending", "xrange", "xread", "xreadgroup", "xrevrange", "xsetid", "xtrim", "zadd", "zcard",
		"zcount", "zdiff", "zdiffstore", "zincrby", "zinter", "zintercard", "zinterstore", "zlexcount",
		"zmpop", "zmscore", "zpopmax", "zpopmin", "zrandmember", "zrange", "zrangebylex", "zrangebyscore",
		"zrangestore", "zrank", "zrem", "zremrangebylex", "zremrangebyrank", "zremrangebyscore", "zrevrange",
		"zrevrangebylex", "zrevrangebyscore", "zrevrank", "zscan", "zscore", "zunion", "zunionstore":
		return true
	default:
		return false
	}
}
