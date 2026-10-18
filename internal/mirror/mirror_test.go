package mirror

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"corepanel/internal/model"
)

func TestListMergesAndDedupes(t *testing.T) {
	d := model.Download{Mirror: " https://a.example/ ", Mirrors: []string{"https://b.example", "https://A.example/", "", "  "}}
	want := []string{"https://a.example", "https://b.example"}
	if got := List(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if List(model.Download{}) != nil {
		t.Fatal("пустые настройки — пустой список")
	}
}

func TestVariants(t *testing.T) {
	gh := "https://github.com/o/r/releases/download/v1/x"
	got := Variants([]string{"https://m1.example", "https://m2.example"}, gh)
	want := []string{"https://m1.example/" + gh, "https://m2.example/" + gh, gh}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if v := Variants([]string{"https://m1.example"}, "https://example.com/x"); len(v) != 1 || v[0] != "https://example.com/x" {
		t.Fatalf("не GitHub идёт напрямую: %v", v)
	}
	if v := Variants(nil, gh); len(v) != 1 || v[0] != gh {
		t.Fatalf("без зеркал: %v", v)
	}
}

func TestValid(t *testing.T) {
	for s, ok := range map[string]bool{"https://ghfast.top/": true, "http://10.0.0.1:8080": true, "ghfast.top": false, "ftp://x.y": false, "": false} {
		if Valid(s) != ok {
			t.Errorf("Valid(%q) != %v", s, ok)
		}
	}
}

func TestCheck(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// зеркало: путь — это полный адрес GitHub
		if !strings.HasPrefix(r.URL.Path, "/https://raw.githubusercontent.com/") {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte("# readme\ntext"))
	}))
	defer good.Close()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<!DOCTYPE html><html><body>captcha</body></html>"))
	}))
	defer page.Close()
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()

	res := Check(context.Background(), model.Download{}, []string{good.URL, page.URL, notFound.URL})
	if len(res) != 4 || res[0].Mirror != "" {
		t.Fatalf("первой строкой должен идти прямой доступ: %+v", res)
	}
	if !res[1].OK {
		t.Errorf("рабочее зеркало: %+v", res[1])
	}
	if res[2].OK || !strings.Contains(res[2].Error, "веб-страница") {
		t.Errorf("страница вместо файла: %+v", res[2])
	}
	if res[3].OK || res[3].Error != "HTTP 404" {
		t.Errorf("404: %+v", res[3])
	}
}
