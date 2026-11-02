# Taurus Pro Consul

[![Go Version](https://img.shields.io/badge/Go-1.27.3+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/stones-hub/taurus-pro-consul)](https://goreportcard.com/report/github.com/stones-hub/taurus-pro-consul)

Taurus Pro Consul 是一个基于 HashiCorp Consul 的 Go 语言客户端封装库，提供了简洁易用的 API 接口，支持服务注册与发现、配置管理、健康检查、键值存储等核心功能。

## ✨ 功能特性

- 🔧 **服务管理**: 支持服务注册、注销、查询和健康检查
- 📝 **配置管理**: 基于 KV 存储的配置管理，支持实时监听配置变更
- 🔄 **服务发现**: 智能负载均衡，支持多种策略（随机、轮询、最少连接）
- 🏥 **健康检查**: 内置健康检查机制，自动过滤不健康服务
- 🚀 **高性能**: 基于官方 Consul API 客户端，性能优异
- 🛡️ **安全认证**: 支持 ACL Token 和 HTTP Basic Auth
- 📊 **监控日志**: 内置日志记录，便于调试和监控
- ⚡ **异步操作**: 支持异步配置监听和后台任务管理

## 📋 系统要求

- Go 1.27.3 或更高版本
- Consul 服务器（支持 v1.32.1+ API）

## 🚀 快速开始

### 安装

```bash
go get github.com/stones-hub/taurus-pro-consul
```

### 基本使用

```go
package main

import (
    "log"
    "time"
    
    consul "github.com/stones-hub/taurus-pro-consul/pkg/consul"
)

func main() {
    // 创建 Consul 客户端
    client, err := consul.NewClient(
        consul.WithAddress("localhost:8500"),
        consul.WithToken("your-acl-token"),
        consul.WithTimeout(time.Second*30),
        consul.WithLogger(log.New(os.Stdout, "[CONSUL] ", log.LstdFlags)),
    )
    if err != nil {
        log.Fatal("Failed to create consul client:", err)
    }
    defer client.Close()

    // 注册服务
    err = client.RegisterService(&consul.ServiceConfig{
        Name:    "my-service",
        Port:    8080,
        Tags:    []string{"api", "v1"},
        Address: "192.168.1.100",
        Checks: []*consul.CheckConfig{
            {
                HTTP:            "http://192.168.1.100:8080/health",
                Interval:        time.Second * 10,
                Timeout:         time.Second * 5,
                DeregisterAfter: time.Minute * 1,
            },
        },
    })
    if err != nil {
        log.Fatal("Failed to register service:", err)
    }

    // 存储配置
    config := map[string]interface{}{
        "database": map[string]string{
            "host": "localhost",
            "port": "5432",
        },
    }
    configBytes, _ := json.Marshal(config)
    err = client.Put("my-service/config", configBytes)
    if err != nil {
        log.Fatal("Failed to store config:", err)
    }

    // 监听配置变更
    var currentConfig map[string]interface{}
    err = client.WatchConfig("my-service/config", &currentConfig, &consul.WatchOptions{
        WaitTime:  time.Second * 10,
        RetryTime: time.Second * 1,
    })
    if err != nil {
        log.Fatal("Failed to watch config:", err)
    }

    // 服务调用
    invoker := client.NewServiceInvoker("my-service",
        consul.WithStrategy(consul.RoundRobin),
        consul.WithInvokeTimeout(time.Second*30),
        consul.WithRetry(3, time.Second*1),
    )

    resp, err := invoker.Call("GET", "/api/users", nil, nil)
    if err != nil {
        log.Fatal("Failed to call service:", err)
    }
    defer resp.Body.Close()

    log.Println("Service called successfully")
}
```

## 📚 API 文档

### 客户端配置

#### 创建客户端

```go
func NewClient(opts ...Option) (*Client, error)
```

#### 配置选项

| 选项 | 类型 | 描述 | 默认值 |
|------|------|------|--------|
| `WithAddress` | string | Consul 服务地址 | "localhost:8500" |
| `WithToken` | string | ACL Token | "" |
| `WithTimeout` | time.Duration | 操作超时时间 | 30s |
| `WithScheme` | string | 连接协议 | "http" |
| `WithDatacenter` | string | 数据中心 | "" |
| `WithWaitTime` | time.Duration | 查询等待时间 | 10s |
| `WithRetryTime` | time.Duration | 重试间隔时间 | 1s |
| `WithMaxRetries` | int | 最大重试次数 | 3 |
| `WithLogger` | *log.Logger | 自定义日志器 | 标准日志器 |

### 服务管理

#### 服务注册

```go
func (c *Client) RegisterService(cfg *ServiceConfig) error
```

#### 服务注销

```go
func (c *Client) DeregisterService(serviceID string) error
```

#### 服务查询

```go
func (c *Client) GetService(name string, tag string) ([]*api.ServiceEntry, error)
func (c *Client) GetHealthyServices(name string) ([]*api.ServiceEntry, error)
```

### 键值存储

#### 基本操作

```go
func (c *Client) Put(key string, value []byte) error
func (c *Client) Get(key string) ([]byte, error)
func (c *Client) Delete(key string) error
func (c *Client) List(prefix string) (map[string][]byte, error)
```

#### 原子操作

```go
func (c *Client) CAS(key string, value []byte, version uint64) (bool, error)
```

### 配置监听

```go
func (c *Client) WatchConfig(key string, config interface{}, opts *WatchOptions) error
```

### 服务调用

#### 创建调用器

```go
func (c *Client) NewServiceInvoker(serviceName string, opts ...InvokerOption) *ServiceInvoker
```

#### 调用选项

| 选项 | 类型 | 描述 | 默认值 |
|------|------|------|--------|
| `WithTags` | []string | 服务标签过滤 | [] |
| `WithStrategy` | LoadBalanceStrategy | 负载均衡策略 | RoundRobin |
| `WithInvokeTimeout` | time.Duration | 调用超时时间 | 30s |
| `WithRetry` | (int, time.Duration) | 重试策略 | (3, 1s) |

#### 负载均衡策略

- `Random`: 随机选择
- `RoundRobin`: 轮询选择
- `LeastConn`: 最少连接数

## 🏗️ 项目结构

```
taurus-pro-consul/
├── pkg/consul/           # 核心包
│   ├── client.go         # 客户端主逻辑
│   ├── service.go        # 服务管理
│   ├── kv.go            # 键值存储
│   ├── health.go        # 健康检查
│   ├── watch.go         # 配置监听
│   └── invoke.go        # 服务调用
├── bin/example/          # 示例代码
│   ├── main.go          # 主示例
│   └── feature/         # 特性示例
├── go.mod               # Go 模块文件
├── go.sum               # 依赖校验文件
└── README.md            # 项目文档
```

## 📖 详细示例

### 微服务架构示例

项目提供了完整的微服务架构示例，包含：

- **用户服务**: 用户管理和认证
- **支付服务**: 支付处理和提供商管理
- **订单服务**: 订单管理和状态跟踪

运行示例：

```bash
cd bin/example
go run main.go
```

### 配置管理示例

```go
// 监听配置变更
type AppConfig struct {
    Database struct {
        Host string `json:"host"`
        Port int    `json:"port"`
    } `json:"database"`
    Features map[string]bool `json:"features"`
}

var config AppConfig
err = client.WatchConfig("app/config", &config, &consul.WatchOptions{
    WaitTime:  time.Second * 10,
    RetryTime: time.Second * 1,
})

// 配置会自动更新到 config 变量中
```

### 健康检查示例

```go
// 注册带健康检查的服务
err = client.RegisterService(&consul.ServiceConfig{
    Name: "web-service",
    Port: 8080,
    Checks: []*consul.CheckConfig{
        {
            HTTP:            "http://localhost:8080/health",
            Interval:        time.Second * 10,
            Timeout:         time.Second * 5,
            DeregisterAfter: time.Minute * 1,
        },
        {
            TCP:             "localhost:8080",
            Interval:        time.Second * 15,
            Timeout:         time.Second * 3,
        },
    },
})
```

## 🔧 配置说明

### Consul 服务器配置

确保 Consul 服务器已启动并可访问：

```bash
# 启动 Consul 开发模式
consul agent -dev

# 或使用配置文件
consul agent -config-file=consul.json
```

### 环境变量

支持通过环境变量配置：

```bash
export CONSUL_HTTP_ADDR=localhost:8500
export CONSUL_HTTP_TOKEN=your-token
export CONSUL_HTTP_SSL_VERIFY=false
```

## 🧪 测试

运行测试：

```bash
go test ./pkg/consul/...
```

运行基准测试：

```bash
go test -bench=. ./pkg/consul/...
```

## 📝 贡献指南

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🤝 支持

如果您遇到问题或有建议，请：

1. 查看 [Issues](../../issues) 页面
2. 创建新的 Issue
3. 联系维护团队

## 🙏 致谢

- [HashiCorp Consul](https://www.consul.io/) - 优秀的服务发现和配置管理工具
- [Go 社区](https://golang.org/) - 强大的编程语言和生态系统

---

**Taurus Pro Consul** - 让 Consul 集成变得简单高效 🚀 