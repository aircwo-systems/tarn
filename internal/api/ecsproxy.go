package api

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	ecssvc "github.com/aircwo-systems/tarn/internal/ecs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// registerECSProxyRoutes wires /_ecs/{account}/{cluster}/{service}/{rest...},
// a reverse proxy that lets a caller reach a running ECS service task at a
// stable URL instead of having to look up the ephemeral host port Docker
// assigned it. All HTTP methods (including WebSocket upgrades, which
// httputil.ReverseProxy proxies natively) are supported.
func (s *Server) registerECSProxyRoutes(mux *http.ServeMux) {
	// NOTE: patterns are registered per-method (not method-less) on purpose:
	// a method-less pattern that matches more methods than an overlapping
	// method-qualified pattern (e.g. GET /{bucket}/{key...}) makes
	// net/http.ServeMux panic at startup. With matching methods the more
	// specific /_ecs/ prefix wins cleanly, same as the API Gateway invoke
	// routes elsewhere in registerRoutes.
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead,
		http.MethodOptions,
	} {
		m := method
		mux.HandleFunc(m+" /_ecs/{account}/{cluster}/{service}/{rest...}", s.ecsProxyHandler)
		mux.HandleFunc(m+" /_ecs/{account}/{cluster}/{service}", s.ecsProxyRedirect)
	}
}

func (s *Server) ecsProxyRedirect(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// numericAccountID matches Tarn's 12-digit account ID convention (see
// internal/account.numericAKID and internal/cli/server.go's
// validAccountDirectoryName).
var numericAccountID = regexp.MustCompile(`^\d{12}$`)

// ecsProxyRoundRobin tracks a per-(account,cluster,service) request counter
// so repeated calls spread across a service's running tasks rather than
// always hitting the first one.
var ecsProxyRoundRobin sync.Map // map[string]*uint64

func (s *Server) ecsProxyHandler(w http.ResponseWriter, r *http.Request) {
	account := r.PathValue("account")
	clusterRef := r.PathValue("cluster")
	serviceName := r.PathValue("service")
	rest := r.PathValue("rest")

	if !numericAccountID.MatchString(account) {
		writeECSProxyError(w, http.StatusNotFound, "unknown account: "+account)
		return
	}

	hs := s.registry.get(account)
	if hs == nil || hs.ECS == nil {
		writeECSProxyError(w, http.StatusNotFound, "ECS is not configured for account "+account)
		return
	}
	ecsSvc := hs.ECS.Service()

	cluster, err := ecsSvc.ResolveCluster(clusterRef)
	if err != nil {
		writeECSProxyError(w, http.StatusNotFound, "cluster not found: "+clusterRef)
		return
	}

	describedServices, err := ecsSvc.DescribeServices(&types.DescribeServicesInput{
		Cluster:  cluster.ClusterArn,
		Services: []string{serviceName},
	})
	if err != nil || describedServices == nil || len(describedServices.Services) == 0 {
		writeECSProxyError(w, http.StatusNotFound, "service not found: "+serviceName)
		return
	}

	hostPort, ok := pickTaskHostPort(ecsSvc, cluster.ClusterArn, serviceName, account+"/"+clusterRef+"/"+serviceName)
	if !ok {
		writeECSProxyError(w, http.StatusServiceUnavailable, "no running task with a published port for service "+serviceName)
		return
	}

	prefix := fmt.Sprintf("/_ecs/%s/%s/%s", account, clusterRef, serviceName)
	target := fmt.Sprintf("127.0.0.1:%d", hostPort)

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = target
			pr.Out.Host = target
			path := rest
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			pr.Out.URL.Path = path
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
		},
	}
	proxy.ServeHTTP(w, r)
}

// pickTaskHostPort finds a RUNNING task belonging to serviceName in
// clusterArn, and returns the host port of the first port binding on its
// first essential container (falling back to any container/binding if the
// task definition's essential flags cannot be resolved). Multiple candidate
// tasks are round-robined across via routeKey.
func pickTaskHostPort(ecsSvc *ecssvc.Service, clusterArn, serviceName, routeKey string) (int, bool) {
	listed, err := ecsSvc.ListTasks(&types.ListTasksInput{
		Cluster:     clusterArn,
		ServiceName: serviceName,
	})
	if err != nil || listed == nil || len(listed.TaskArns) == 0 {
		return 0, false
	}

	described, err := ecsSvc.DescribeTasks(&types.DescribeTasksInput{
		Cluster: clusterArn,
		Tasks:   listed.TaskArns,
	})
	if err != nil || described == nil {
		return 0, false
	}

	var candidates []int
	for _, task := range described.Tasks {
		if task.LastStatus != types.TaskStatusRunning {
			continue
		}
		if port, ok := firstBoundPort(ecsSvc, task); ok {
			candidates = append(candidates, port)
		}
	}
	if len(candidates) == 0 {
		return 0, false
	}
	// DescribeTasks' order isn't guaranteed stable across calls (the store
	// may back it with a map), so sort before indexing — otherwise this
	// wouldn't round-robin at all, just pick unpredictably.
	sort.Ints(candidates)

	counterAny, _ := ecsProxyRoundRobin.LoadOrStore(routeKey, new(uint64))
	counter := counterAny.(*uint64)
	idx := atomic.AddUint64(counter, 1) - 1
	return candidates[int(idx%uint64(len(candidates)))], true
}

// firstBoundPort returns the host port of the first port binding found on
// task's first essential container (in Containers order), or, if the task
// definition can't be resolved, the first binding on any container.
func firstBoundPort(ecsSvc *ecssvc.Service, task types.Task) (int, bool) {
	essential := essentialContainerNames(ecsSvc, task.TaskDefinitionArn)
	for _, c := range task.Containers {
		if essential != nil && !essential[c.Name] {
			continue
		}
		for _, nb := range c.NetworkBindings {
			if nb.HostPort != 0 {
				return nb.HostPort, true
			}
		}
	}
	if essential != nil {
		// No essential container had a binding; don't fall back to a
		// non-essential one — that would route to a sidecar.
		return 0, false
	}
	for _, c := range task.Containers {
		for _, nb := range c.NetworkBindings {
			if nb.HostPort != 0 {
				return nb.HostPort, true
			}
		}
	}
	return 0, false
}

// essentialContainerNames returns the set of essential container names for
// taskDefinitionArn, or nil if the task definition can't be resolved (an
// absent Essential flag defaults to true, per ECS semantics).
func essentialContainerNames(ecsSvc *ecssvc.Service, taskDefinitionArn string) map[string]bool {
	desc, err := ecsSvc.DescribeTaskDefinition(taskDefinitionArn, nil)
	if err != nil || desc == nil || desc.TaskDefinition == nil {
		return nil
	}
	names := make(map[string]bool, len(desc.TaskDefinition.ContainerDefinitions))
	for _, cd := range desc.TaskDefinition.ContainerDefinitions {
		if cd.Essential == nil || *cd.Essential {
			names[cd.Name] = true
		}
	}
	return names
}

func writeECSProxyError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintln(w, msg)
}
