package core

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type slow struct {
	wait     time.Duration
	started  chan struct{}
	canceled chan bool
}

func (s *slow) Register(r gin.IRouter) {
	r.GET("/slow", func(c *gin.Context) {
		close(s.started)
		select {
		case <-time.After(s.wait):
			c.String(200, "done")
			s.canceled <- false
		case <-c.Request.Context().Done():
			s.canceled <- true
		}
	})
}

func serve(t *testing.T, route *slow, shutdown time.Duration) (string, context.CancelFunc, chan error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app := App{Modules: []*Module{{Name: "m", Providers: []any{*route}}}, Server: Server{ShutdownTimeout: shutdown}}
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, ln) }()
	return "http://" + ln.Addr().String(), cancel, done
}

func TestGracefulShutdown(t *testing.T) {
	route := &slow{wait: 300 * time.Millisecond, started: make(chan struct{}), canceled: make(chan bool, 1)}
	url, stop, done := serve(t, route, 5*time.Second)

	res := make(chan int, 1)
	go func() {
		r, err := http.Get(url + "/slow")
		if err != nil {
			res <- 0
			return
		}
		r.Body.Close()
		res <- r.StatusCode
	}()
	<-route.started
	stop()

	if code := <-res; code != 200 {
		t.Fatalf("in-flight request must finish: %d", code)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if _, err := http.Get(url + "/slow"); err == nil {
		t.Fatal("no new connections after shutdown")
	}
}

func TestShutdownTimeout(t *testing.T) {
	route := &slow{wait: time.Minute, started: make(chan struct{}), canceled: make(chan bool, 1)}
	url, stop, done := serve(t, route, 100*time.Millisecond)

	go http.Get(url + "/slow")
	<-route.started
	stop()

	if err := <-done; err == nil {
		t.Fatal("shutdown past the timeout must report an error")
	}
	if !<-route.canceled {
		t.Fatal("request context must be canceled when the server gives up")
	}
}

func TestReply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, tc := range map[string]struct {
		status int
		parts  []Part
		want   string
	}{
		"text":      {200, []Part{Text("ok")}, `{"text":"ok","code":1}`},
		"code":      {200, []Part{Code(WarningCode)}, `{"code":2}`},
		"text code": {200, []Part{Text("ok"), Code(DangerCode)}, `{"text":"ok","code":3}`},
		"data":      {201, []Part{Text("created"), Data(map[string]int{"id": 1})}, `{"text":"created","code":1,"data":{"id":1}}`},
		"by status": {404, []Part{Text("x")}, `{"text":"x","code":2}`},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		Reply(c, tc.status, tc.parts...)
		var got, want any
		json.Unmarshal(w.Body.Bytes(), &got)
		json.Unmarshal([]byte(tc.want), &want)
		if w.Code != tc.status || !equalJSON(got, want) {
			t.Errorf("%s: %d %s, want %s", name, w.Code, w.Body, tc.want)
		}
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

type tracked struct {
	log     *[]string
	failure error
}

func (t *tracked) Init(context.Context) error  { *t.log = append(*t.log, "init"); return t.failure }
func (t *tracked) Close(context.Context) error { *t.log = append(*t.log, "close"); return nil }
func (t *tracked) Register(r gin.IRouter) {
	r.GET("/t", func(c *gin.Context) { *t.log = append(*t.log, "request"); c.Status(200) })
}

func TestServeLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var log []string
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- App{Modules: []*Module{{Name: "m", Providers: []any{tracked{log: &log}}}}}.Serve(ctx, ln)
	}()

	var res *http.Response
	var err error
	for range 50 {
		if res, err = http.Get("http://" + ln.Addr().String() + "/t"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, ","); got != "init,request,close" {
		t.Fatalf("lifecycle order: %s", got)
	}

	log = nil
	ln, _ = net.Listen("tcp", "127.0.0.1:0")
	err = App{Modules: []*Module{{Name: "m", Providers: []any{tracked{log: &log, failure: errors.New("no config")}}}}}.Serve(context.Background(), ln)
	if err == nil || !strings.Contains(err.Error(), "no config") {
		t.Fatalf("a failed Init must stop the startup: %v", err)
	}
	if _, err := net.Dial("tcp", ln.Addr().String()); err == nil {
		t.Fatal("the listener must be closed when startup fails")
	}
}
