# Taurus Pro Consul

一个简单易用的 Consul 客户端封装库。

## 功能特性

- 服务注册与发现
- KV 存储操作
- 健康检查
- 配置变更监听
- 灵活的配置选项
- 自动重试机制
- 支持 HTTP Basic Auth

## 安装

```bash
go get github.com/yelei-cn/taurus-pro-consul
```

## 快速开始

```go
package main

import (
    "fmt"
    "log"
    "time"
    
    consul "github.com/yelei-cn/taurus-pro-consul/pkg/consul"
)

func main() {
    // 创建客户端（使用默认配置）
    client, err := consul.NewClient()
    if err != nil {
        log.Fatal(err)
    }
    
    // 或者使用自定义配置
    client, err = consul.NewClient(
        consul.WithAddress("localhost:8500"),
        consul.WithToken("your-acl-token"),
        consul.WithTimeout(5 * time.Second),
        consul.WithScheme("https"),
        consul.WithMaxRetries(5),
        consul.WithBasicAuth("username", "password"),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // 注册服务
    err = client.Register("my-service", "service-1", "127.0.0.1", 8080, []string{"v1"})
    if err != nil {
        log.Fatal(err)
    }
    
    // 获取服务
    services, err := client.GetService("my-service", "v1")
    if err != nil {
        log.Fatal(err)
    }
    
    for _, service := range services {
        fmt.Printf("Found service: %s at %s:%d\n", 
            service.Service.Service,
            service.Service.Address,
            service.Service.Port)
    }
    
    // KV操作
    err = client.Put("config/app", []byte("hello world"))
    if err != nil {
        log.Fatal(err)
    }
    
    value, err := client.Get("config/app")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Value: %s\n", string(value))
    
    // 监听配置变更
    ch, err := client.WatchKey("config/app")
    if err != nil {
        log.Fatal(err)
    }
    
    go func() {
        for value := range ch {
            fmt.Printf("Config updated: %s\n", string(value))
        }
    }()
}
```

## 配置选项

客户端支持多个配置选项，所有选项都是可选的，未指定时使用默认值：

- `WithAddress(address string)` - 设置Consul地址，默认 "127.0.0.1:8500"
- `WithToken(token string)` - 设置ACL Token
- `WithTimeout(timeout time.Duration)` - 设置操作超时时间，默认 10秒
- `WithScheme(scheme string)` - 设置连接协议（http/https），默认 "http"
- `WithDatacenter(datacenter string)` - 设置数据中心
- `WithWaitTime(waitTime time.Duration)` - 设置查询等待时间，默认 10秒
- `WithRetryTime(retryTime time.Duration)` - 设置重试间隔时间，默认 3秒
- `WithMaxRetries(maxRetries int)` - 设置最大重试次数，默认 3次
- `WithLogger(logger *log.Logger)` - 设置自定义日志器
- `WithBasicAuth(username, password string)` - 设置HTTP Basic Auth认证信息

### 默认配置

```go
defaultConfig := &Config{
    address:    "127.0.0.1:8500",
    timeout:    10 * time.Second,
    scheme:     "http",
    waitTime:   time.Second * 10,
    retryTime:  time.Second * 3,
    maxRetries: 3,
    logger:     log.New(os.Stdout, "[CONSUL] ", log.LstdFlags),
}
```

### 主要方法

- `NewClient(opts ...Option) (*Client, error)` - 创建新的客户端
- `Register(name, id, address string, port int, tags []string) error` - 注册服务
- `Deregister(id string) error` - 注销服务
- `GetService(name, tag string) ([]*api.ServiceEntry, error)` - 获取服务实例
- `Put(key string, value []byte) error` - 写入KV
- `Get(key string) ([]byte, error)` - 获取KV
- `Delete(key string) error` - 删除KV
- `WatchKey(key string) (<-chan []byte, error)` - 监听KV变更
- `GetHealthyServices(name string) ([]*api.ServiceEntry, error)` - 获取健康的服务列表
- `GetAllServices() (map[string][]string, error)` - 获取所有服务

## 特性说明

1. 自动重试
   - 客户端创建时会自动进行连接测试
   - 连接失败时会根据配置进行重试
   - 可通过配置项调整重试次数和间隔

2. 安全连接
   - 支持 HTTPS
   - 支持 ACL Token
   - 支持 Basic Auth

3. 灵活配置
   - 所有配置项都是可选的
   - 使用函数式选项模式，易于扩展
   - 提供合理的默认值

4. 日志记录
   - 默认提供基本日志记录
   - 支持自定义日志器
   - 记录重要操作和错误信息

## 注意事项

1. 确保Consul服务已经启动并可访问
2. 建议在生产环境中配置ACL Token
3. 服务注册时建议使用唯一的服务ID
4. 监听配置变更时注意处理channel关闭的情况
5. 在生产环境中建议配置适当的重试策略

## License

MIT
