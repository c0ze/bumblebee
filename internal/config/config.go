package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML-decodable time.Duration ("30s", "24h").
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("bad duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// Bytes is a YAML-decodable size ("512MB", "20GB", "1024").
type Bytes int64

func (b *Bytes) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	v, err := parseBytes(s)
	if err != nil {
		return err
	}
	*b = Bytes(v)
	return nil
}

func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	// KB/MB/GB are interpreted as binary (1024) units.
	mult := int64(1)
	for suffix, m := range map[string]int64{"KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30} {
		if strings.HasSuffix(s, suffix) {
			mult = m
			s = strings.TrimSpace(strings.TrimSuffix(s, suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad byte size: %w", err)
	}
	return n * mult, nil
}

type Config struct {
	Server ServerConfig  `yaml:"server"`
	Cache  CacheConfig   `yaml:"cache"`
	Routes []RouteConfig `yaml:"routes"`
}

type ServerConfig struct {
	Addr      string `yaml:"addr"`
	LogLevel  string `yaml:"log_level"`
	AuthToken string `yaml:"auth_token"`
}

type CacheConfig struct {
	DefaultBackend string `yaml:"default_backend"`
	Memory         struct {
		MaxBytes Bytes `yaml:"max_bytes"`
	} `yaml:"memory"`
	Disk struct {
		Dir      string `yaml:"dir"`
		MaxBytes Bytes  `yaml:"max_bytes"`
	} `yaml:"disk"`
}

type RouteConfig struct {
	Path     string         `yaml:"path"`
	Upstream UpstreamConfig `yaml:"upstream"`
	Cache    RouteCache     `yaml:"cache"`
	Pipeline []StageConfig  `yaml:"pipeline"`
}

type UpstreamConfig struct {
	Method         string   `yaml:"method"`
	URL            string   `yaml:"url"`
	Pool           []string `yaml:"pool"`
	ForwardBody    bool     `yaml:"forward_body"`
	ForwardHeaders []string `yaml:"forward_headers"`
	ForwardQuery   []string `yaml:"forward_query"`
	Timeout        Duration `yaml:"timeout"`
	Retries        int      `yaml:"retries"`
	MaxInflight    int      `yaml:"max_inflight"`
}

type RouteCache struct {
	Backend    string   `yaml:"backend"`
	TTL        Duration `yaml:"ttl"`
	KeyHeaders []string `yaml:"key_headers"`
	KeyQuery   []string `yaml:"key_query"`
}

type StageConfig struct {
	Type      string            `yaml:"type"`
	Params    map[string]any    `yaml:"params"`
	Overrides map[string]string `yaml:"overrides"`
}

// Load reads, env-expands, parses, validates, and defaults a config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded := os.ExpandEnv(string(raw))
	var c Config
	if err := yaml.Unmarshal([]byte(expanded), &c); err != nil {
		return nil, err
	}
	c.defaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) defaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Cache.DefaultBackend == "" {
		c.Cache.DefaultBackend = "memory"
	}
	if c.Cache.Memory.MaxBytes == 0 {
		c.Cache.Memory.MaxBytes = Bytes(512 << 20) // 512 MiB
	}
	if c.Cache.Disk.Dir != "" && c.Cache.Disk.MaxBytes == 0 {
		c.Cache.Disk.MaxBytes = Bytes(20 << 30) // 20 GiB
	}
	for i := range c.Routes {
		r := &c.Routes[i]
		if r.Upstream.Method == "" {
			r.Upstream.Method = "GET"
		}
		if r.Cache.Backend == "" {
			r.Cache.Backend = c.Cache.DefaultBackend
		}
		if r.Upstream.Timeout == 0 {
			r.Upstream.Timeout = Duration(30 * time.Second)
		}
	}
}

func (c *Config) validate() error {
	if len(c.Routes) == 0 {
		return fmt.Errorf("no routes configured")
	}
	for i, r := range c.Routes {
		if r.Path == "" {
			return fmt.Errorf("route %d: path is required", i)
		}
		if r.Upstream.URL == "" {
			return fmt.Errorf("route %s: upstream.url is required", r.Path)
		}
		if len(r.Upstream.Pool) == 0 {
			return fmt.Errorf("route %s: upstream.pool must have at least one host", r.Path)
		}
		if len(r.Pipeline) == 0 {
			return fmt.Errorf("route %s: pipeline must have at least one stage", r.Path)
		}
		switch r.Cache.Backend {
		case "memory":
		case "disk":
			if c.Cache.Disk.Dir == "" {
				return fmt.Errorf("route %s: cache.disk.dir is required for the disk backend", r.Path)
			}
		default:
			return fmt.Errorf("route %s: unsupported cache backend %q (memory, disk)", r.Path, r.Cache.Backend)
		}
	}
	return nil
}
