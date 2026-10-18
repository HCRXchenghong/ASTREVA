package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type createConversationResp struct {
	Token        string `json:"token"`
	Conversation struct {
		ID string `json:"id"`
	} `json:"conversation"`
}

func main() {
	baseHTTP := flag.String("http", "http://localhost:8080", "HTTP base URL")
	baseWS := flag.String("ws", "ws://localhost:8080", "WebSocket base URL")
	concurrency := flag.Int("n", 100, "number of visitor connections")
	duration := flag.Duration("duration", 30*time.Second, "test duration")
	interval := flag.Duration("interval", 5*time.Second, "message interval per connection")
	messagesPerConn := flag.Int("messages-per-conn", 0, "fixed messages per connection; 0 means send until duration expires")
	messageType := flag.String("message-type", "text", "message type to send, for example text or image")
	content := flag.String("content", "压测消息", "message content")
	connectParallel := flag.Int("connect-parallel", 256, "maximum concurrent HTTP+WebSocket setup workers")
	setupTimeout := flag.Duration("setup-timeout", 2*time.Minute, "maximum time allowed for connection setup")
	flag.Parse()

	var connected int64
	var sent int64
	var acked int64
	var received int64
	var failed int64
	latencies := &latencyStats{}

	start := time.Now()
	setupStart := time.Now()
	runtimes, setupFailed := setupVisitors(*baseHTTP, *baseWS, *concurrency, *connectParallel, *setupTimeout)
	setupElapsed := time.Since(setupStart)
	atomic.AddInt64(&connected, int64(len(runtimes)))
	atomic.AddInt64(&failed, int64(setupFailed))

	runCtx, cancelRun := context.WithTimeout(context.Background(), *duration)
	defer cancelRun()

	var wg sync.WaitGroup
	for _, runtime := range runtimes {
		wg.Add(1)
		go func(runtime visitorRuntime) {
			defer wg.Done()
			if err := runVisitor(runCtx, runtime, *interval, *messagesPerConn, *messageType, *content, &sent, &acked, &received, latencies); err != nil {
				atomic.AddInt64(&failed, 1)
			}
		}(runtime)
	}
	wg.Wait()

	latencySummary := latencies.summary()
	fmt.Printf("duration=%s setup=%s target=%d connected=%d sent=%d acked=%d received=%d failed=%d ack_latency_avg=%s ack_latency_p95=%s ack_latency_max=%s\n",
		time.Since(start).Round(time.Millisecond),
		setupElapsed.Round(time.Millisecond),
		*concurrency,
		connected,
		sent,
		acked,
		received,
		failed,
		latencySummary.avg,
		latencySummary.p95,
		latencySummary.max,
	)
}

type visitorRuntime struct {
	idx            int
	conversationID string
	conn           *websocket.Conn
}

type setupResult struct {
	runtime visitorRuntime
	err     error
}

func setupVisitors(baseHTTP, baseWS string, concurrency int, connectParallel int, setupTimeout time.Duration) ([]visitorRuntime, int) {
	if connectParallel <= 0 || connectParallel > concurrency {
		connectParallel = concurrency
	}
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), setupTimeout)
	defer cancelSetup()

	jobs := make(chan int)
	results := make(chan setupResult, concurrency)
	var setupWG sync.WaitGroup

	for worker := 0; worker < connectParallel; worker++ {
		setupWG.Add(1)
		go func() {
			defer setupWG.Done()
			for idx := range jobs {
				runtime, err := setupVisitor(setupCtx, baseHTTP, baseWS, idx)
				results <- setupResult{runtime: runtime, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for idx := 0; idx < concurrency; idx++ {
			select {
			case <-setupCtx.Done():
				return
			case jobs <- idx:
			}
		}
	}()

	go func() {
		setupWG.Wait()
		close(results)
	}()

	runtimes := make([]visitorRuntime, 0, concurrency)
	failed := 0
	completed := 0
	for result := range results {
		completed++
		if result.err != nil {
			failed++
			continue
		}
		runtimes = append(runtimes, result.runtime)
	}
	failed += concurrency - completed
	return runtimes, failed
}

func setupVisitor(ctx context.Context, baseHTTP, baseWS string, idx int) (visitorRuntime, error) {
	session, err := createConversation(ctx, baseHTTP)
	if err != nil {
		return visitorRuntime{}, err
	}

	wsURL, err := url.Parse(baseWS + "/ws")
	if err != nil {
		return visitorRuntime{}, err
	}
	query := wsURL.Query()
	query.Set("role", "visitor")
	query.Set("token", session.Token)
	query.Set("conversation_id", session.Conversation.ID)
	wsURL.RawQuery = query.Encode()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL.String(), nil)
	if err != nil {
		return visitorRuntime{}, err
	}
	return visitorRuntime{idx: idx, conversationID: session.Conversation.ID, conn: conn}, nil
}

func runVisitor(ctx context.Context, runtime visitorRuntime, interval time.Duration, messagesPerConn int, messageType string, content string, sent, acked, received *int64, latencies *latencyStats) error {
	defer runtime.conn.Close()
	var pendingMu sync.Mutex
	pending := map[string]time.Time{}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, payload, err := runtime.conn.ReadMessage()
			if err != nil {
				return
			}
			atomic.AddInt64(received, 1)
			var event struct {
				Event string `json:"event"`
				Data  struct {
					ClientMsgID string `json:"client_msg_id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(payload, &event); err != nil || event.Event != "message.ack" || event.Data.ClientMsgID == "" {
				continue
			}
			pendingMu.Lock()
			startedAt, ok := pending[event.Data.ClientMsgID]
			if ok {
				delete(pending, event.Data.ClientMsgID)
			}
			pendingMu.Unlock()
			if ok {
				atomic.AddInt64(acked, 1)
				latencies.add(time.Since(startedAt))
			}
		}
	}()

	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sendOne := func() error {
		clientMsgID := fmt.Sprintf("load_%d_%d", runtime.idx, time.Now().UnixNano())
		payload := map[string]any{
			"event": "message.send",
			"data": map[string]any{
				"conversation_id": runtime.conversationID,
				"client_msg_id":   clientMsgID,
				"message_type":    messageType,
				"content":         content,
			},
		}
		pendingMu.Lock()
		pending[clientMsgID] = time.Now()
		pendingMu.Unlock()
		if err := runtime.conn.WriteJSON(payload); err != nil {
			return err
		}
		atomic.AddInt64(sent, 1)
		return nil
	}

	sentByConn := 0
	sendAndCount := func() error {
		if err := sendOne(); err != nil {
			return err
		}
		sentByConn++
		return nil
	}
	if err := sendAndCount(); err != nil {
		return err
	}
	if messagesPerConn > 0 && sentByConn >= messagesPerConn {
		<-ctx.Done()
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readDone:
			return nil
		case <-ticker.C:
			if err := sendAndCount(); err != nil {
				return err
			}
			if messagesPerConn > 0 && sentByConn >= messagesPerConn {
				<-ctx.Done()
				return nil
			}
		}
	}
}

func createConversation(ctx context.Context, baseHTTP string) (createConversationResp, error) {
	body := bytes.NewBufferString(`{"source":"loadtest"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseHTTP+"/api/visitor/conversations", body)
	if err != nil {
		return createConversationResp{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return createConversationResp{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return createConversationResp{}, fmt.Errorf("create conversation status=%d", resp.StatusCode)
	}
	var out createConversationResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return createConversationResp{}, err
	}
	if out.Token == "" || out.Conversation.ID == "" {
		log.Printf("invalid response: %+v", out)
		return createConversationResp{}, fmt.Errorf("invalid conversation response")
	}
	return out, nil
}

type latencyStats struct {
	mu      sync.Mutex
	samples []time.Duration
}

type latencySummary struct {
	avg time.Duration
	p95 time.Duration
	max time.Duration
}

func (s *latencyStats) add(value time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, value)
}

func (s *latencyStats) summary() latencySummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) == 0 {
		return latencySummary{}
	}
	samples := append([]time.Duration(nil), s.samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	var total time.Duration
	for _, sample := range samples {
		total += sample
	}
	p95Index := int(float64(len(samples))*0.95) - 1
	if p95Index < 0 {
		p95Index = 0
	}
	if p95Index >= len(samples) {
		p95Index = len(samples) - 1
	}
	return latencySummary{
		avg: (total / time.Duration(len(samples))).Round(time.Millisecond),
		p95: samples[p95Index].Round(time.Millisecond),
		max: samples[len(samples)-1].Round(time.Millisecond),
	}
}
