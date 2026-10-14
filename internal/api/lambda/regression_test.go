package lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	lambdasvc "github.com/aircwo-systems/tarn/internal/lambda"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func regressionHandler(t *testing.T) *Handler {
	t.Helper()
	h := newLambdaHandler(t)
	if _, err := h.svc.CreateFunction(context.Background(), &types.FunctionConfig{FunctionName: "fn", Runtime: types.RuntimeNodeJS20, Handler: "index.handler"}, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := h.svc.PublishVersion(context.Background(), "fn", ""); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func functionRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	r.SetPathValue("name", "fn")
	r.SetPathValue("aliasName", "live")
	return r
}

type pausedFunctionWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	resume  chan struct{}
}

func (w *pausedFunctionWriter) WriteHeader(status int) {
	close(w.entered)
	<-w.resume
	w.ResponseRecorder.WriteHeader(status)
}

func TestAliasHTTPResponsesAreDetachedDuringUpdates(t *testing.T) {
	h := regressionHandler(t)
	w := &pausedFunctionWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		h.CreateAlias(w, functionRequest(http.MethodPost, `{"Name":"live","FunctionVersion":"1"}`))
		close(done)
	}()
	<-w.entered
	update := httptest.NewRecorder()
	h.UpdateAlias(update, functionRequest(http.MethodPut, `{"FunctionVersion":"2"}`))
	close(w.resume)
	<-done
	if update.Code != http.StatusOK || w.Code != http.StatusCreated {
		t.Fatalf("alias responses %d %d: %s %s", update.Code, w.Code, update.Body, w.Body)
	}
	var alias lambdasvc.Alias
	if err := json.Unmarshal(w.Body.Bytes(), &alias); err != nil {
		t.Fatal(err)
	}
	if alias.FunctionVersion != "1" {
		t.Fatalf("response changed after unlock: %+v", alias)
	}
	get := httptest.NewRecorder()
	h.GetAlias(get, functionRequest(http.MethodGet, ""))
	if err := json.Unmarshal(get.Body.Bytes(), &alias); err != nil {
		t.Fatal(err)
	}
	if alias.FunctionVersion != "2" {
		t.Fatal("alias update not committed")
	}
}

func TestPolicyConcurrentHTTPReadsAndWrites(t *testing.T) {
	h := regressionHandler(t)
	start := make(chan struct{})
	failures := make(chan string, 4)
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range 30 {
				w := httptest.NewRecorder()
				if worker%2 == 0 {
					h.GetPolicy(w, functionRequest(http.MethodGet, ""))
					if w.Code != 200 && w.Code != 404 {
						failures <- w.Body.String()
						return
					}
				} else {
					h.AddPermission(w, functionRequest(http.MethodPost, fmt.Sprintf(`{"StatementId":"%d","Action":"action%d","Principal":"s3.amazonaws.com"}`, worker, i)))
					if w.Code != 200 {
						failures <- w.Body.String()
						return
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
	// Rebuilding the HTTP adapter must preserve the stored policy and aliases.
	rebuilt := NewHandler(h.svc, nil)
	w := httptest.NewRecorder()
	rebuilt.GetPolicy(w, functionRequest(http.MethodGet, ""))
	if w.Code != 200 {
		t.Fatalf("handler replacement lost policy: %s", w.Body)
	}
}

func TestPublishedVersionsAppearInHTTPListAndQualifiedReads(t *testing.T) {
	h := regressionHandler(t)
	w := httptest.NewRecorder()
	h.ListVersionsByFunction(w, functionRequest(http.MethodGet, ""))
	var body struct{ Versions []types.FunctionConfig }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Versions) != 3 || body.Versions[0].Version != "$LATEST" || body.Versions[1].Version != "1" || body.Versions[2].Version != "2" {
		t.Fatalf("published versions omitted: %s", w.Body)
	}
	r := functionRequest(http.MethodGet, "")
	r.URL.RawQuery = "Qualifier=1"
	w = httptest.NewRecorder()
	h.GetFunctionConfiguration(w, r)
	var fn types.FunctionConfig
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || fn.Version != "1" {
		t.Fatalf("qualifier ignored: %d %s", w.Code, w.Body)
	}
}

func TestFunctionValidationAndConcurrencyHTTP(t *testing.T) {
	h := regressionHandler(t)
	for _, body := range []string{`{"FunctionName":"../outside","Runtime":"nodejs20.x","Handler":"index.handler"}`, `{"FunctionName":"badzip","Runtime":"nodejs20.x","Handler":"index.handler","Code":{"ZipFile":"aW52YWxpZA=="}}`} {
		w := httptest.NewRecorder()
		h.CreateFunction(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if w.Code != 400 || w.Header().Get("X-Amzn-ErrorType") != "InvalidParameterValueException" {
			t.Fatalf("invalid deployment: %d %s", w.Code, w.Body)
		}
	}
	for _, value := range []int{0, 2} {
		w := httptest.NewRecorder()
		h.PutFunctionConcurrency(w, functionRequest(http.MethodPut, fmt.Sprintf(`{"ReservedConcurrentExecutions":%d}`, value)))
		if w.Code != 200 {
			t.Fatalf("put concurrency: %d %s", w.Code, w.Body)
		}
		w = httptest.NewRecorder()
		h.GetFunctionConcurrency(w, functionRequest(http.MethodGet, ""))
		var response map[string]int
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["ReservedConcurrentExecutions"] != value {
			t.Fatal("concurrency not stored")
		}
	}
	w := httptest.NewRecorder()
	h.PutFunctionConcurrency(w, functionRequest(http.MethodPut, `{"ReservedConcurrentExecutions":-1}`))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.DeleteFunctionConcurrency(w, functionRequest(http.MethodDelete, ""))
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.GetFunctionConcurrency(w, functionRequest(http.MethodGet, ""))
	if strings.TrimSpace(w.Body.String()) != `{}` {
		t.Fatal(w.Body.String())
	}
}

func TestImageCreateWithoutRuntimeOrHandlerPersistsImageConfiguration(t *testing.T) {
	h := newLambdaHandler(t)
	w := httptest.NewRecorder()
	h.CreateFunction(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"FunctionName":"image","PackageType":"Image","Code":{"ImageUri":"local:latest"},"ImageConfig":{"Command":["index.handler"]}}`)))
	if w.Code != 201 {
		t.Fatalf("image create: %d %s", w.Code, w.Body)
	}
	r := functionRequest(http.MethodGet, "")
	r.SetPathValue("name", "image")
	w = httptest.NewRecorder()
	h.GetFunction(w, r)
	var response struct {
		Configuration struct{ PackageType string }
		Code          struct {
			ImageUri       string
			RepositoryType string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Configuration.PackageType != "Image" || response.Code.ImageUri != "local:latest" || response.Code.RepositoryType != "ECR" {
		t.Fatalf("image metadata lost: %s", w.Body)
	}
}
