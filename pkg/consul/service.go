package consul

import (
	"context"
	"errors"
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
	Checks  []*CheckConfig    // 健康检查配置
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

	// 添加健康检查配置
	if len(cfg.Checks) > 0 {
		reg.Checks = make([]*api.AgentServiceCheck, len(cfg.Checks))
		for i, check := range cfg.Checks {
			reg.Checks[i] = &api.AgentServiceCheck{
				Name:                           fmt.Sprintf("service:%s check", cfg.ID),
				HTTP:                           check.HTTP,
				TCP:                            check.TCP,
				Interval:                       check.Interval.String(),
				Timeout:                        check.Timeout.String(),
				DeregisterCriticalServiceAfter: check.DeregisterAfter.String(),
				TLSSkipVerify:                  check.TLSSkipVerify,
				Method:                         check.Method,
				Header:                         check.Header,
			}
		}
	}

	// 注册服务
	if err := c.client.Agent().ServiceRegister(reg); err != nil {
		return fmt.Errorf("failed to register service: %v", err)
	}

	c.logger.Printf("Service registered successfully: %s (ID: %s)", cfg.Name, cfg.ID)
	return nil
}

// DeregisterService 注销服务
// 单次注销最多等待 WithTimeout 配置的时长（<=0 表示不限时），客户端 Close 会取消进行中的注销。
// 需要由调用方控制时限时使用 DeregisterServiceContext。
func (c *Client) DeregisterService(serviceID string) error {
	ctx := context.Background()
	if c.config != nil && c.config.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.timeout)
		defer cancel()
	}
	return c.DeregisterServiceContext(ctx, serviceID)
}

// DeregisterServiceContext 注销服务，ctx 取消或到期时底层 HTTP 请求随之中止
// 客户端 Close 同样会取消进行中的注销，Close 之后调用直接返回错误。
// 返回的错误可用 errors.Is 判断 context.Canceled / context.DeadlineExceeded，
// 可用 errors.As 取出 api.StatusError（例如服务不存在时 Consul 返回的 404）。
func (c *Client) DeregisterServiceContext(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return fmt.Errorf("service ID cannot be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.ctx != nil && c.ctx.Err() != nil {
		return fmt.Errorf("failed to deregister service: consul client closed: %w", c.ctx.Err())
	}

	// 客户端 Close 时取消请求
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.ctx != nil {
		stop := context.AfterFunc(c.ctx, cancel)
		defer stop()
	}

	q := (&api.QueryOptions{}).WithContext(reqCtx)
	if err := c.client.Agent().ServiceDeregisterOpts(serviceID, q); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() == nil && c.ctx != nil && c.ctx.Err() != nil {
			return fmt.Errorf("failed to deregister service: consul client closed: %w", err)
		}
		return fmt.Errorf("failed to deregister service: %w", err)
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
