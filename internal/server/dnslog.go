package server

import (
	"bufio"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// leaseNames читает имена устройств из аренд DHCP (dnsmasq/odhcpd): адрес → имя хоста.
func leaseNames() map[string]string {
	names := map[string]string{}
	for _, path := range []string{"/tmp/dhcp.leases", "/var/lib/misc/dnsmasq.leases"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			// срок MAC адрес имя идентификатор-клиента
			p := strings.Fields(sc.Text())
			if len(p) >= 4 && p[3] != "*" {
				names[p[2]] = p[3]
			}
		}
		f.Close()
	}
	return names
}

// dnsQueries отдаёт журнал DNS-запросов и список устройств; ?client= выбирает одно устройство.
func (s *Server) dnsQueries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 3000 {
		limit = 500
	}
	names := leaseNames()
	type client struct {
		IP    string `json:"ip"`
		Name  string `json:"name,omitempty"`
		Count int    `json:"count"`
		Last  int64  `json:"last"`
	}
	var clients []client
	for _, c := range s.Sup.DNS.Clients() {
		clients = append(clients, client{IP: c.IP, Name: names[c.IP], Count: c.Count, Last: c.Last})
	}
	ok(w, map[string]any{
		"entries":   s.Sup.DNS.Query(q.Get("client"), q.Get("q"), limit),
		"clients":   clients,
		"debug":     s.St.Get().General.LogLevel == "debug",
		"log_level": s.St.Get().General.LogLevel,
	})
}

func (s *Server) dnsQueriesClear(w http.ResponseWriter, r *http.Request) {
	s.Sup.DNS.Clear()
	ok(w, map[string]any{"cleared": true})
}
