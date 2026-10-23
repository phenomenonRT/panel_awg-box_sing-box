package supervisor

import "testing"

func TestDNSLog(t *testing.T) {
	d := NewDNSLog(10)
	for _, l := range []string{
		"+0000 2026-10-23 09:17:47 INFO [2961671953 0ms] inbound/tun[tun-in]: inbound DNS packet from 192.168.1.168:54108",
		"+0000 2026-10-23 09:17:47 DEBUG [2961671953 0ms] dns: exchange c.bing.com. IN A",
		"+0000 2026-10-23 09:17:47 DEBUG [2961671953 84ms] dns: exchanged c.bing.com NOERROR 14",
		"+0000 2026-10-23 09:17:47 INFO [2961671953 84ms] dns: exchanged CNAME c.bing.com. 14 IN CNAME c-bing-com.ax-0001.ax-msedge.net.",
		"+0000 2026-10-23 09:17:47 INFO [2961671953 84ms] dns: exchanged A ax-0001.ax-msedge.net. 14 IN A 150.171.27.10",
		"+0000 2026-10-23 09:17:48 INFO [4011709913 0ms] inbound/tun[tun-in]: inbound DNS packet from 192.168.1.50:64375",
		"+0000 2026-10-23 09:17:48 DEBUG [4011709913 0ms] dns: exchange bx-ring.msedge.net. IN AAAA",
		"+0000 2026-10-23 09:17:48 DEBUG [4011709913 0ms] dns: strategy rejected",
		"+0000 2026-10-23 09:17:49 INFO [55070327 0ms] inbound/tun[tun-in]: inbound DNS packet from 192.168.1.168:63373",
		"+0000 2026-10-23 09:17:49 DEBUG [55070327 0ms] dns: exchange claude.com. IN A",
		"+0000 2026-10-23 09:17:49 DEBUG [55070327 0ms] dns: cached claude.com NOERROR 166",
		"+0000 2026-10-23 09:17:49 DEBUG [1496685244 64ms] dns: lookup domain api.github.com",
	} {
		d.Add(l)
	}
	all := d.Query("", "", 0)
	if len(all) != 3 || all[0].Domain != "claude.com" {
		t.Fatalf("all: %+v", all)
	}
	if !all[0].Cached || all[0].Rcode != "NOERROR" {
		t.Errorf("cached: %+v", all[0])
	}
	if all[1].Rcode != "rejected" || all[1].Client != "192.168.1.50" {
		t.Errorf("rejected: %+v", all[1])
	}
	b := all[2]
	if b.Client != "192.168.1.168" || b.Rcode != "NOERROR" || b.Ms != 84 || len(b.Answers) != 2 || b.Answers[1] != "150.171.27.10" || b.Time != 1792747067000 {
		t.Errorf("bing: %+v", b)
	}
	if got := d.Query("192.168.1.168", "", 0); len(got) != 2 {
		t.Errorf("filter: %d", len(got))
	}
	if got := d.Query("", "150.171", 0); len(got) != 1 {
		t.Errorf("text by answer: %d", len(got))
	}
	cl := d.Clients()
	if len(cl) != 2 || cl[0].IP != "192.168.1.168" || cl[0].Count != 2 {
		t.Errorf("clients: %+v", cl)
	}
	d.Clear()
	if len(d.Query("", "", 0)) != 0 {
		t.Error("clear")
	}
}
