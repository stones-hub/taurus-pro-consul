package consul_test

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	consul "github.com/stones-hub/taurus-pro-consul/pkg/consul"
)

// 真实 Consul 集成测试，默认跳过，只有显式设置 CONSUL_INTEGRATION_ADDR（如 127.0.0.1:18500）才会连接。
const integrationEnv = "CONSUL_INTEGRATION_ADDR"

const integrationWait = 10 * time.Second

func integrationClients(t *testing.T) (*consul.Client, *api.Client) {
	t.Helper()
	addr := os.Getenv(integrationEnv)
	if addr == "" {
		t.Skipf("set %s to run real Consul integration tests", integrationEnv)
	}
	c, err := consul.NewClient(consul.WithAddress(addr), consul.WithMaxRetries(0), consul.WithLogger(log.New(io.Discard, "", 0)))
	if err != nil {
		t.Fatalf("NewClient(%s): %v", addr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	raw, err := api.NewClient(&api.Config{Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	return c, raw
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// registerTTL 注册带 TTL 检查的服务并在测试结束时注销。
func registerTTL(t *testing.T, raw *api.Client, id, name, addr string, port int) string {
	t.Helper()
	checkID := "ttl:" + id
	err := raw.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID: id, Name: name, Address: addr, Port: port,
		Check: &api.AgentServiceCheck{CheckID: checkID, TTL: "1m"},
	})
	if err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
	t.Cleanup(func() { _ = raw.Agent().ServiceDeregister(id) })
	return checkID
}

func TestIntegrationGetHealthChecksByInstanceID(t *testing.T) {
	c, raw := integrationClients(t)
	name := uniqueName("it-health")
	idA, idB := name+"-a", name+"-b"
	checkA := registerTTL(t, raw, idA, name, "127.0.0.1", 9)
	if err := raw.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID: idA, Name: name, Port: 9,
		Checks: api.AgentServiceChecks{
			{CheckID: checkA, TTL: "1m"},
			{CheckID: checkA + "-2", TTL: "1m"},
			{CheckID: checkA + "-3", TTL: "1m"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	checkB := registerTTL(t, raw, idB, name, "127.0.0.1", 9)

	for id, st := range map[string]string{checkA: "pass", checkA + "-2": "warn", checkA + "-3": "fail", checkB: "fail"} {
		var err error
		switch st {
		case "pass":
			err = raw.Agent().PassTTL(id, "ok")
		case "warn":
			err = raw.Agent().WarnTTL(id, "warn")
		default:
			err = raw.Agent().FailTTL(id, "down")
		}
		if err != nil {
			t.Fatalf("update %s: %v", id, err)
		}
	}

	got, err := c.GetHealthChecks(idA)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{checkA: api.HealthPassing, checkA + "-2": api.HealthWarning, checkA + "-3": api.HealthCritical}
	if len(got) != len(want) {
		t.Fatalf("got %d checks, want %d: %+v", len(got), len(want), got)
	}
	for _, ch := range got {
		if ch.ServiceID != idA {
			t.Errorf("leaked check %s of service %s", ch.CheckID, ch.ServiceID)
		}
		if want[ch.CheckID] != ch.Status {
			t.Errorf("%s status = %q, want %q", ch.CheckID, ch.Status, want[ch.CheckID])
		}
	}

	// 实例 B 只有 critical 检查，不能被隐藏
	gotB, err := c.GetHealthChecks(idB)
	if err != nil || len(gotB) != 1 || gotB[0].Status != api.HealthCritical {
		t.Fatalf("instance B: checks=%+v err=%v, want one critical", gotB, err)
	}

	if byName, err := c.GetHealthChecks(name); err != nil || len(byName) != 0 {
		t.Errorf("by service name: %+v %v, want empty", byName, err)
	}
	if unknown, err := c.GetHealthChecks(name + "-missing"); err != nil || len(unknown) != 0 {
		t.Errorf("unknown ID: %+v %v, want empty", unknown, err)
	}
}

func TestIntegrationRoundRobinConcurrent(t *testing.T) {
	c, raw := integrationClients(t)
	name := uniqueName("it-rr")

	var hits [2]atomic.Int64
	for i := range hits {
		i := i
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits[i].Add(1) }))
		t.Cleanup(backend.Close)
		host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
		port, _ := strconv.Atoi(portStr)
		check := registerTTL(t, raw, fmt.Sprintf("%s-%d", name, i), name, host, port)
		if err := raw.Agent().PassTTL(check, "ok"); err != nil {
			t.Fatal(err)
		}
	}

	// 等待两个实例都出现在健康列表中（有界等待）
	deadline := time.Now().Add(integrationWait)
	for {
		svcs, err := c.GetHealthyServices(name)
		if err == nil && len(svcs) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("instances not healthy in time: %v %d", err, len(svcs))
		}
		time.Sleep(100 * time.Millisecond)
	}

	inv := c.NewServiceInvoker(name, consul.WithRetry(0, 0))
	const goroutines, perGoroutine = 12, 10
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < perGoroutine; n++ {
				resp, err := inv.Call(http.MethodGet, "/", nil, nil)
				if err != nil {
					t.Errorf("Call: %v", err)
					return
				}
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	for i := range hits {
		if got := hits[i].Load(); got != goroutines*perGoroutine/2 {
			t.Errorf("backend %d hits = %d, want %d", i, got, goroutines*perGoroutine/2)
		}
	}
}
