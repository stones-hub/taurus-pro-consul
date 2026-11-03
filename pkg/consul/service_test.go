package consul_test

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	consul "github.com/stones-hub/taurus-pro-consul/pkg/consul"
)

const (
	deregisterPrefix = "/v1/agent/service/deregister/"
	testToken        = "test-acl-token"
	// 阻塞类用例里"应当很快返回"的上限，远小于服务端阻塞时长
	returnBound = 3 * time.Second
)

// fakeAgent 是一个真实 HTTP 服务端，模拟 Consul agent 的相关接口：
//   - /v1/health/state/any: NewClient 的连通性检查
//   - PUT /v1/agent/service/deregister/<id>:
//     "exists" 返回 200，"block" 一直阻塞到请求被取消，"boom" 返回 500，其他返回 404
//   - GET /v1/kv/slow: 延迟 slowDelay 后返回，用于确认没有全局 http.Client.Timeout
type fakeAgent struct {
	srv       *httptest.Server
	slowDelay time.Duration

	entered   chan struct{} // 阻塞请求进入 handler
	cancelled chan struct{} // 阻塞请求的服务端 ctx 被取消（即客户端中止了真实请求）
	release   chan struct{} // 测试结束时释放仍在阻塞的 handler

	mu          sync.Mutex
	deregisters []recordedRequest
}

type recordedRequest struct {
	method string
	path   string
	token  string
	query  string
}

func newFakeAgent(t *testing.T) *fakeAgent {
	t.Helper()
	f := &fakeAgent{
		slowDelay: 500 * time.Millisecond,
		entered:   make(chan struct{}, 16),
		cancelled: make(chan struct{}, 16),
		release:   make(chan struct{}),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	// Cleanup 按 LIFO 执行：先释放阻塞的 handler，再关闭服务端
	t.Cleanup(f.srv.Close)
	t.Cleanup(func() { close(f.release) })
	return f
}

func (f *fakeAgent) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/health/state/any":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	case r.URL.Path == "/v1/kv/slow":
		select {
		case <-time.After(f.slowDelay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Consul-Index", "1")
		_, _ = io.WriteString(w, `[{"Key":"slow","Value":"b2s=","ModifyIndex":1}]`)
	case strings.HasPrefix(r.URL.Path, deregisterPrefix):
		id := strings.TrimPrefix(r.URL.Path, deregisterPrefix)
		f.mu.Lock()
		f.deregisters = append(f.deregisters, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			token:  r.Header.Get("X-Consul-Token"),
			query:  r.URL.RawQuery,
		})
		f.mu.Unlock()
		switch id {
		case "exists":
			w.WriteHeader(http.StatusOK)
		case "block":
			f.entered <- struct{}{}
			select {
			case <-r.Context().Done():
				f.cancelled <- struct{}{}
			case <-f.release:
			}
		case "boom":
			http.Error(w, "rpc error: agent unavailable", http.StatusInternalServerError)
		default:
			http.Error(w, `Unknown service ID "`+id+`". Ensure that the service ID is passed, not the service name.`, http.StatusNotFound)
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAgent) deregisterCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deregisters)
}

func (f *fakeAgent) lastDeregister(t *testing.T) recordedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deregisters) == 0 {
		t.Fatal("no deregister request received")
	}
	return f.deregisters[len(f.deregisters)-1]
}

func (f *fakeAgent) newClient(t *testing.T, opts ...consul.Option) *consul.Client {
	t.Helper()
	base := []consul.Option{
		consul.WithAddress(strings.TrimPrefix(f.srv.URL, "http://")),
		consul.WithToken(testToken),
		consul.WithMaxRetries(0),
		consul.WithLogger(log.New(io.Discard, "", 0)),
	}
	c, err := consul.NewClient(append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(returnBound):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// callAsync 在后台执行 fn，返回结果通道和开始时间
func callAsync(fn func() error) (<-chan error, time.Time) {
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- fn() }()
	return done, start
}

func waitResult(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(returnBound):
		t.Fatalf("%s did not return within %v", what, returnBound)
		return nil
	}
}

func TestDeregisterServiceSuccess(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t, consul.WithDatacenter("dc-test"))

	calls := map[string]func() error{
		"DeregisterService": func() error { return c.DeregisterService("exists") },
		"DeregisterServiceContext": func() error {
			return c.DeregisterServiceContext(context.Background(), "exists")
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := f.lastDeregister(t)
			if got.method != http.MethodPut {
				t.Errorf("method = %q, want PUT", got.method)
			}
			if got.path != deregisterPrefix+"exists" {
				t.Errorf("path = %q", got.path)
			}
			if got.token != testToken {
				t.Errorf("X-Consul-Token = %q, want client token", got.token)
			}
			if !strings.Contains(got.query, "dc=dc-test") {
				t.Errorf("query = %q, want client datacenter", got.query)
			}
		})
	}
}

func TestDeregisterServiceEmptyID(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t)

	for name, err := range map[string]error{
		"DeregisterService":        c.DeregisterService(""),
		"DeregisterServiceContext": c.DeregisterServiceContext(context.Background(), ""),
	} {
		if err == nil || err.Error() != "service ID cannot be empty" {
			t.Errorf("%s: err = %v, want %q", name, err, "service ID cannot be empty")
		}
	}
	if n := f.deregisterCount(); n != 0 {
		t.Errorf("empty ID sent %d requests, want 0", n)
	}
}

func TestDeregisterServiceErrorStatus(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t)

	cases := []struct {
		id   string
		code int
		msg  string
	}{
		{
			id:   "missing",
			code: http.StatusNotFound,
			msg:  `failed to deregister service: Unexpected response code: 404 (Unknown service ID "missing". Ensure that the service ID is passed, not the service name.)`,
		},
		{
			id:   "boom",
			code: http.StatusInternalServerError,
			msg:  "failed to deregister service: Unexpected response code: 500 (rpc error: agent unavailable)",
		},
	}
	for _, tc := range cases {
		calls := map[string]func() error{
			"DeregisterService": func() error { return c.DeregisterService(tc.id) },
			"DeregisterServiceContext": func() error {
				return c.DeregisterServiceContext(context.Background(), tc.id)
			},
		}
		for name, call := range calls {
			t.Run(tc.id+"/"+name, func(t *testing.T) {
				err := call()
				if err == nil {
					t.Fatal("expected error")
				}
				// 错误文本与旧版 fmt.Errorf("...: %v") 完全一致
				if err.Error() != tc.msg {
					t.Errorf("err = %q\nwant  %q", err.Error(), tc.msg)
				}
				var se api.StatusError
				if !errors.As(err, &se) || se.Code != tc.code {
					t.Errorf("errors.As StatusError = %+v, want code %d", se, tc.code)
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("status error must not look like a context error: %v", err)
				}
			})
		}
	}
}

func TestDeregisterServiceContextCancel(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, _ := callAsync(func() error { return c.DeregisterServiceContext(ctx, "block") })

	waitSignal(t, f.entered, "request to reach server")
	cancel()

	err := waitResult(t, done, "DeregisterServiceContext after cancel")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !strings.HasPrefix(err.Error(), "failed to deregister service: ") {
		t.Errorf("err = %q, want original prefix", err.Error())
	}
	if strings.Contains(err.Error(), "client closed") {
		t.Errorf("caller cancel must not be reported as client closed: %v", err)
	}
	// 服务端观察到连接被中止，说明取消的是底层 HTTP 请求，而不只是放弃等待
	waitSignal(t, f.cancelled, "server to observe cancellation")
}

func TestDeregisterServiceContextDeadline(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done, start := callAsync(func() error { return c.DeregisterServiceContext(ctx, "block") })

	waitSignal(t, f.entered, "request to reach server")
	err := waitResult(t, done, "DeregisterServiceContext with deadline")
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("returned after %v, before deadline", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	waitSignal(t, f.cancelled, "server to observe cancellation")
}

func TestDeregisterServiceConfiguredTimeout(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t, consul.WithTimeout(200*time.Millisecond))

	done, start := callAsync(func() error { return c.DeregisterService("block") })

	waitSignal(t, f.entered, "request to reach server")
	err := waitResult(t, done, "DeregisterService with WithTimeout")
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("returned after %v, before configured timeout", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	waitSignal(t, f.cancelled, "server to observe cancellation")
}

func TestDeregisterServiceCloseCancelsInFlight(t *testing.T) {
	cases := map[string]func(c *consul.Client) error{
		// WithTimeout(0) 不限时，只有 Close 能让旧 API 返回
		"DeregisterService": func(c *consul.Client) error { return c.DeregisterService("block") },
		"DeregisterServiceContext": func(c *consul.Client) error {
			return c.DeregisterServiceContext(context.Background(), "block")
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAgent(t)
			c := f.newClient(t, consul.WithTimeout(0))

			done, _ := callAsync(func() error { return call(c) })
			waitSignal(t, f.entered, "request to reach server")
			if err := c.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			err := waitResult(t, done, name+" after Close")
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if !strings.Contains(err.Error(), "consul client closed") {
				t.Errorf("err = %q, want client closed hint", err.Error())
			}
			waitSignal(t, f.cancelled, "server to observe cancellation")
		})
	}
}

func TestDeregisterServiceAfterClose(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for name, err := range map[string]error{
		"DeregisterService":        c.DeregisterService("exists"),
		"DeregisterServiceContext": c.DeregisterServiceContext(context.Background(), "exists"),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want context.Canceled", name, err)
		}
	}
	if n := f.deregisterCount(); n != 0 {
		t.Errorf("closed client sent %d requests, want 0", n)
	}
	// 空 ID 校验仍优先于关闭检查，与旧版行为一致
	if err := c.DeregisterService(""); err == nil || err.Error() != "service ID cannot be empty" {
		t.Errorf("empty ID after Close: err = %v", err)
	}
}

func TestDeregisterServiceContextNilContext(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t)

	var nilCtx context.Context
	if err := c.DeregisterServiceContext(nilCtx, "exists"); err != nil {
		t.Fatalf("nil ctx: unexpected error: %v", err)
	}
}

// WithTimeout 只约束注销，不能变成全局 http.Client.Timeout，否则会打断 watch 的阻塞查询
func TestWithTimeoutDoesNotLimitOtherRequests(t *testing.T) {
	f := newFakeAgent(t)
	c := f.newClient(t, consul.WithTimeout(100*time.Millisecond))

	start := time.Now()
	val, err := c.Get("slow")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "ok" {
		t.Errorf("value = %q, want ok", val)
	}
	if elapsed := time.Since(start); elapsed < f.slowDelay {
		t.Errorf("Get returned after %v, expected server delay %v", elapsed, f.slowDelay)
	}
}
