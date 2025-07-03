package consul

import (
	"fmt"

	"github.com/hashicorp/consul/api"
)

// ServiceConfig 定义服务注册的配置
type ServiceConfig struct {
	Name    string            // 服务名称
	ID      string            // 服务实例ID，如果为空则自动生成
	Tags    []string          // 服务标签
	Address string            // 服务地址，如果为空则使用本机地址
	Port    int               // 服务端口
	Meta    map[string]string // 服务元数据
}

// RegisterService 注册服务到Consul
func (c *Client) RegisterService(cfg *ServiceConfig) error {
	if cfg == nil {
		return fmt.Errorf("service config cannot be nil")
	}

	// 验证必要字段
	if cfg.Name == "" {
		return fmt.Errorf("service name cannot be empty")
	}

	if cfg.Port <= 0 {
		return fmt.Errorf("invalid port number: %d", cfg.Port)
	}

	// 如果没有指定ID，使用Name-Port作为默认ID
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("%s-%d", cfg.Name, cfg.Port)
	}

	// 创建服务注册配置
	reg := &api.AgentServiceRegistration{
		ID:      cfg.ID,
		Name:    cfg.Name,
		Tags:    cfg.Tags,
		Port:    cfg.Port,
		Address: cfg.Address,
		Meta:    cfg.Meta,
	}

	// 注册服务
	if err := c.client.Agent().ServiceRegister(reg); err != nil {
		return fmt.Errorf("failed to register service: %v", err)
	}

	c.logger.Printf("Service registered successfully: %s (ID: %s)", cfg.Name, cfg.ID)
	return nil
}

// DeregisterService 注销服务
func (c *Client) DeregisterService(serviceID string) error {
	if serviceID == "" {
		return fmt.Errorf("service ID cannot be empty")
	}

	if err := c.client.Agent().ServiceDeregister(serviceID); err != nil {
		return fmt.Errorf("failed to deregister service: %v", err)
	}

	c.logger.Printf("Service deregistered successfully: %s", serviceID)
	return nil
}

// GetService 获取服务实例
func (c *Client) GetService(name string, tag string) ([]*api.ServiceEntry, error) {
	services, err := c.GetHealthyServices(name)
	if err != nil {
		return nil, err
	}

	// 如果指定了标签，进行过滤
	if tag != "" {
		var filtered []*api.ServiceEntry
		for _, service := range services {
			for _, t := range service.Service.Tags {
				if t == tag {
					filtered = append(filtered, service)
					break
				}
			}
		}
		services = filtered
	}

	return services, nil
}

// GetAllServices 获取所有服务
func (c *Client) GetAllServices() (map[string][]string, error) {
	services, _, err := c.client.Catalog().Services(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %v", err)
	}
	return services, nil
}
