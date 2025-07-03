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

// 用户服务配置
type UserConfig struct {
	Database struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"database"`
	Features map[string]bool `json:"features"`
}

// 支付服务配置
type PaymentConfig struct {
	Providers map[string]struct {
		Endpoint string `json:"endpoint"`
		ApiKey   string `json:"api_key"`
		Timeout  int    `json:"timeout"`
	} `json:"providers"`
	DefaultProvider string `json:"default_provider"`
}

// 订单服务配置
type OrderConfig struct {
	OrderPrefix   string `json:"order_prefix"`
	ExpireMinutes int    `json:"expire_minutes"`
	Notifications struct {
		Email bool `json:"email"`
		SMS   bool `json:"sms"`
	} `json:"notifications"`
}

// 用户服务
type UserService struct {
	config  *UserConfig
	logger  *log.Logger
	client  *consul.Client
	address string
	port    int
}

// 支付服务
type PaymentService struct {
	config      *PaymentConfig
	logger      *log.Logger
	client      *consul.Client
	address     string
	port        int
	userInvoker *consul.ServiceInvoker
}

// 订单服务
type OrderService struct {
	config         *OrderConfig
	logger         *log.Logger
	client         *consul.Client
	address        string
	port           int
	userInvoker    *consul.ServiceInvoker
	paymentInvoker *consul.ServiceInvoker
}

// 启动用户服务
func startUserService(wg *sync.WaitGroup, client *consul.Client, logger *log.Logger, ctx context.Context) {
	defer wg.Done()

	userService := &UserService{
		config:  &UserConfig{},
		logger:  log.New(os.Stdout, "[USER-SERVICE] ", log.LstdFlags),
		client:  client,
		address: "192.168.40.30",
		port:    8081,
	}

	// 初始化用户服务配置
	initialConfig := UserConfig{}
	initialConfig.Database.Host = "localhost"
	initialConfig.Database.Port = 5432
	initialConfig.Database.Username = "user_service"
	initialConfig.Database.Password = "password123"
	initialConfig.Features = map[string]bool{
		"email_verification": true,
		"sms_notification":   false,
		"oauth_login":        true,
	}

	// 写入初始配置
	configBytes, _ := json.Marshal(initialConfig)
	if err := client.Put("config/user-service", configBytes); err != nil {
		userService.logger.Printf("Failed to write initial config: %v", err)
		return
	}

	// 注册服务
	err := client.RegisterService(&consul.ServiceConfig{
		Name:    "user-service",
		ID:      "user-service-1",
		Address: userService.address,
		Port:    userService.port,
		Tags:    []string{"api", "v1", "user"},
		Meta: map[string]string{
			"version": "1.0.0",
			"env":     "dev",
		},
		Checks: []*consul.CheckConfig{
			{
				HTTP:            fmt.Sprintf("http://%s:%d/health", userService.address, userService.port),
				Interval:        time.Second * 10,
				Timeout:         time.Second * 5,
				DeregisterAfter: time.Minute,
			},
		},
	})
	if err != nil {
		userService.logger.Printf("Failed to register service: %v", err)
		return
	}

	// 监听配置变更
	err = client.WatchConfig("config/user-service", userService.config, &consul.WatchOptions{
		WaitTime:  time.Second * 10,
		RetryTime: time.Second,
	})
	if err != nil {
		userService.logger.Printf("Failed to watch config: %v", err)
		return
	}

	// HTTP 处理函数
	mux := http.NewServeMux()
	mux.HandleFunc("/users/verify", func(w http.ResponseWriter, r *http.Request) {
		if !userService.config.Features["email_verification"] {
			http.Error(w, "Email verification is disabled", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"verified": true})
	})

	mux.HandleFunc("/users/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       r.URL.Query().Get("id"),
			"name":     "Test User",
			"features": userService.config.Features,
		})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// HTTP 服务器
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", userService.port),
		Handler: mux,
	}

	// 在独立的 goroutine 中启动服务器
	go func() {
		userService.logger.Printf("Starting user service on port %d", userService.port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			userService.logger.Printf("User service error: %v", err)
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	userService.logger.Println("Shutting down user service...")

	// 优雅关闭服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		userService.logger.Printf("User service forced to shutdown: %v", err)
	}
}

// 启动支付服务
func startPaymentService(wg *sync.WaitGroup, client *consul.Client, logger *log.Logger, ctx context.Context) {
	defer wg.Done()

	paymentService := &PaymentService{
		config:  &PaymentConfig{},
		logger:  log.New(os.Stdout, "[PAYMENT-SERVICE] ", log.LstdFlags),
		client:  client,
		address: "192.168.40.30",
		port:    8082,
	}

	// 初始化支付服务配置
	initialConfig := PaymentConfig{
		Providers: map[string]struct {
			Endpoint string `json:"endpoint"`
			ApiKey   string `json:"api_key"`
			Timeout  int    `json:"timeout"`
		}{
			"stripe": {
				Endpoint: "https://api.stripe.com/v1",
				ApiKey:   "sk_test_123",
				Timeout:  30,
			},
			"paypal": {
				Endpoint: "https://api.paypal.com/v1",
				ApiKey:   "pk_test_456",
				Timeout:  30,
			},
		},
		DefaultProvider: "stripe",
	}

	// 写入初始配置
	configBytes, _ := json.Marshal(initialConfig)
	if err := client.Put("config/payment-service", configBytes); err != nil {
		paymentService.logger.Printf("Failed to write initial config: %v", err)
		return
	}

	// 注册服务
	err := client.RegisterService(&consul.ServiceConfig{
		Name:    "payment-service",
		ID:      "payment-service-1",
		Address: paymentService.address,
		Port:    paymentService.port,
		Tags:    []string{"api", "v1", "payment"},
		Meta: map[string]string{
			"version": "1.0.0",
			"env":     "dev",
		},
		Checks: []*consul.CheckConfig{
			{
				HTTP:            fmt.Sprintf("http://%s:%d/health", paymentService.address, paymentService.port),
				Interval:        time.Second * 10,
				Timeout:         time.Second * 5,
				DeregisterAfter: time.Minute,
			},
		},
	})
	if err != nil {
		paymentService.logger.Printf("Failed to register service: %v", err)
		return
	}

	// 监听配置变更
	err = client.WatchConfig("config/payment-service", paymentService.config, &consul.WatchOptions{
		WaitTime:  time.Second * 10,
		RetryTime: time.Second,
	})
	if err != nil {
		paymentService.logger.Printf("Failed to watch config: %v", err)
		return
	}

	// 创建用户服务调用器
	paymentService.userInvoker = client.NewServiceInvoker("user-service",
		consul.WithStrategy(consul.RoundRobin),
		consul.WithInvokeTimeout(time.Second*5),
		consul.WithRetry(3, time.Second),
		consul.WithTags([]string{"api", "v1"}),
	)

	// HTTP 处理函数
	mux := http.NewServeMux()
	mux.HandleFunc("/payments/process", func(w http.ResponseWriter, r *http.Request) {
		// 调用用户服务验证用户
		var userResponse map[string]interface{}
		err := paymentService.userInvoker.CallJSON(
			"GET",
			fmt.Sprintf("/users/info?id=%s", r.URL.Query().Get("user_id")),
			nil,
			nil,
			&userResponse,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to verify user: %v", err), http.StatusInternalServerError)
			return
		}

		// 获取当前配置的支付提供商
		provider := paymentService.config.Providers[paymentService.config.DefaultProvider]

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"payment_id":    "pay_123",
			"user":          userResponse,
			"provider":      paymentService.config.DefaultProvider,
			"provider_info": provider,
			"status":        "processed",
		})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// HTTP 服务器
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", paymentService.port),
		Handler: mux,
	}

	// 在独立的 goroutine 中启动服务器
	go func() {
		paymentService.logger.Printf("Starting payment service on port %d", paymentService.port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			paymentService.logger.Printf("Payment service error: %v", err)
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	paymentService.logger.Println("Shutting down payment service...")

	// 优雅关闭服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		paymentService.logger.Printf("Payment service forced to shutdown: %v", err)
	}
}

// 启动订单服务
func startOrderService(wg *sync.WaitGroup, client *consul.Client, logger *log.Logger, ctx context.Context) {
	defer wg.Done()

	orderService := &OrderService{
		config:  &OrderConfig{},
		logger:  log.New(os.Stdout, "[ORDER-SERVICE] ", log.LstdFlags),
		client:  client,
		address: "192.168.40.30",
		port:    8083,
	}

	// 初始化订单服务配置
	initialConfig := OrderConfig{
		OrderPrefix:   "ORD",
		ExpireMinutes: 30,
		Notifications: struct {
			Email bool `json:"email"`
			SMS   bool `json:"sms"`
		}{
			Email: true,
			SMS:   false,
		},
	}

	// 写入初始配置
	configBytes, _ := json.Marshal(initialConfig)
	if err := client.Put("config/order-service", configBytes); err != nil {
		orderService.logger.Printf("Failed to write initial config: %v", err)
		return
	}

	// 注册服务
	err := client.RegisterService(&consul.ServiceConfig{
		Name:    "order-service",
		ID:      "order-service-1",
		Address: orderService.address,
		Port:    orderService.port,
		Tags:    []string{"api", "v1", "order"},
		Meta: map[string]string{
			"version": "1.0.0",
			"env":     "dev",
		},
		Checks: []*consul.CheckConfig{
			{
				HTTP:            fmt.Sprintf("http://%s:%d/health", orderService.address, orderService.port),
				Interval:        time.Second * 10,
				Timeout:         time.Second * 5,
				DeregisterAfter: time.Minute,
			},
		},
	})
	if err != nil {
		orderService.logger.Printf("Failed to register service: %v", err)
		return
	}

	// 监听配置变更
	err = client.WatchConfig("config/order-service", orderService.config, &consul.WatchOptions{
		WaitTime:  time.Second * 10,
		RetryTime: time.Second,
	})
	if err != nil {
		orderService.logger.Printf("Failed to watch config: %v", err)
		return
	}

	// 创建用户服务调用器
	orderService.userInvoker = client.NewServiceInvoker("user-service",
		consul.WithStrategy(consul.RoundRobin),
		consul.WithInvokeTimeout(time.Second*5),
		consul.WithRetry(3, time.Second),
		consul.WithTags([]string{"api", "v1"}),
	)

	// 创建支付服务调用器
	orderService.paymentInvoker = client.NewServiceInvoker("payment-service",
		consul.WithStrategy(consul.RoundRobin),
		consul.WithInvokeTimeout(time.Second*5),
		consul.WithRetry(3, time.Second),
		consul.WithTags([]string{"api", "v1"}),
	)

	// HTTP 处理函数
	mux := http.NewServeMux()
	mux.HandleFunc("/orders/create", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user_id")
		amount := r.URL.Query().Get("amount")

		// 1. 调用用户服务验证用户
		var userResponse map[string]interface{}
		err := orderService.userInvoker.CallJSON(
			"GET",
			fmt.Sprintf("/users/info?id=%s", userID),
			nil,
			nil,
			&userResponse,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to verify user: %v", err), http.StatusInternalServerError)
			return
		}

		// 2. 创建订单号
		orderID := fmt.Sprintf("%s-%s-%d",
			orderService.config.OrderPrefix,
			userID,
			time.Now().Unix(),
		)

		// 3. 调用支付服务处理支付
		var paymentResponse map[string]interface{}
		err = orderService.paymentInvoker.CallJSON(
			"POST",
			fmt.Sprintf("/payments/process?user_id=%s&order_id=%s&amount=%s",
				userID, orderID, amount),
			nil,
			nil,
			&paymentResponse,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to process payment: %v", err), http.StatusInternalServerError)
			return
		}

		// 4. 返回完整的订单信息
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"order_id":  orderID,
			"user":      userResponse,
			"payment":   paymentResponse,
			"status":    "created",
			"expire_in": fmt.Sprintf("%d minutes", orderService.config.ExpireMinutes),
		})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// HTTP 服务器
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", orderService.port),
		Handler: mux,
	}

	// 在独立的 goroutine 中启动服务器
	go func() {
		orderService.logger.Printf("Starting order service on port %d", orderService.port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			orderService.logger.Printf("Order service error: %v", err)
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	orderService.logger.Println("Shutting down order service...")

	// 优雅关闭服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		orderService.logger.Printf("Order service forced to shutdown: %v", err)
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
	wg.Add(3)

	// 启动用户服务
	go startUserService(&wg, client, logger, ctx)

	// 启动支付服务
	go startPaymentService(&wg, client, logger, ctx)

	// 启动订单服务
	go startOrderService(&wg, client, logger, ctx)

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Println("Shutting down services...")

	// 取消上下文，触发所有服务关闭
	cancel()

	// 注销所有服务
	if err := client.DeregisterService("user-service-1"); err != nil {
		logger.Printf("Error deregistering user service: %v", err)
	}
	if err := client.DeregisterService("payment-service-1"); err != nil {
		logger.Printf("Error deregistering payment service: %v", err)
	}
	if err := client.DeregisterService("order-service-1"); err != nil {
		logger.Printf("Error deregistering order service: %v", err)
	}

	// 等待所有服务完全停止
	wg.Wait()
	logger.Println("All services stopped gracefully")
}
