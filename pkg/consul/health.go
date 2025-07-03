package consul

import (
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"
)

// CheckConfig 定义健康检查配置
type CheckConfig struct {
	HTTP            string              // HTTP 检查URL
	TCP             string              // TCP 检查地址
	Interval        time.Duration       // 检查间隔
	Timeout         time.Duration       // 检查超时
	DeregisterAfter time.Duration       // 取消注册时间
	TLSSkipVerify   bool                // 是否跳过TLS验证
	Method          string              // HTTP方法
	Header          map[string][]string // HTTP头
}

// GetHealthChecks 获取服务的健康检查状态
func (c *Client) GetHealthChecks(serviceID string) (api.HealthChecks, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("service ID cannot be empty")
	}

	// 先获取服务的所有实例
	services, err := c.GetHealthyServices(serviceID)
	if err != nil {
		return nil, err
	}

	// 收集所有健康检查
	var allChecks api.HealthChecks
	for _, service := range services {
		allChecks = append(allChecks, service.Checks...)
	}

	return allChecks, nil
}

// GetHealthyServices 获取健康的服务列表
func (c *Client) GetHealthyServices(name string) ([]*api.ServiceEntry, error) {
	if name == "" {
		return nil, fmt.Errorf("service name cannot be empty")
	}

	services, _, err := c.client.Health().Service(name, "", true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy services: %v", err)
	}
	return services, nil
}
