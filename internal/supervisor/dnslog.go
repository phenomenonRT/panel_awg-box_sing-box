package supervisor

// Журнал DNS-запросов: собирается из журнала ядра (уровень логов debug). Ядро пишет запрос
// несколькими строками с общим номером соединения:
//
//	[id 0ms] inbound/tun[tun-in]: inbound DNS packet from 192.168.1.168:61337   — кто спросил
//	[id 0ms] dns: exchange c.bing.com. IN A                                       — что спросил
//	[id 84ms] dns: exchanged c.bing.com NOERROR 14                                — код ответа
//	[id 84ms] dns: exchanged A host. 14 IN A 150.171.27.10                        — ответы
//
// Строки объединяются по номеру, получается запись «устройство → имя → результат».

import (
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DNSEntry — один DNS-запрос.
type DNSEntry struct {
	Seq     int64    `json:"seq"`
	Time    int64    `json:"t"` // unix, мс
	Client  string   `json:"client"`
	Domain  string   `json:"domain"`
	Type    string   `json:"type"`
	Rcode   string   `json:"rcode,omitempty"` // NOERROR, NXDOMAIN, rejected…; пусто — ответа ещё нет
	Cached  bool     `json:"cached,omitempty"`
	Ms      int64    `json:"ms,omitempty"`
	Answers []string `json:"answers,omitempty"`
	id      string
}

// DNSClient — устройство, от которого были запросы.
type DNSClient struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
	Last  int64  `json:"last"`
}

var (
	dnsIDRe     = regexp.MustCompile(`\[(\d+) ([^\]]*)\]`)
	dnsClientRe = regexp.MustCompile(`inbound DNS packet from (\S+)$`)
	dnsAskRe    = regexp.MustCompile(`dns: exchange (\S+) IN (\S+)$`)
	dnsDoneRe   = regexp.MustCompile(`dns: (exchanged|cached) (\S+) ([A-Z]+) (\d+)$`)
	dnsAnsRe    = regexp.MustCompile(`dns: (?:exchanged|cached) (A|AAAA|CNAME) \S+ \d+ IN (?:A|AAAA|CNAME) (\S+)$`)
	dnsStampRe  = regexp.MustCompile(`^([+-]\d{4}) (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)`)
)

// DNSLog хранит последние запросы в кольцевом буфере.
type DNSLog struct {
	mu      sync.Mutex
	max     int
	seq     int64
	entries []*DNSEntry
	byID    map[string]*DNSEntry
	client  map[string]string // номер соединения → адрес устройства
	order   []string          // порядок появления номеров, для ограничения памяти
}

func NewDNSLog(max int) *DNSLog {
	return &DNSLog{max: max, byID: map[string]*DNSEntry{}, client: map[string]string{}}
}

func lineTime(line string) int64 {
	if m := dnsStampRe.FindStringSubmatch(line); m != nil {
		if t, err := time.Parse("-0700 2006-01-02 15:04:05", m[1]+" "+m[2]); err == nil {
			return t.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}

func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// Add разбирает одну строку журнала ядра.
func (d *DNSLog) Add(line string) {
	if !strings.Contains(line, "DNS packet from") && !strings.Contains(line, "dns: ") {
		return
	}
	idm := dnsIDRe.FindStringSubmatch(line)
	if idm == nil {
		return
	}
	id := idm[1]
	d.mu.Lock()
	defer d.mu.Unlock()

	if m := dnsClientRe.FindStringSubmatch(line); m != nil {
		d.client[id] = hostOnly(m[1])
		d.order = append(d.order, id)
		if len(d.order) > 4*d.max {
			for _, old := range d.order[:2*d.max] {
				delete(d.client, old)
				delete(d.byID, old)
			}
			d.order = append([]string(nil), d.order[2*d.max:]...)
		}
		return
	}
	if m := dnsAskRe.FindStringSubmatch(line); m != nil {
		d.seq++
		e := &DNSEntry{Seq: d.seq, Time: lineTime(line), Client: d.client[id],
			Domain: strings.TrimSuffix(m[1], "."), Type: m[2], id: id}
		d.entries = append(d.entries, e)
		if len(d.entries) > d.max {
			cut := len(d.entries) - d.max
			for _, old := range d.entries[:cut] {
				if d.byID[old.id] == old {
					delete(d.byID, old.id)
				}
			}
			d.entries = append([]*DNSEntry(nil), d.entries[cut:]...)
		}
		d.byID[id] = e
		return
	}
	e := d.byID[id]
	if e == nil {
		return
	}
	if m := dnsDoneRe.FindStringSubmatch(line); m != nil {
		e.Rcode, e.Cached = m[3], m[1] == "cached"
		if dur, err := time.ParseDuration(idm[2]); err == nil {
			e.Ms = dur.Milliseconds()
		}
		return
	}
	if m := dnsAnsRe.FindStringSubmatch(line); m != nil {
		if len(e.Answers) < 4 {
			e.Answers = append(e.Answers, strings.TrimSuffix(m[2], "."))
		}
		return
	}
	if strings.Contains(line, "dns: strategy rejected") {
		e.Rcode = "rejected"
	}
}

// Query возвращает запросы (новые первыми) с фильтром по устройству и тексту.
func (d *DNSLog) Query(client, text string, limit int) []DNSEntry {
	text = strings.ToLower(strings.TrimSpace(text))
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []DNSEntry
	for i := len(d.entries) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		e := d.entries[i]
		if client != "" && e.Client != client {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(e.Domain), text) && !strings.Contains(strings.ToLower(strings.Join(e.Answers, " ")), text) {
			continue
		}
		c := *e
		c.Answers = append([]string(nil), e.Answers...)
		out = append(out, c)
	}
	return out
}

// Clients перечисляет устройства, от которых есть запросы.
func (d *DNSLog) Clients() []DNSClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := map[string]*DNSClient{}
	var out []*DNSClient
	for _, e := range d.entries {
		c := idx[e.Client]
		if c == nil {
			c = &DNSClient{IP: e.Client}
			idx[e.Client] = c
			out = append(out, c)
		}
		c.Count++
		if e.Time > c.Last {
			c.Last = e.Time
		}
	}
	res := make([]DNSClient, len(out))
	for i, c := range out {
		res[i] = *c
	}
	return res
}

// Clear стирает журнал.
func (d *DNSLog) Clear() {
	d.mu.Lock()
	d.entries, d.byID, d.client, d.order = nil, map[string]*DNSEntry{}, map[string]string{}, nil
	d.mu.Unlock()
}
