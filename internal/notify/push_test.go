package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withFakeClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	saved := httpClient
	httpClient = srv.Client()
	t.Cleanup(func() { httpClient = saved })
}

func TestPush_OffIsNoOp(t *testing.T) {
	if err := Push(PushOptions{Provider: "off", Topic: "x"}, "hi", "body"); err != nil {
		t.Fatalf("off should be silent, got %v", err)
	}
	if err := Push(PushOptions{}, "hi", "body"); err != nil {
		t.Fatalf("empty provider should be silent, got %v", err)
	}
}

func TestPush_UnknownProvider(t *testing.T) {
	if err := Push(PushOptions{Provider: "pushover", Topic: "x"}, "hi", "body"); err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestPushNtfy_PostsTitleAndBody(t *testing.T) {
	var gotPath, gotTitle, gotAuth, gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("Title")
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withFakeClient(t, srv)

	err := Push(PushOptions{
		Provider: "ntfy",
		URL:      srv.URL,
		Topic:    "c9s-stefano-test",
		Token:    "tok",
	}, "Hello", "claude is waiting")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/c9s-stefano-test" {
		t.Errorf("path = %q, want /c9s-stefano-test", gotPath)
	}
	if gotTitle != "Hello" {
		t.Errorf("Title header = %q, want Hello", gotTitle)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", gotAuth)
	}
	if gotBody != "claude is waiting" {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.HasPrefix(gotCT, "text/plain") {
		t.Errorf("content type = %q", gotCT)
	}
}

func TestPushNtfy_TrailingSlashURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withFakeClient(t, srv)

	err := Push(PushOptions{Provider: "ntfy", URL: srv.URL + "/", Topic: "abc"}, "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/abc" {
		t.Errorf("trailing slash not stripped: %q", gotPath)
	}
}

func TestPushNtfy_BasicAuth(t *testing.T) {
	var gotAuthUser, gotAuthPass string
	var gotOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthUser, gotAuthPass, gotOK = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withFakeClient(t, srv)

	err := Push(PushOptions{
		Provider: "ntfy",
		URL:      srv.URL,
		Topic:    "t",
		User:     "stefano",
		Password: "hunter2",
	}, "title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if !gotOK {
		t.Fatal("server did not parse Basic auth header")
	}
	if gotAuthUser != "stefano" || gotAuthPass != "hunter2" {
		t.Errorf("basic auth = %q/%q, want stefano/hunter2", gotAuthUser, gotAuthPass)
	}
}

func TestPushNtfy_BearerWinsOverBasic(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withFakeClient(t, srv)

	err := Push(PushOptions{
		Provider: "ntfy",
		URL:      srv.URL,
		Topic:    "t",
		Token:    "tok",
		User:     "u",
		Password: "p",
	}, "title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok (token must win)", gotAuth)
	}
}

func TestPushNtfy_EmptyTopic(t *testing.T) {
	if err := Push(PushOptions{Provider: "ntfy"}, "x", "y"); err == nil {
		t.Fatal("expected error for empty topic")
	}
}

func TestPushNtfy_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withFakeClient(t, srv)

	if err := Push(PushOptions{Provider: "ntfy", URL: srv.URL, Topic: "t"}, "x", "y"); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPushNtfy_EmptyTitleAndBodyIsNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withFakeClient(t, srv)

	if err := Push(PushOptions{Provider: "ntfy", URL: srv.URL, Topic: "t"}, "", ""); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("ntfy was hit despite empty payload")
	}
}
