package consul

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/consul/api"
)

// LoadBalanceStrategy 定义负载均衡策略
type LoadBalanceStrategy int

const (
	// Random 随机选择一个服务实例
	Random LoadBalanceStrategy = iota
	// RoundRobin 轮询选择服务实例
	RoundRobin
	// LeastConn 最少连接数
	LeastConn
)

// ServiceInvoker 服务调用器
type ServiceInvoker struct {
	client        *Client
	serviceName   string
	tags          []string
	strategy      LoadBalanceStrategy
	timeout       time.Duration
	retryCount    int
	retryInterval time.Duration
	currentIndex  int // 用于轮询策略
	httpClient    *http.Client
}

// InvokerOption 定义服务调用器的配置选项
type InvokerOption func(*ServiceInvoker)

// WithTags 设置服务标签过滤
func WithTags(tags []string) InvokerOption {
	return func(i *ServiceInvoker) {
		i.tags = tags
	}
}

// WithStrategy 设置负载均衡策略
func WithStrategy(strategy LoadBalanceStrategy) InvokerOption {
	return func(i *ServiceInvoker) {
		i.strategy = strategy
	}
}

// WithTimeout 设置调用超时时间
func WithInvokeTimeout(timeout time.Duration) InvokerOption {
	return func(i *ServiceInvoker) {
		i.timeout = timeout
		i.httpClient.Timeout = timeout
	}
}

// WithRetry 设置重试策略
func WithRetry(count int, interval time.Duration) InvokerOption {
	return func(i *ServiceInvoker) {
		i.retryCount = count
		i.retryInterval = interval
	}
}

// NewServiceInvoker 创建服务调用器
func (c *Client) NewServiceInvoker(serviceName string, opts ...InvokerOption) *ServiceInvoker {
	invoker := &ServiceInvoker{
		client:        c,
		serviceName:   serviceName,
		strategy:      RoundRobin, // 默认使用轮询策略
		timeout:       time.Second * 30,
		retryCount:    3,
		retryInterval: time.Second,
		httpClient:    &http.Client{},
	}

	// 应用选项
	for _, opt := range opts {
		opt(invoker)
	}

	// 设置HTTP客户端超时
	invoker.httpClient.Timeout = invoker.timeout

	return invoker
}

// Call 调用服务的指定API
func (i *ServiceInvoker) Call(method, path string, headers map[string]string, body []byte) (*http.Response, error) {
	// 获取健康的服务实例
	services, err := i.client.GetHealthyServices(i.serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service instances: %v", err)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no healthy service instances found for %s", i.serviceName)
	}

	// 根据标签过滤服务实例
	if len(i.tags) > 0 {
		var filtered []*api.ServiceEntry
		for _, service := range services {
			if containsAll(service.Service.Tags, i.tags) {
				filtered = append(filtered, service)
			}
		}
		services = filtered
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no service instances found matching tags for %s", i.serviceName)
	}

	// 选择服务实例
	var selectedService *api.ServiceEntry
	switch i.strategy {
	case Random:
		selectedService = services[rand.Intn(len(services))]
	case RoundRobin:
		selectedService = services[i.currentIndex%len(services)]
		i.currentIndex++
	case LeastConn:
		// 这里可以实现最少连接数的选择逻辑
		// 需要维护每个实例的连接数统计
		selectedService = services[0]
	}

	// 构建请求URL
	url := fmt.Sprintf("http://%s:%d%s",
		selectedService.Service.Address,
		selectedService.Service.Port,
		path)

	// 创建请求
	req, err := http.NewRequest(method, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// 添加请求头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 执行请求（带重试）
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt <= i.retryCount; attempt++ {
		resp, err = i.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		if attempt < i.retryCount {
			time.Sleep(i.retryInterval)
			i.client.logger.Printf("Retry attempt %d for service %s: %v", attempt+1, i.serviceName, err)
		}
	}

	return nil, fmt.Errorf("service call failed after %d attempts: %v", i.retryCount+1, lastErr)
}

// CallJSON 调用服务的JSON API
func (i *ServiceInvoker) CallJSON(method, path string, headers map[string]string, requestBody interface{}, responseBody interface{}) error {
	// 将请求体序列化为JSON
	var bodyBytes []byte
	var err error
	if requestBody != nil {
		bodyBytes, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}
	}

	// 设置JSON请求头
	if headers == nil {
		headers = make(map[string]string)
	}
	headers["Content-Type"] = "application/json"
	headers["Accept"] = "application/json"

	// 发送请求
	resp, err := i.Call(method, path, headers, bodyBytes)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("service returned error status: %s", resp.Status)
	}

	// 解析响应体
	if responseBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
			return fmt.Errorf("failed to decode response body: %v", err)
		}
	}

	return nil
}

// 辅助函数：检查数组是否包含所有指定的标签
func containsAll(array []string, items []string) bool {
	for _, item := range items {
		found := false
		for _, element := range array {
			if element == item {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
