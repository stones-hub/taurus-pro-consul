package consul_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/consul/api"
	consul "github.com/stones-hub/taurus-pro-consul/pkg/consul"
)

// reliabilityAgent 模拟 Consul agent 的 /v1/health/service/<name> 与 /v1/agent/checks，
// 记录健康服务查询次数，用于确认 GetHealthChecks 不再走按服务名查询的旧路径。
type reliabilityAgent struct {
	srv *httptest.Server

	mu       sync.Mutex
	services map[string][]*api.ServiceEntry
	checks   map[string]*api.AgentCheck
	failures bool

	serviceQueries atomic.Int64
}

func newReliabilityAgent(t *testing.T) *reliabilityAgent {
	t.Helper()
	a := &reliabilityAgent{
		services: map[string][]*api.ServiceEntry{},
		checks:   map[string]*api.AgentCheck{},
	}
	a.srv = httptest.NewServer(http.HandlerFunc(a.handle))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *reliabilityAgent) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case r.URL.Path == "/v1/health/state/any":
		_, _ = io.WriteString(w, "[]")
	case strings.HasPrefix(r.URL.Path, "/v1/health/service/"):
		a.serviceQueries.Add(1)
		name := strings.TrimPrefix(r.URL.Path, "/v1/health/service/")
		_ = json.NewEncoder(w).Encode(a.services[name])
	case r.URL.Path == "/v1/agent/checks":
		if a.failures {
			http.Error(w, "agent unavailable", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(a.checks)
	default:
		http.NotFound(w, r)
	}
}

func (a *reliabilityAgent) newClient(t *testing.T) *consul.Client {
	t.Helper()
	c, err := consul.NewClient(
		consul.WithAddress(strings.TrimPrefix(a.srv.URL, "http://")),
		consul.WithMaxRetries(0),
		consul.WithLogger(log.New(io.Discard, "", 0)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// addBackend 启动一个返回自身标识的真实 HTTP 服务，并把它登记为 name 的健康实例。
func (a *reliabilityAgent) addBackend(t *testing.T, name, id string, hits *atomic.Int64) {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, id)
	}))
	t.Cleanup(backend.Close)
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	a.mu.Lock()
	a.services[name] = append(a.services[name], &api.ServiceEntry{
		Service: &api.AgentService{ID: id, Service: name, Address: host, Port: port},
	})
	a.mu.Unlock()
}

func (a *reliabilityAgent) setCheck(id, serviceID, serviceName, status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checks[id] = &api.AgentCheck{
		Node: "node-1", CheckID: id, Name: id, Status: status,
		ServiceID: serviceID, ServiceName: serviceName, Type: "http",
	}
}

func callBody(t *testing.T, inv *consul.ServiceInvoker) string {
	t.Helper()
	resp, err := inv.Call(http.MethodGet, "/", nil, nil)
	if err != nil {
		t.Errorf("Call: %v", err)
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestRoundRobinSequentialOrder(t *testing.T) {
	a := newReliabilityAgent(t)
	var hits [3]atomic.Int64
	for i := range hits {
		a.addBackend(t, "web", fmt.Sprintf("web-%d", i), &hits[i])
	}
	inv := a.newClient(t).NewServiceInvoker("web", consul.WithStrategy(consul.RoundRobin), consul.WithRetry(0, 0))

	want := []string{"web-0", "web-1", "web-2", "web-0", "web-1", "web-2", "web-0"}
	for i, w := range want {
		if got := callBody(t, inv); got != w {
			t.Fatalf("call %d = %q, want %q", i, got, w)
		}
	}
}

// 12 个 goroutine 共享同一个默认（RoundRobin）invoker；-race 下旧实现在 currentIndex 上报数据竞争。
func TestRoundRobinConcurrentSharedInvoker(t *testing.T) {
	a := newReliabilityAgent(t)
	var hits [3]atomic.Int64
	for i := range hits {
		a.addBackend(t, "web", fmt.Sprintf("web-%d", i), &hits[i])
	}
	inv := a.newClient(t).NewServiceInvoker("web", consul.WithRetry(0, 0))

	const goroutines, perGoroutine = 12, 30
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < perGoroutine; n++ {
				callBody(t, inv)
			}
		}()
	}
	wg.Wait()

	// 原子计数保证 360 次调用在 3 个实例上严格均分
	for i := range hits {
		if got := hits[i].Load(); got != goroutines*perGoroutine/3 {
			t.Errorf("web-%d hits = %d, want %d", i, got, goroutines*perGoroutine/3)
		}
	}
}

func TestGetHealthChecksByExactInstanceID(t *testing.T) {
	a := newReliabilityAgent(t)
	// 实例 ID 与服务名不同；同一实例有多个状态的检查；另有同名服务的其他实例和无关服务
	a.setCheck("svc-a:1", "inst-a", "web", api.HealthPassing)
	a.setCheck("svc-a:2", "inst-a", "web", api.HealthWarning)
	a.setCheck("svc-a:3", "inst-a", "web", api.HealthCritical)
	a.setCheck("svc-b:1", "inst-b", "web", api.HealthCritical)
	a.setCheck("inst-a-extra:1", "inst-a-extra", "other", api.HealthPassing) // 前缀相似，不能命中
	a.setCheck("serf", "", "", api.HealthPassing)                            // 节点级检查
	c := a.newClient(t)

	got, err := c.GetHealthChecks("inst-a")
	if err != nil {
		t.Fatalf("GetHealthChecks: %v", err)
	}
	wantStatus := []string{api.HealthPassing, api.HealthWarning, api.HealthCritical}
	if len(got) != len(wantStatus) {
		t.Fatalf("got %d checks, want %d: %+v", len(got), len(wantStatus), got)
	}
	for i, ch := range got {
		if ch.ServiceID != "inst-a" || ch.ServiceName != "web" {
			t.Errorf("check %d belongs to %q/%q", i, ch.ServiceID, ch.ServiceName)
		}
		if ch.Status != wantStatus[i] {
			t.Errorf("check %d (%s) status = %q, want %q", i, ch.CheckID, ch.Status, wantStatus[i])
		}
	}
	if n := a.serviceQueries.Load(); n != 0 {
		t.Errorf("made %d health-by-service-name queries, want 0", n)
	}

	// 用服务名查询不再命中任何实例
	byName, err := c.GetHealthChecks("web")
	if err != nil || len(byName) != 0 {
		t.Errorf("by service name: checks=%v err=%v, want empty", byName, err)
	}
}

func TestGetHealthChecksOnlyCritical(t *testing.T) {
	a := newReliabilityAgent(t)
	a.setCheck("c1", "inst-x", "web", api.HealthCritical)
	got, err := a.newClient(t).GetHealthChecks("inst-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != api.HealthCritical {
		t.Fatalf("got %+v, want the single critical check", got)
	}
}

func TestGetHealthChecksEmptyID(t *testing.T) {
	a := newReliabilityAgent(t)
	_, err := a.newClient(t).GetHealthChecks("")
	if err == nil || err.Error() != "service ID cannot be empty" {
		t.Fatalf("err = %v", err)
	}
}

func TestGetHealthChecksUnknownID(t *testing.T) {
	a := newReliabilityAgent(t)
	a.setCheck("c1", "inst-a", "web", api.HealthPassing)
	got, err := a.newClient(t).GetHealthChecks("nope")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestGetHealthChecksAgentError(t *testing.T) {
	a := newReliabilityAgent(t)
	a.failures = true
	_, err := a.newClient(t).GetHealthChecks("inst-a")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.HasPrefix(err.Error(), "failed to get health checks: ") || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %q", err)
	}
}
