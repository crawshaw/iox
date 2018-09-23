// Package webfetch is an *http.Client wrapper with caching and logging.
package webfetch

import (
	"context"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"

	"crawshaw.io/iox"
)

type Client struct {
	Filer    *iox.Filer
	Client   *http.Client
	Logf     func(format string, v ...interface{})
	CacheGet func(ctx context.Context, dst io.Writer, url string) (bool, error)
	CachePut func(ctx context.Context, url string, src io.Reader, srcLen int64) error

	initOnce sync.Once // initializes the remaining fields with the init method

	ctx          context.Context
	shutdown     chan struct{}
	shutdownDone chan struct{}

	mu       sync.Mutex
	cancelFn func()
	fetchers map[string]*fetcher
}

func (c *Client) init() {
	c.ctx, c.cancelFn = context.WithCancel(context.Background())
	c.shutdown = make(chan struct{})
	c.shutdownDone = make(chan struct{})
	c.fetchers = make(map[string]*fetcher)
}

func (c *Client) Shutdown(ctx context.Context) {
	c.initOnce.Do(c.init)

	c.mu.Lock()
	close(c.shutdown)
	if len(c.fetchers) == 0 {
		close(c.shutdownDone)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-c.shutdownDone:
	}

	c.mu.Lock()
	c.cancelFn()
	c.mu.Unlock()

	// Wait the briefest of moments of cancelFn to propagate.
	select {
	case <-c.shutdownDone:
		return
	case <-time.After(10 * time.Millisecond):
	}

	// Force-close any leftover response bodies.
	c.mu.Lock()
	for url, f := range c.fetchers {
		if c.Logf != nil {
			c.Logf(
				`{"where": "webfetch", "what": "force_shutdown", "name": %q, "when": %q}`,
				url, time.Now(),
			)
		}
		f.reqs = 0
		f.reqCleanupLocked()
	}
	c.mu.Unlock()

	<-c.shutdownDone
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	c.initOnce.Do(c.init)

	select {
	case <-c.shutdown:
		return nil, context.Canceled
	default:
	}

	urlstr := req.URL.String()

	c.mu.Lock()
	select {
	case <-c.shutdown:
		c.mu.Unlock()
		return nil, context.Canceled
	case <-c.ctx.Done():
		c.mu.Unlock()
		return nil, context.Canceled
	default:
	}
	f := c.fetchers[urlstr]
	if f == nil {
		f = &fetcher{
			c:    c,
			url:  urlstr,
			reqs: 1,
			f:    c.Filer.BufferFile(0),
			done: make(chan struct{}),
		}
		c.fetchers[urlstr] = f
		go f.fetch(req)
	} else {
		f.reqs++
	}
	c.mu.Unlock()

	<-f.done
	return f.response(req)
}

type fetcher struct {
	c   *Client
	url string

	reqs int // guarded by c.mu

	f *iox.BufferFile // owned by fetch until done is closed

	done chan struct{}
	res  *http.Response
	err  error
}

func (f *fetcher) fetchFromCache() {
	if f.c.CacheGet == nil {
		return // cache disabled
	}
	found, err := f.c.CacheGet(f.c.ctx, f.f, f.url)
	if err != nil {
		f.err = err
		close(f.done)
		return
	}
	if found {
		close(f.done)
	}
}

func (f *fetcher) saveToCache() {
	if f.c.CachePut == nil {
		return // cache disabled
	}

	f.err = f.c.CachePut(f.c.ctx, f.url, io.NewSectionReader(f.f, 0, f.f.Len()), f.f.Len())
}

func (f *fetcher) fetch(req *http.Request) {
	// First see if the result is already cached.
	f.fetchFromCache()
	select {
	case <-f.done:
		return
	default:
	}

	// URL is not in our cache, so time to fetch from the web.
	req = req.WithContext(f.c.ctx)
	start := time.Now()
	f.res, f.err = f.c.Client.Do(req)
	duration := time.Since(start)

	if f.err == nil {
		_, f.err = io.Copy(f.f, f.res.Body)
		if err := f.res.Body.Close(); f.err == nil {
			f.err = err
		}
	}
	if f.c.Logf != nil {
		sc := 0
		if f.res != nil {
			sc = f.res.StatusCode
		}
		f.c.Logf(
			`{"where": "webfetch", "what": "fetch", "name": %q, "when": %q, "duration": %q, "status": %d, "len": %d}`,
			req.URL.String(), start, duration, sc, f.f.Len(),
		)
	}

	if f.err == nil && f.res.StatusCode == 200 {
		f.saveToCache()
	}
	close(f.done)
}

// response creates an http response
//
// This function is responsible for decrementing f.reqs.
// Either it must do so explicitly, or it must pass the
// responsibility on.
func (f *fetcher) response(req *http.Request) (*http.Response, error) {
	if f.err != nil {
		f.reqCleanup()
		return nil, f.err
	}

	var res http.Response
	if f.res == nil {
		// cache hit, fake a http.Response
	} else {
		res = *f.res
	}

	frc := &fetchReadCloser{
		Reader: io.NewSectionReader(f.f, 0, f.f.Len()),
		f:      f,
	}
	runtime.SetFinalizer(frc, func(frc *fetchReadCloser) {
		if frc.f != nil {
			panic("webfetch: http.Response.Body for " + frc.f.url + " not closed")
		}
	})

	res.Request = req
	res.Body = frc
	return &res, nil
}

func (f *fetcher) reqCleanup() error {
	f.c.mu.Lock()
	defer f.c.mu.Unlock()
	if f.reqs == 0 {
		// Happens if the response body is closed after a forced shutdown.
		return context.Canceled
	}
	f.reqs--
	f.reqCleanupLocked()
	return nil
}

// reqCleanupLocked requires f.c.mu be held
func (f *fetcher) reqCleanupLocked() {
	if f.reqs != 0 {
		return
	}
	f.f.Close()
	delete(f.c.fetchers, f.url)
	if len(f.c.fetchers) == 0 {
		select {
		case <-f.c.shutdown:
			close(f.c.shutdownDone)
		default:
		}
	}
}

type fetchReadCloser struct {
	io.Reader
	f *fetcher
}

func (frc *fetchReadCloser) Close() (err error) {
	if frc.f != nil {
		err = frc.f.reqCleanup()
		frc.f = nil
	}
	return err
}
