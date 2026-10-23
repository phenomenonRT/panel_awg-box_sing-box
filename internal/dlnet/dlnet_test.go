package dlnet

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"corepanel/internal/model"
)

func get(t *testing.T, d model.Download, url string) (string, error) {
	tr, err := Transport(d)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Transport: tr}).Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

func TestTransportErrors(t *testing.T) {
	if _, err := Transport(model.Download{Via: model.ViaCore}); err == nil {
		t.Error("via=core без mixed-порта должен давать ошибку")
	}
	if _, err := Transport(model.Download{Via: model.ViaIface, Interface: "bad name!"}); err == nil {
		t.Error("неверное имя интерфейса должно давать ошибку")
	}
	if _, err := Transport(model.Download{Via: model.ViaProxy, Proxy: "::"}); err == nil {
		t.Error("неверный прокси должен давать ошибку")
	}
}

func TestViaCoreUsesProxy(t *testing.T) {
	px := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "via-proxy") }))
	defer px.Close()
	var p int
	for _, c := range px.Listener.Addr().String()[len("127.0.0.1:"):] {
		p = p*10 + int(c-'0')
	}
	got, err := get(t, model.Download{Via: model.ViaCore, CorePort: p}, "http://example.invalid/x")
	if err != nil || got != "via-proxy" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestViaIfaceLoopback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	got, err := get(t, model.Download{Via: model.ViaIface, Interface: "lo"}, srv.URL)
	if err != nil {
		t.Skip("нет прав на SO_BINDTODEVICE: ", err)
	}
	if got != "ok" {
		t.Fatalf("got %q", got)
	}
	if _, err := get(t, model.Download{Via: model.ViaIface, Interface: "nonexist0"}, srv.URL); err == nil {
		t.Error("несуществующий интерфейс должен давать ошибку")
	}
}
