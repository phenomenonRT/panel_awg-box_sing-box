// Package dlnet собирает http.Transport для загрузок панели (ядра, обновление панели, подписки,
// наборы правил) по выбранному в настройках маршруту: напрямую, через HTTP-прокси, через ядро
// (тогда маршрут выбирают правила маршрутизации) или через конкретный сетевой интерфейс.
package dlnet

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"corepanel/internal/model"
)

var ifaceRe = regexp.MustCompile(`^[A-Za-z0-9_.:@+-]{1,15}$`)

// ValidIface сообщает, похоже ли имя на имя сетевого интерфейса Linux.
func ValidIface(name string) bool { return ifaceRe.MatchString(name) }

// Transport возвращает транспорт для загрузок с учётом d.Via.
func Transport(d model.Download) (*http.Transport, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	switch d.Via {
	case model.ViaProxy:
		if p := strings.TrimSpace(d.Proxy); p != "" {
			pu, err := url.Parse(p)
			if err != nil || pu.Host == "" {
				return nil, fmt.Errorf("неверный адрес прокси «%s»", d.Proxy)
			}
			tr.Proxy = http.ProxyURL(pu)
		}
	case model.ViaCore:
		if d.CorePort <= 0 {
			return nil, fmt.Errorf("загрузка через ядро невозможна: в настройках ядра выключен mixed-порт")
		}
		pu, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(d.CorePort)))
		tr.Proxy = http.ProxyURL(pu)
	case model.ViaIface:
		if !ValidIface(d.Interface) {
			return nil, fmt.Errorf("для загрузки через интерфейс выберите его имя")
		}
		ctl, err := bindControl(d.Interface)
		if err != nil {
			return nil, err
		}
		tr.Proxy = nil
		tr.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: ctl}).DialContext
	}
	return tr, nil
}
