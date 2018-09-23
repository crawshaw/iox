package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"crawshaw.io/iox"
)

func TestWebFetch(t *testing.T) {
	newReq := func(url string) *http.Request {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}

	block := make(chan struct{})
	close(block)

	sawMu := new(sync.Mutex)
	saw := make(map[string]int)

	handler := func(w http.ResponseWriter, r *http.Request) {
		sawMu.Lock()
		saw[r.URL.Path]++
		sawMu.Unlock()

		select {
		case <-r.Context().Done():
			return
		case <-block:
		}

		switch r.URL.Path {
		case "/404":
			w.WriteHeader(404)
		case "/500":
			w.WriteHeader(500)
		default:
			w.WriteHeader(200)
		}

		io.WriteString(w, "contentof:")
		io.WriteString(w, r.URL.Path)
	}
	ts := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer ts.Close()

	filer := iox.NewFiler(0)

	do := func(t *testing.T, webclient *Client, path string) {
		res, err := webclient.Do(newReq(ts.URL + path))
		if err != nil {
			t.Fatal(err)
		}
		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(body), "contentof:"+path; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	cache := newMemCache(t.Logf)
	webclient := &Client{
		Filer:    filer,
		Client:   ts.Client(),
		CachePut: cache.put,
		CacheGet: cache.get,
	}
	defer webclient.Shutdown(context.Background())

	t.Run("basic_fetch", func(t *testing.T) {
		do(t, webclient, "/basic")
	})

	t.Run("basic_fetch_repeat", func(t *testing.T) {
		saw = make(map[string]int)
		webclient.Logf = t.Logf
		defer func() { webclient.Logf = nil }()

		do(t, webclient, "/basic_repeat") // puts into cache
		do(t, webclient, "/basic_repeat") // should hit cache

		wg := new(sync.WaitGroup)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				// should hit cache, and probably elide into concurrent calls
				do(t, webclient, "/basic_repeat")
				wg.Done()
			}()
		}
		wg.Wait()

		const want = 1
		if got := saw["/basic_repeat"]; got != want {
			t.Errorf(`saw["/basic_repeat"]=%d, want %d`, got, want)
		}
	})

	t.Run("no_cache", func(t *testing.T) {
		webclient := &Client{
			Filer:  filer,
			Client: ts.Client(),
			Logf:   t.Logf,
		}
		defer webclient.Shutdown(context.Background())

		saw = make(map[string]int)

		do(t, webclient, "/no_cache")
		do(t, webclient, "/no_cache")

		const want = 2
		if got := saw["/no_cache"]; got != want {
			t.Errorf(`saw["/no_cache"]=%d, want %d`, got, want)
		}
	})

	t.Run("merge_batch", func(t *testing.T) {
		saw = make(map[string]int)
		block = make(chan struct{})

		wg := new(sync.WaitGroup)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				do(t, webclient, "/")
			}()
		}

		close(block)
		wg.Wait()

		if got, want := saw["/"], 1; got != want {
			t.Errorf("saw %d requests, want %d", got, want)
		}
	})

	t.Run("errors_skip_cache", func(t *testing.T) {
		saw = make(map[string]int)

		do(t, webclient, "/404")
		do(t, webclient, "/404")

		if got, want := saw["/404"], 2; got != want {
			t.Errorf("saw %d requests for /404, want %d", got, want)
		}

		do(t, webclient, "/500")
		do(t, webclient, "/500")

		if got, want := saw["/500"], 2; got != want {
			t.Errorf("saw %d requests for /500, want %d", got, want)
		}
	})

	t.Run("shutdown_graceful", func(t *testing.T) {
		cache := newMemCache(t.Logf)
		webclient := &Client{
			Filer:    filer,
			Client:   ts.Client(),
			CacheGet: cache.get,
			CachePut: cache.put,
		}
		block = make(chan struct{})

		done1 := make(chan struct{})
		go func() {
			do(t, webclient, "/shutdown_graceful")
			close(done1)
		}()
		done2 := make(chan struct{})
		go func() {
			do(t, webclient, "/shutdown_graceful")
			close(done2)
		}()
		select {
		case <-done1:
			t.Fatalf("done 1 while blocked")
		case <-done2:
			t.Fatalf("done 2 while blocked")
		case <-time.After(10 * time.Millisecond):
		}

		shutdownDone := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			webclient.Shutdown(ctx)
			close(shutdownDone)
		}()
		select {
		case <-done1:
			t.Fatalf("shutdown cancelled fetch 1 early")
		case <-done2:
			t.Fatalf("shutdown cancelled fetch 2 early")
		case <-shutdownDone:
			t.Fatalf("shutdown finished early")
		case <-time.After(10 * time.Millisecond):
		}

		close(block)
		<-done1
		<-done2
		<-shutdownDone
	})

	t.Run("shutdown_forced", func(t *testing.T) {
		cache := newMemCache(t.Logf)
		webclient := &Client{
			Filer:    filer,
			Client:   ts.Client(),
			CacheGet: cache.get,
			CachePut: cache.put,
		}
		block = make(chan struct{})
		defer close(block)

		done := make(chan struct{})
		go func() {
			res, err := webclient.Do(newReq(ts.URL + "/shutdown_forced"))
			if err == nil {
				res.Body.Close()
				t.Errorf("fetch unexpectedly successful")
			} else {
				if uerr, ok := err.(*url.Error); ok {
					err = uerr.Err
				}
				if err != context.Canceled {
					t.Errorf("err=%v, want context.Canceled", err)
				}
			}
			close(done)
		}()
		select {
		case <-done:
			t.Fatalf("done while blocked")
		case <-time.After(10 * time.Millisecond):
		}

		shutdownDone := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			webclient.Shutdown(ctx)
			close(shutdownDone)
		}()
		select {
		case <-done:
			t.Fatalf("shutdown cancelled fetch early")
		case <-shutdownDone:
			t.Fatalf("shutdown finished early")
		case <-time.After(10 * time.Millisecond):
		}

		cancel()
		<-done
		<-shutdownDone
	})

	t.Run("logs", func(t *testing.T) {
		var logs []string
		webclient.Logf = func(format string, v ...interface{}) {
			logs = append(logs, fmt.Sprintf(format, v...))
		}
		defer func() { webclient.Logf = nil }()

		do(t, webclient, "/logs")

		if len(logs) != 1 {
			t.Errorf("bad logs, len=%d", len(logs))
		}
		dec := make(map[string]interface{})
		if err := json.Unmarshal([]byte(logs[0]), &dec); err != nil {
			t.Fatal(err)
		}
		if got := dec["where"]; got != "webfetch" {
			t.Errorf(`where=%q, want "webfetch"`, got)
		}
		if got := dec["where"]; got != "webfetch" {
			t.Errorf(`what=%q, want "webfetch"`, got)
		}
		if got, want := dec["name"], ts.URL+"/logs"; got != want {
			t.Errorf(`name=%q, want %q`, got, want)
		}
		if got, want := dec["status"], float64(200); got != want {
			t.Errorf(`status=%v (%T), want %v`, got, got, want)
		}
		if got, want := dec["len"], float64(len("contentof:/logs")); got != want {
			t.Errorf(`len=%v, want %v`, got, want)
		}
		if _, found := dec["when"]; !found {
			t.Error(`missing "when"`)
		}
		if _, found := dec["duration"]; !found {
			t.Error(`missing "duration"`)
		}
	})
}

func TestDanglingBody(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}
	ts := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer ts.Close()

	filer := iox.NewFiler(0)

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	var logs []string
	cache := newMemCache(t.Logf)
	webclient := &Client{
		Filer:    filer,
		Client:   ts.Client(),
		CacheGet: cache.get,
		CachePut: cache.put,
		Logf: func(format string, v ...interface{}) {
			logs = append(logs, fmt.Sprintf(format, v...))
		},
	}
	res, err := webclient.Do(req)
	if err != nil {
		return
	}
	if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		webclient.Shutdown(ctx)
		close(done)
	}()

	select {
	case <-done:
		t.Errorf("early shutdown, response body still open")
	case <-time.After(10 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("shutdown stuck on dangling response body")
	}

	foundShutdownLog := false
	for _, log := range logs {
		t.Log(log)
		if strings.Contains(log, "force_shutdown") {
			foundShutdownLog = true
		}
	}
	if !foundShutdownLog {
		t.Errorf("logs do not mention the forced shutdown")
	}

	if err := res.Body.Close(); err == nil {
		t.Errorf("no error from forced shutdown")
	}
}

func TestConcurrency(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "contentof:")
		io.WriteString(w, r.URL.Path)
	}
	ts := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer ts.Close()

	filer := iox.NewFiler(0)

	newReq := func(path string) *http.Request {
		req, err := http.NewRequest("GET", ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}

	cache := newMemCache(t.Logf)
	webclient := &Client{
		Filer:    filer,
		Client:   ts.Client(),
		CachePut: cache.put,
		CacheGet: cache.get,
	}
	defer webclient.Shutdown(context.Background())

	// Concurrent cache filling.
	wg := new(sync.WaitGroup)
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

			path := fmt.Sprintf("/file%d", i)
			res, err := webclient.Do(newReq(path))
			if err != nil {
				t.Fatal(err)
			}
			body, err := ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(body), "contentof:"+path; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		}()
	}
	wg.Wait()

	// Concurrent cache hits.
	wg = new(sync.WaitGroup)
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

			path := fmt.Sprintf("/file%d", i)
			res, err := webclient.Do(newReq(path))
			if err != nil {
				t.Fatal(err)
			}
			body, err := ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(body), "contentof:"+path; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		}()
	}
	wg.Wait()
}

type memCache struct {
	logf func(format string, v ...interface{})

	mu sync.Mutex
	m  map[string]string
}

func (m *memCache) get(ctx context.Context, dst io.Writer, url string) (bool, error) {
	m.mu.Lock()
	v, found := m.m[url]
	m.mu.Unlock()

	m.logf("memCache.get(%q) found=%v", url, found)
	if !found {
		return false, nil
	}
	_, err := io.WriteString(dst, v)
	return true, err
}

func (m *memCache) put(ctx context.Context, url string, src io.Reader, srcLen int64) error {
	v, err := ioutil.ReadAll(src)
	m.logf("memCache.put(%q) len=%d", url, len(v))
	if err != nil {
		return err
	}
	if len(v) != int(srcLen) {
		return fmt.Errorf("memCache: put len=%d not equal to srcLen=%d", len(v), srcLen)
	}

	m.mu.Lock()
	m.m[url] = string(v)
	m.mu.Unlock()

	return nil
}

func newMemCache(logf func(format string, v ...interface{})) *memCache {
	return &memCache{
		m:    make(map[string]string),
		logf: logf,
	}
}
