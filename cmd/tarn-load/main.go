// Command tarn-load drives sustained load against a running Tarn instance and
// reports throughput and latency, so performance changes have before and after
// numbers. It talks raw HTTP to the default account and removes every resource
// it creates (all named tarn-load-*).
//
//	go run ./cmd/tarn-load -endpoint http://127.0.0.1:4599 -scenario all
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	accountID = "000000000000"
	region    = "us-east-1"
	prefix    = "tarn-load-"
)

var (
	endpoint    string
	duration    time.Duration
	concurrency int
	esmMessages int
	snsPublish  int

	httpClient = &http.Client{Timeout: 16 * time.Minute}
)

func main() {
	scenario := flag.String("scenario", "all", "invoke, esm, sns, dynamodb or all")
	flag.StringVar(&endpoint, "endpoint", "http://127.0.0.1:4566", "Tarn endpoint")
	flag.DurationVar(&duration, "duration", 20*time.Second, "how long the invoke and dynamodb scenarios run")
	flag.IntVar(&concurrency, "concurrency", 8, "concurrent clients for the invoke and dynamodb scenarios")
	flag.IntVar(&esmMessages, "esm-messages", 300, "messages sent to the SQS event source mapping")
	flag.IntVar(&snsPublish, "sns-publishes", 20, "publishes to the SNS topic with three Lambda subscribers")
	flag.Parse()

	scenarios := map[string]func() error{
		"invoke":   runInvoke,
		"esm":      runESM,
		"sns":      runSNS,
		"dynamodb": runDynamoDB,
	}
	order := []string{"invoke", "esm", "sns", "dynamodb"}
	if *scenario != "all" {
		if _, ok := scenarios[*scenario]; !ok {
			log.Fatalf("unknown scenario %q", *scenario)
		}
		order = []string{*scenario}
	}
	failed := false
	for _, name := range order {
		fmt.Printf("== %s\n", name)
		if err := scenarios[name](); err != nil {
			fmt.Printf("   FAILED: %v\n", err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// ---- scenarios -------------------------------------------------------------

// runInvoke measures sustained synchronous Invoke of a warm function.
func runInvoke() error {
	fn := prefix + "echo"
	if err := createFunction(fn, `exports.handler = async (e) => ({ ok: true, n: e.n });`); err != nil {
		return err
	}
	defer deleteFunction(fn)
	if _, err := invoke(fn, `{"n":0}`, ""); err != nil { // cold start outside the measurement
		return err
	}
	stats := runFor(duration, concurrency, func(i int) error {
		_, err := invoke(fn, fmt.Sprintf(`{"n":%d}`, i), "")
		return err
	})
	stats.print("invoke")
	return nil
}

// runESM measures how fast an SQS event source mapping drains a backlog.
func runESM() error {
	fn := prefix + "consumer"
	queue := prefix + "esm"
	if err := createFunction(fn, `exports.handler = async (e) => ({ n: e.Records.length });`); err != nil {
		return err
	}
	defer deleteFunction(fn)
	if _, err := invoke(fn, `{"Records":[]}`, ""); err != nil {
		return err
	}
	if err := sqs("CreateQueue", url.Values{"QueueName": {queue}}, nil); err != nil {
		return err
	}
	defer func() { _ = sqs("DeleteQueue", url.Values{"QueueUrl": {queueURL(queue)}}, nil) }()

	for sent := 0; sent < esmMessages; sent += 10 {
		form := url.Values{"QueueUrl": {queueURL(queue)}}
		for j := 1; j <= 10 && sent+j <= esmMessages; j++ {
			form.Set(fmt.Sprintf("SendMessageBatchRequestEntry.%d.Id", j), strconv.Itoa(j))
			form.Set(fmt.Sprintf("SendMessageBatchRequestEntry.%d.MessageBody", j), fmt.Sprintf(`{"i":%d}`, sent+j))
		}
		if err := sqs("SendMessageBatch", form, nil); err != nil {
			return err
		}
	}

	var mapping struct{ UUID string }
	start := time.Now()
	if err := doJSON(http.MethodPost, "/2015-03-31/event-source-mappings", map[string]any{
		"EventSourceArn": fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, accountID, queue),
		"FunctionName":   fn,
		"BatchSize":      10,
	}, &mapping); err != nil {
		return err
	}
	defer func() { _ = doJSON(http.MethodDelete, "/2015-03-31/event-source-mappings/"+mapping.UUID, nil, nil) }()

	deadline := start.Add(10 * time.Minute)
	for {
		visible, inFlight, err := queueDepth(queue)
		if err != nil {
			return err
		}
		if visible == 0 && inFlight == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("queue not drained after %s (%d visible, %d in flight)", time.Since(start).Round(time.Second), visible, inFlight)
		}
		time.Sleep(100 * time.Millisecond)
	}
	elapsed := time.Since(start)
	fmt.Printf("   drained %d messages in %s: %.1f msgs/sec\n", esmMessages, elapsed.Round(time.Millisecond), float64(esmMessages)/elapsed.Seconds())
	return nil
}

// runSNS measures Publish latency for a topic with three Lambda subscribers
// that each take about 200ms.
func runSNS() error {
	topicName := prefix + "fanout"
	var arn string
	if err := snsCall(url.Values{"Action": {"CreateTopic"}, "Name": {topicName}}, &arn, "TopicArn"); err != nil {
		return err
	}
	defer func() { _ = snsCall(url.Values{"Action": {"DeleteTopic"}, "TopicArn": {arn}}, nil, "") }()

	for i := 1; i <= 3; i++ {
		fn := fmt.Sprintf("%ssub-%d", prefix, i)
		if err := createFunction(fn, `exports.handler = async () => { await new Promise(r => setTimeout(r, 200)); return {}; };`); err != nil {
			return err
		}
		defer deleteFunction(fn)
		if _, err := invoke(fn, `{}`, ""); err != nil {
			return err
		}
		if err := snsCall(url.Values{
			"Action":   {"Subscribe"},
			"TopicArn": {arn},
			"Protocol": {"lambda"},
			"Endpoint": {fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", region, accountID, fn)},
		}, nil, ""); err != nil {
			return err
		}
	}

	var lat []time.Duration
	errs := 0
	for i := 0; i < snsPublish; i++ {
		start := time.Now()
		if err := snsCall(url.Values{"Action": {"Publish"}, "TopicArn": {arn}, "Message": {fmt.Sprintf(`{"i":%d}`, i)}}, nil, ""); err != nil {
			errs++
			continue
		}
		lat = append(lat, time.Since(start))
	}
	s := stats{latencies: lat, errors: errs, elapsed: sum(lat)}
	s.print("publish")
	return nil
}

// runDynamoDB measures sustained PutItem followed by Query on the same key.
func runDynamoDB() error {
	table := prefix + "items"
	if err := dynamo("CreateTable", map[string]any{
		"TableName":            table,
		"BillingMode":          "PAY_PER_REQUEST",
		"AttributeDefinitions": []map[string]string{{"AttributeName": "pk", "AttributeType": "S"}, {"AttributeName": "sk", "AttributeType": "S"}},
		"KeySchema":            []map[string]string{{"AttributeName": "pk", "KeyType": "HASH"}, {"AttributeName": "sk", "KeyType": "RANGE"}},
	}); err != nil {
		return err
	}
	defer func() { _ = dynamo("DeleteTable", map[string]any{"TableName": table}) }()

	stats := runFor(duration, concurrency, func(i int) error {
		pk := fmt.Sprintf("p%d", i%64)
		sk := fmt.Sprintf("%012d", i)
		if err := dynamo("PutItem", map[string]any{
			"TableName": table,
			"Item":      map[string]any{"pk": map[string]string{"S": pk}, "sk": map[string]string{"S": sk}, "v": map[string]string{"S": strings.Repeat("x", 256)}},
		}); err != nil {
			return err
		}
		return dynamo("Query", map[string]any{
			"TableName":                 table,
			"KeyConditionExpression":    "pk = :pk",
			"ExpressionAttributeValues": map[string]any{":pk": map[string]string{"S": pk}},
			"Limit":                     10,
		})
	})
	stats.print("put+query")
	return nil
}

// ---- load loop and stats ---------------------------------------------------

type stats struct {
	latencies []time.Duration
	errors    int
	firstErr  error
	elapsed   time.Duration
}

// runFor calls op from workers concurrent goroutines until d has passed.
func runFor(d time.Duration, workers int, op func(i int) error) stats {
	var (
		mu  sync.Mutex
		out stats
		n   int
		wg  sync.WaitGroup
	)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				mu.Lock()
				i := n
				n++
				mu.Unlock()
				t := time.Now()
				err := op(i)
				lat := time.Since(t)
				mu.Lock()
				if err != nil {
					out.errors++
					if out.firstErr == nil {
						out.firstErr = err
					}
				} else {
					out.latencies = append(out.latencies, lat)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	out.elapsed = time.Since(start)
	return out
}

func (s stats) print(label string) {
	sort.Slice(s.latencies, func(i, j int) bool { return s.latencies[i] < s.latencies[j] })
	ok := len(s.latencies)
	fmt.Printf("   %s: %d ok, %d errors in %s: %.1f ops/sec\n", label, ok, s.errors, s.elapsed.Round(time.Millisecond), float64(ok)/s.elapsed.Seconds())
	if ok > 0 {
		fmt.Printf("   latency p50 %s  p95 %s  p99 %s  max %s\n", pct(s.latencies, 50), pct(s.latencies, 95), pct(s.latencies, 99), s.latencies[ok-1].Round(time.Microsecond))
	}
	if s.firstErr != nil {
		fmt.Printf("   first error: %v\n", s.firstErr)
	}
}

func pct(sorted []time.Duration, p int) time.Duration {
	i := (len(sorted)*p + 99) / 100
	if i > 0 {
		i--
	}
	return sorted[i].Round(time.Microsecond)
}

func sum(ds []time.Duration) time.Duration {
	var t time.Duration
	for _, d := range ds {
		t += d
	}
	return t
}

// ---- AWS calls -------------------------------------------------------------

func createFunction(name, code string) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("index.js")
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(code)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	deleteFunction(name)
	return doJSON(http.MethodPost, "/2015-03-31/functions", map[string]any{
		"FunctionName": name,
		"Runtime":      "nodejs20.x",
		"Handler":      "index.handler",
		"Role":         "arn:aws:iam::" + accountID + ":role/load",
		"Timeout":      30,
		"MemorySize":   128,
		"Code":         map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(buf.Bytes())},
	}, nil)
}

func deleteFunction(name string) {
	_ = doJSON(http.MethodDelete, "/2015-03-31/functions/"+name, nil, nil)
}

func invoke(name, payload, invocationType string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint+"/2015-03-31/functions/"+name+"/invocations", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if invocationType != "" {
		req.Header.Set("X-Amz-Invocation-Type", invocationType)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 || resp.Header.Get("X-Amz-Function-Error") != "" {
		return nil, fmt.Errorf("invoke %s: %d %s", name, resp.StatusCode, truncate(body))
	}
	return body, nil
}

func queueURL(name string) string { return endpoint + "/" + accountID + "/" + name }

func sqs(action string, form url.Values, out *[]byte) error {
	form.Set("Action", action)
	resp, err := httpClient.PostForm(endpoint+"/", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sqs %s: %d %s", action, resp.StatusCode, truncate(body))
	}
	if out != nil {
		*out = body
	}
	return nil
}

var attrRe = regexp.MustCompile(`<Name>(\w+)</Name>\s*<Value>(\d+)</Value>`)

func queueDepth(queue string) (visible, inFlight int, err error) {
	var body []byte
	if err := sqs("GetQueueAttributes", url.Values{"QueueUrl": {queueURL(queue)}, "AttributeName.1": {"All"}}, &body); err != nil {
		return 0, 0, err
	}
	for _, m := range attrRe.FindAllStringSubmatch(string(body), -1) {
		n, _ := strconv.Atoi(m[2])
		switch m[1] {
		case "ApproximateNumberOfMessages", "ApproximateNumberOfMessagesDelayed":
			visible += n
		case "ApproximateNumberOfMessagesNotVisible":
			inFlight += n
		}
	}
	return visible, inFlight, nil
}

// snsCall sends a query-protocol SNS request; when out is set it receives the
// text of the first <tag> element in the response.
func snsCall(form url.Values, out *string, tag string) error {
	resp, err := httpClient.PostForm(endpoint+"/", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sns %s: %d %s", form.Get("Action"), resp.StatusCode, truncate(body))
	}
	if out != nil {
		m := regexp.MustCompile("<" + tag + ">([^<]+)</" + tag + ">").FindSubmatch(body)
		if m == nil {
			return fmt.Errorf("sns %s: no %s in %s", form.Get("Action"), tag, truncate(body))
		}
		*out = string(m[1])
	}
	return nil
}

func dynamo(action string, input any) error {
	b, _ := json.Marshal(input)
	req, err := http.NewRequest(http.MethodPost, endpoint+"/", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810."+action)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dynamodb %s: %d %s", action, resp.StatusCode, truncate(body))
	}
	return nil
}

func doJSON(method, path string, in, out any) error {
	var r io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, endpoint+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, truncate(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
