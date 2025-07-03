package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	consul "github.com/yelei-cn/taurus-pro-consul/pkg/consul"
)

// 启动单个用户服务实例
func startUserInstance(wg *sync.WaitGroup, client *consul.Client, logger *log.Logger, ctx context.Context, port int) {
	defer wg.Done()

	serviceID := fmt.Sprintf("user-service-%d", port)
	instanceLogger := log.New(os.Stdout, fmt.Sprintf("[USER-%d] ", port), log.LstdFlags)

	// 注册服务
	err := client.RegisterService(&consul.ServiceConfig{
		Name:    "user-service",
		ID:      serviceID,
		Address: "192.168.40.30",
		Port:    port,
		Tags:    []string{"api", "v1", "user"},
		Meta: map[string]string{
			"version": "1.0.0",
			"env":     "dev",
			"port":    fmt.Sprintf("%d", port),
		},
		Checks: []*consul.CheckConfig{
			{
				HTTP:            fmt.Sprintf("http://192.168.40.30:%d/health", port),
				Interval:        time.Second * 10,
				Timeout:         time.Second * 5,
				DeregisterAfter: time.Minute,
			},
		},
	})
	if err != nil {
		logger.Printf("Failed to register service %s: %v", serviceID, err)
		return
	}

	// HTTP 处理函数
	mux := http.NewServeMux()

	// 健康检查端点
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// 用户信息端点
	mux.HandleFunc("/users/info", func(w http.ResponseWriter, r *http.Request) {
		instanceLogger.Printf("Handling request from instance %s", serviceID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":        r.URL.Query().Get("id"),
			"name":      "Test User",
			"instance":  serviceID,
			"timestamp": time.Now(),
			"features": map[string]bool{
				"email_verification": true,
				"sms_notification":   false,
				"oauth_login":        true,
			},
		})
	})

	// HTTP 服务器
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// 在独立的 goroutine 中启动服务器
	go func() {
		instanceLogger.Printf("Starting user service instance on port %d", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			instanceLogger.Printf("User service error: %v", err)
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	instanceLogger.Printf("Shutting down user service instance on port %d", port)

	// 优雅关闭服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 注销服务
	if err := client.DeregisterService(serviceID); err != nil {
		instanceLogger.Printf("Error deregistering service %s: %v", serviceID, err)
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		instanceLogger.Printf("User service instance on port %d forced to shutdown: %v", port, err)
	}
}

// 启动支付服务（用于测试负载均衡）
func startPaymentTester(wg *sync.WaitGroup, client *consul.Client, logger *log.Logger, ctx context.Context) {
	defer wg.Done()

	// 创建服务调用器（使用轮询策略）
	userInvoker := client.NewServiceInvoker("user-service",
		consul.WithStrategy(consul.RoundRobin),
		consul.WithInvokeTimeout(time.Second*5),
		consul.WithRetry(3, time.Second),
		consul.WithTags([]string{"api", "v1"}),
	)

	// HTTP 处理函数
	mux := http.NewServeMux()

	// 测试端点
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		var responses []map[string]interface{}
		count := 10 // 每次测试发送10个请求

		for i := 0; i < count; i++ {
			var response map[string]interface{}
			err := userInvoker.CallJSON(
				"GET",
				"/users/info?id=123",
				nil,
				nil,
				&response,
			)
			if err != nil {
				logger.Printf("Error calling user service: %v", err)
				http.Error(w, fmt.Sprintf("Error calling user service: %v", err), http.StatusInternalServerError)
				return
			}
			responses = append(responses, response)
			time.Sleep(time.Millisecond * 100) // 稍微延迟，便于观察
		}

		// 统计每个实例处理的请求数
		instanceStats := make(map[string]int)
		for _, resp := range responses {
			if instance, ok := resp["instance"].(string); ok {
				instanceStats[instance]++
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"test_time": time.Now(),
			"stats":     instanceStats,
			"requests":  responses,
		})
	})

	// HTTP 服务器
	server := &http.Server{
		Addr:    ":8090",
		Handler: mux,
	}

	// 在独立的 goroutine 中启动服务器
	go func() {
		logger.Printf("Starting payment tester service on port 8090")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Printf("Payment tester service error: %v", err)
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	logger.Println("Shutting down payment tester service...")

	// 优雅关闭服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("Payment tester service forced to shutdown: %v", err)
	}
}

func main() {
	logger := log.New(os.Stdout, "[MAIN] ", log.LstdFlags)

	// 创建 Consul 客户端
	client, err := consul.NewClient(
		consul.WithAddress("192.168.3.240:8500"),
		consul.WithLogger(logger),
		consul.WithTimeout(time.Second*5),
		consul.WithRetryTime(time.Second),
		consul.WithMaxRetries(3),
	)
	if err != nil {
		logger.Fatalf("Failed to create consul client: %v", err)
	}

	// 创建上下文和取消函数
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建等待组
	var wg sync.WaitGroup

	// 启动3个用户服务实例
	userPorts := []int{8081, 8082, 8083}
	wg.Add(len(userPorts) + 1) // +1 是为了支付测试服务

	// 启动用户服务实例
	for _, port := range userPorts {
		go startUserInstance(&wg, client, logger, ctx, port)
	}

	// 等待几秒钟，确保所有用户服务实例都已启动
	time.Sleep(time.Second * 2)

	// 启动支付测试服务
	go startPaymentTester(&wg, client, logger, ctx)

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Println("Shutting down all services...")

	// 取消上下文，触发所有服务关闭
	cancel()

	// 等待所有服务完全停止
	wg.Wait()
	logger.Println("All services stopped gracefully")
}
