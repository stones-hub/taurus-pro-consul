// Package consul 提供了对Consul服务的基本封装
package consul

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hashicorp/consul/api"
)

// Client 是Consul客户端的封装
type Client struct {
	client *api.Client
	logger *log.Logger
	config *Config
}

// Config 是Consul客户端的配置
type Config struct {
	address     string             // Consul服务地址，例如：127.0.0.1:8500
	token       string             // ACL Token
	timeout     time.Duration      // 操作超时时间
	scheme      string             // 连接协议（http/https）
	datacenter  string             // 数据中心
	waitTime    time.Duration      // 查询等待时间
	retryTime   time.Duration      // 重试间隔时间
	maxRetries  int                // 最大重试次数
	logger      *log.Logger        // 自定义日志器
	credentials *api.HttpBasicAuth // HTTP Basic Auth 认证信息
}

// Option 定义配置选项函数类型
type Option func(*Config)

// WithAddress 设置Consul地址
func WithAddress(address string) Option {
	return func(c *Config) {
		c.address = address
	}
}

// WithToken 设置ACL Token
func WithToken(token string) Option {
	return func(c *Config) {
		c.token = token
	}
}

// WithTimeout 设置操作超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.timeout = timeout
	}
}

// WithScheme 设置连接协议
func WithScheme(scheme string) Option {
	return func(c *Config) {
		c.scheme = scheme
	}
}

// WithDatacenter 设置数据中心
func WithDatacenter(datacenter string) Option {
	return func(c *Config) {
		c.datacenter = datacenter
	}
}

// WithWaitTime 设置查询等待时间
func WithWaitTime(waitTime time.Duration) Option {
	return func(c *Config) {
		c.waitTime = waitTime
	}
}

// WithRetryTime 设置重试间隔时间
func WithRetryTime(retryTime time.Duration) Option {
	return func(c *Config) {
		c.retryTime = retryTime
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(maxRetries int) Option {
	return func(c *Config) {
		c.maxRetries = maxRetries
	}
}

// WithLogger 设置自定义日志器
func WithLogger(logger *log.Logger) Option {
	return func(c *Config) {
		c.logger = logger
	}
}

// WithBasicAuth 设置HTTP Basic Auth认证信息
func WithBasicAuth(username, password string) Option {
	return func(c *Config) {
		c.credentials = &api.HttpBasicAuth{
			Username: username,
			Password: password,
		}
	}
}

// NewClient 创建新的Consul客户端
func NewClient(opts ...Option) (*Client, error) {
	// 初始化默认配置
	cfg := &Config{
		address:    "127.0.0.1:8500",
		timeout:    10 * time.Second,
		scheme:     "http",
		waitTime:   time.Second * 10,
		retryTime:  time.Second * 3,
		maxRetries: 3,
		logger:     log.New(os.Stdout, "[CONSUL] ", log.LstdFlags),
	}

	// 应用自定义选项
	for _, opt := range opts {
		opt(cfg)
	}

	// 创建Consul API配置
	config := api.DefaultConfig()
	config.Address = cfg.address
	config.Token = cfg.token
	config.Scheme = cfg.scheme
	config.Datacenter = cfg.datacenter
	config.WaitTime = cfg.waitTime
	config.HttpAuth = cfg.credentials

	// 创建Consul客户端
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %v", err)
	}

	// 测试连接（带重试机制）
	var lastErr error
	for i := 0; i <= cfg.maxRetries; i++ {
		if _, _, err := client.Health().State("any", nil); err == nil {
			// 连接成功
			return &Client{
				client: client,
				logger: cfg.logger,
				config: cfg,
			}, nil
		} else {
			lastErr = err
			if i < cfg.maxRetries {
				cfg.logger.Printf("Failed to connect to consul (attempt %d/%d): %v", i+1, cfg.maxRetries, err)
				time.Sleep(cfg.retryTime)
			}
		}
	}

	return nil, fmt.Errorf("failed to connect to consul after %d attempts: %v", cfg.maxRetries, lastErr)
}
