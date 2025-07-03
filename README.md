# Taurus Pro Consul

一个功能强大的 Consul 服务发现和配置管理包，提供简单易用的 API 来管理分布式系统中的服务注册、发现、配置管理和服务间调用。

## 功能特性

- 服务注册与发现
- 健康检查
- 配置管理和实时更新
- 服务间调用（支持负载均衡和重试机制）
- 标签化服务管理
- 优雅的错误处理

## 快速开始

### 安装

```bash
go get github.com/yelei-cn/taurus-pro-consul
```

### 基本使用

```go
import consul "github.com/yelei-cn/taurus-pro-consul/pkg/consul"

// 创建客户端
client, err := consul.NewClient(
    consul.WithAddress("localhost:8500"),
    consul.WithLogger(logger),
    consul.WithTimeout(time.Second*5),
)

// 注册服务
err = client.RegisterService(&consul.ServiceConfig{
    Name:    "my-service",
    ID:      "my-service-1",
    Address: "localhost",
    Port:    8080,
    Tags:    []string{"api", "v1"},
    Checks: []*consul.CheckConfig{
        {
            HTTP:            "http://localhost:8080/health",
            Interval:        time.Second * 10,
            Timeout:         time.Second * 5,
            DeregisterAfter: time.Minute,
        },
    },
})
```

## 功能演示

本示例展示了一个完整的微服务系统，包含用户服务、支付服务和订单服务。

### 1. 启动服务

```bash
cd bin/example
go run main.go
```

服务启动后，将看到以下输出：
```
[MAIN] Value put for key: config/user-service
[MAIN] Value put for key: config/order-service
[MAIN] Value put for key: config/payment-service
[MAIN] Service registered successfully: user-service (ID: user-service-1)
[USER-SERVICE] Starting user service on port 8081
[MAIN] Service registered successfully: order-service (ID: order-service-1)
[MAIN] Service registered successfully: payment-service (ID: payment-service-1)
[ORDER-SERVICE] Starting order service on port 8083
[PAYMENT-SERVICE] Starting payment service on port 8082
```

### 2. 验证服务健康状态

检查用户服务的健康状态：
```bash
curl http://192.168.3.240:8500/v1/health/service/user-service
```

响应示例：
```json
[
  {
    "Node": {
      "ID": "19df7117-a804-85bf-7f01-0d75a5f2904a",
      "Node": "consul-node",
      ...
    },
    "Service": {
      "ID": "user-service-1",
      "Service": "user-service",
      "Tags": ["api", "v1", "user"],
      "Address": "192.168.40.30",
      "Port": 8081,
      ...
    },
    "Checks": [
      {
        "Status": "passing",
        "Output": "HTTP GET http://192.168.40.30:8081/health: 200 OK Output: {\"status\":\"healthy\"}"
      }
    ]
  }
]
```

### 3. 测试配置管理

更新用户服务配置：
```bash
curl -X PUT -d '{
  "database": {
    "host": "192.168.40.30",
    "port": 5432,
    "username": "user_service",
    "password": "password123"
  },
  "features": {
    "email_verification": false,
    "sms_notification": true,
    "oauth_login": true
  }
}' http://192.168.3.240:8500/v1/kv/config/user-service
```

验证配置更新：
```bash
curl http://192.168.40.30:8081/users/verify
```

预期响应：
```
Email verification is disabled
```

### 4. 测试服务调用链

创建订单（触发完整的服务调用链）：
```bash
curl "http://192.168.40.30:8083/orders/create?user_id=123&amount=100"
```

响应示例：
```json
{
  "expire_in": "30 minutes",
  "order_id": "ORD-123-1751536571",
  "payment": {
    "payment_id": "pay_123",
    "provider": "stripe",
    "provider_info": {
      "api_key": "sk_test_123",
      "endpoint": "https://api.stripe.com/v1",
      "timeout": 30
    },
    "status": "processed",
    "user": {
      "features": {
        "email_verification": false,
        "oauth_login": true,
        "sms_notification": true
      },
      "id": "123",
      "name": "Test User"
    }
  },
  "status": "created"
}
```

## 架构说明

示例系统包含三个微服务：

1. 用户服务 (8081端口)
   - 提供用户信息和验证
   - 支持可配置的功能开关
   - 提供健康检查接口

2. 支付服务 (8082端口)
   - 处理支付请求
   - 调用用户服务验证用户信息
   - 支持多支付提供商配置

3. 订单服务 (8083端口)
   - 创建和管理订单
   - 协调用户服务和支付服务
   - 展示完整的服务调用链

## 贡献

欢迎提交 Issue 和 Pull Request。

## 许可证

MIT License 