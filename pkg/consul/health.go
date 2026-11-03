package consul

import (
	"fmt"
	"sort"
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

// GetHealthChecks 获取本地 agent 上指定服务实例的全部健康检查（passing/warning/critical）。
//
// serviceID 是服务实例 ID（RegisterService 中的 ID，未指定时为 Name-Port），不是服务名，
// 按 ServiceID 精确匹配。查询范围是客户端所连接的本地 agent（/v1/agent/checks），
// 与 RegisterService 的注册范围一致，不会返回其他节点上的实例。
// 未知 ID 返回空结果而非错误；结果按 CheckID 排序。
func (c *Client) GetHealthChecks(serviceID string) (api.HealthChecks, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("service ID cannot be empty")
	}

	agentChecks, err := c.client.Agent().Checks()
	if err != nil {
		return nil, fmt.Errorf("failed to get health checks: %v", err)
	}

	checks := api.HealthChecks{}
	for _, ac := range agentChecks {
		if ac == nil || ac.ServiceID != serviceID {
			continue
		}
		checks = append(checks, &api.HealthCheck{
			Node:        ac.Node,
			CheckID:     ac.CheckID,
			Name:        ac.Name,
			Status:      ac.Status,
			Notes:       ac.Notes,
			Output:      ac.Output,
			ServiceID:   ac.ServiceID,
			ServiceName: ac.ServiceName,
			Type:        ac.Type,
			Namespace:   ac.Namespace,
			Partition:   ac.Partition,
			ExposedPort: ac.ExposedPort,
			Definition:  ac.Definition,
		})
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].CheckID < checks[j].CheckID })

	return checks, nil
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
