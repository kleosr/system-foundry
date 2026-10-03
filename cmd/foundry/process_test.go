package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const demoPassword = "local-demo-password"

type liveProcess struct {
	t        *testing.T
	server   *httptest.Server
	client   *http.Client
	stop     func()
	password string
}

func startLocal(t *testing.T, extra map[string]string) *liveProcess {
	t.Helper()
	env := map[string]string{"FOUNDRY_MODE": "local", "FOUNDRY_DEMO_PASSWORD": demoPassword, "FOUNDRY_RATE_LIMIT_PER_MINUTE": "1000"}
	for key, value := range extra {
		env[key] = value
	}
	return startProcess(t, "127.0.0.1:0", env)
}

func startProcess(t *testing.T, addr string, env map[string]string) *liveProcess {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	env["FOUNDRY_ORIGIN"] = "http://" + ln.Addr().String()
	cfg, err := loadConfig(envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := assemble(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	server := &httptest.Server{Listener: ln, Config: &http.Server{Handler: proc.edge.Handler()}}
	server.Start()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		server.Close()
		cancel()
		proc.cleanup()
	}
	t.Cleanup(stop)
	noRedirect := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &liveProcess{t: t, server: server, client: noRedirect, stop: stop, password: demoPassword}
}

func (p *liveProcess) do(method, path, token string, body string, headers map[string]string) (int, string) {
	p.t.Helper()
	req, err := http.NewRequest(method, p.server.URL+path, strings.NewReader(body))
	if err != nil {
		p.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(payload)
}

func (p *liveProcess) session(email string) *http.Cookie {
	p.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, p.server.URL+"/login", strings.NewReader(`{"email":"`+email+`","password":"`+p.password+`"}`))
	req.Header.Set("Origin", p.server.URL)
	resp, err := p.client.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "sid" {
			return cookie
		}
	}
	p.t.Fatalf("login %s = %d", email, resp.StatusCode)
	return nil
}

func (p *liveProcess) token(email string) string {
	p.t.Helper()
	verifier := strings.Repeat("v", 64)
	query := url.Values{
		"response_type": {"code"}, "client_id": {demoClientID}, "redirect_uri": {demoRedirect}, "state": {"s1"},
		"nonce": {"n1"}, "code_challenge": {pkceChallenge(verifier)}, "code_challenge_method": {"S256"},
	}
	req, _ := http.NewRequest(http.MethodGet, p.server.URL+"/authorize?"+query.Encode(), nil)
	req.AddCookie(p.session(email))
	resp, err := p.client.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	resp.Body.Close()
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		p.t.Fatalf("authorize = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {demoClientID}, "code": {location.Query().Get("code")},
		"redirect_uri": {demoRedirect}, "code_verifier": {verifier},
	}
	resp, err = p.client.PostForm(p.server.URL+"/token", form)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&tokens); err != nil || tokens.AccessToken == "" {
		p.t.Fatalf("token = %d err=%v", resp.StatusCode, err)
	}
	return tokens.AccessToken
}

func (p *liveProcess) openStream(email string) *bufio.Reader {
	p.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, p.server.URL+"/stream", nil)
	req.AddCookie(p.session(email))
	resp, err := (&http.Client{}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		p.t.Fatalf("stream err=%v", err)
	}
	p.t.Cleanup(func() { resp.Body.Close() })
	return bufio.NewReader(resp.Body)
}

func readUntil(t *testing.T, stream *bufio.Reader, needle string) {
	t.Helper()
	found := make(chan bool, 1)
	go func() {
		for {
			line, err := stream.ReadString('\n')
			if err != nil {
				found <- false
				return
			}
			if strings.Contains(line, needle) {
				found <- true
				return
			}
		}
	}()
	select {
	case ok := <-found:
		if !ok {
			t.Fatalf("stream closed before %q", needle)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %q", needle)
	}
}
