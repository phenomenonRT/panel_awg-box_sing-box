package gen

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"corepanel/internal/model"
	"corepanel/internal/rulesets"
)

// Схема конфигурации: sing-box 1.12 и новее (DNS-серверы с полем type,
// rule actions, endpoints для WireGuard, rule_set вместо geoip/geosite).

// Singbox собирает config.json для sing-box.
func Singbox(s *model.Settings) (*Result, error) {
	p := buildPlan(s, model.CoreSingbox)
	res := &Result{Core: model.CoreSingbox, Filename: "config.json"}
	root := NewO()

	if s.General.LogLevel == "none" { // «без логов»
		root.Set("log", NewO().Set("disabled", true))
	} else {
		root.Set("log", NewO().Set("level", sbLogLevel(s.General.LogLevel)).Set("timestamp", true))
	}

	if s.DNS.Enabled {
		dns, err := sbDNS(s, p)
		if err != nil {
			return nil, err
		}
		root.Set("dns", dns)
	} else {
		root.Set("dns", sbDNSBasic(s, p))
	}

	root.Set("inbounds", sbInbounds(s))

	outbounds, endpoints := sbOutbounds(s, p)
	root.Set("outbounds", outbounds)
	if len(endpoints) > 0 {
		root.Set("endpoints", endpoints)
	}
	if len(p.sets) > 0 && s.Core == model.CoreAmnezia {
		// sing-box 1.14+: списки скачиваются через явный HTTP-клиент (неявный клиент по умолчанию устарел)
		// «direct» как detour ядро отвергает («detour to an empty direct outbound makes no sense»):
		// для прямой загрузки detour просто не задаётся.
		cl := NewO().Set("tag", "rules-dl")
		if v := s.General.UpdateVia; v != "" && v != "direct" && v != "block" {
			cl.Set("detour", v)
		}
		root.Set("http_clients", []any{cl})
	}
	root.Set("route", sbRoute(s, p))
	if s.Tun.Enabled && s.Tun.AutoRoute && s.Tun.StrictRoute && s.Tun.AutoRedirect {
		// в sing-tun при auto_redirect набор ip-правил строится в «режиме меток» и сразу возвращается,
		// блок strict_route (unreachable для семейства без адреса TUN) не выполняется
		p.warn("strict_route вместе с auto_redirect на Linux не блокирует IPv6 и не защищает, пока ядро остановлено: " +
			"правила TUN существуют только пока работает ядро. Если IPv6 не нужен, отключите его на роутере")
	}

	exp := NewO()
	clash := NewO().Set("external_controller", s.General.Controller).Opt("secret", s.General.Secret).Opt("external_ui", s.General.ExternalUI)
	if s.General.Controller != "" {
		exp.Set("clash_api", clash)
	}
	exp.Set("cache_file", NewO().Set("enabled", true).Set("path", "cache.db").Opt("store_fakeip", s.DNS.Enabled && s.DNS.Mode == "fakeip"))
	root.Set("experimental", exp)

	if len(s.OverrideSingbox) > 0 {
		Merge(root, s.OverrideSingbox)
	}

	b, err := JSON(root)
	if err != nil {
		return nil, err
	}
	res.Config = string(b)
	res.Warnings = p.warnings
	return res, nil
}

func sbLogLevel(l string) string {
	switch l {
	case "warning":
		return "warn"
	case "silent", "":
		return "warn"
	}
	return l
}

// ---------- DNS ----------

func sbDNSServer(tag string, a dnsAddr, detour string) *O {
	o := NewO().Set("type", a.Type).Set("tag", tag)
	if a.Type == "local" {
		return o
	}
	o.Set("server", a.Host)
	if a.Port > 0 {
		o.Set("server_port", a.Port)
	}
	if (a.Type == "https" || a.Type == "h3") && a.Path != "" && a.Path != "/dns-query" {
		o.Set("path", a.Path)
	}
	if !isIP(a.Host) {
		o.Set("domain_resolver", "bootstrap")
	}
	if detour != "" && detour != "direct" && detour != "block" {
		o.Set("detour", detour)
	}
	return o
}

// sbDNSBasic — минимальный DNS при выключенном DNS панели. Ядру всё равно нужно самому
// разрешать имена (проверка задержки по адресу с доменом, загрузка наборов правил, узлы с доменом),
// иначе системный резолвер роутера отвечает REFUSED. Запросы идут по IP-адресу bootstrap напрямую.
func sbDNSBasic(s *model.Settings, p *plan) *O {
	addr := s.DNS.Bootstrap
	if addr == "" {
		addr = s.DNS.Local
	}
	boot, err := parseDNSAddr(addr)
	if err != nil || boot.Type == "local" || !isIP(boot.Host) {
		boot = dnsAddr{Type: "udp", Host: "77.88.8.8"}
	}
	dns := NewO().Set("servers", []any{sbDNSServer("bootstrap", boot, "")}).Set("final", "bootstrap")
	if !s.General.IPv6 {
		dns.Set("strategy", "ipv4_only")
	}
	return dns
}

func sbDNS(s *model.Settings, p *plan) (*O, error) {
	dns := NewO()
	remote, err := parseDNSAddr(s.DNS.Remote)
	if err != nil {
		return nil, fmt.Errorf("DNS (remote): %w", err)
	}
	local, err := parseDNSAddr(s.DNS.Local)
	if err != nil {
		return nil, fmt.Errorf("DNS (local): %w", err)
	}
	bootAddr := s.DNS.Bootstrap
	if bootAddr == "" {
		bootAddr = s.DNS.Local
	}
	boot, err := parseDNSAddr(bootAddr)
	if err != nil || boot.Type == "local" || !isIP(boot.Host) {
		p.warn("DNS bootstrap должен быть IP-адресом — использован 77.88.8.8")
		boot = dnsAddr{Type: "udp", Host: "77.88.8.8"}
	}

	detour := s.DNS.RemoteDetour
	if detour == "" {
		detour = s.Final
	}
	// Local DNS можно провести через VPN. Имена самих узлов при этом разрешаются напрямую по IP bootstrap,
	// иначе узлу для подключения понадобился бы он сам.
	localDetour := s.DNS.LocalDetour
	nodeDNS := "local"
	if localDetour != "" && localDetour != "direct" && localDetour != "block" {
		if local.Type == "local" {
			p.warn("Local DNS «local» — это системный резолвер роутера, через VPN его провести нельзя; выберите IP-адрес")
			localDetour = ""
		} else {
			nodeDNS = "bootstrap"
		}
	}
	hostRules, hostsSrv := sbLocalHosts(s)
	servers := []any{
		sbDNSServer("bootstrap", boot, ""),
		sbDNSServer("local", local, localDetour),
		sbDNSServer("remote", remote, detour),
	}
	fake := s.DNS.Mode == "fakeip"
	if fake {
		f := NewO().Set("type", "fakeip").Set("tag", "fakeip").Set("inet4_range", s.DNS.FakeIPRange)
		if s.General.IPv6 {
			f.Set("inet6_range", "fc00::/18")
		}
		servers = append(servers, f)
	}
	if hostsSrv != nil {
		servers = append(servers, hostsSrv)
	}
	dns.Set("servers", servers)

	// Локальные домены — самыми первыми: их не должны перехватывать ни fakeip, ни пользовательские правила.
	rules := append([]any(nil), hostRules...)
	if doms := nodeServerDomains(s); len(doms) > 0 {
		rules = append(rules, NewO().Set("domain", doms).Set("action", "route").Set("server", nodeDNS))
	}
	for _, r := range p.dnsRules {
		rule := sbMatch(r)
		if rule == nil {
			continue
		}
		switch r.Outbound {
		case "block":
			rule.Set("action", "reject")
		default:
			rule.Set("action", "route").Set("server", r.Outbound)
		}
		rules = append(rules, rule)
	}
	if fake {
		qt := []string{"A"}
		if s.General.IPv6 {
			qt = append(qt, "AAAA")
		}
		rules = append(rules, NewO().Set("query_type", qt).Set("action", "route").Set("server", "fakeip"))
	}
	if len(rules) > 0 {
		dns.Set("rules", rules)
	}
	final := s.DNS.Final
	if final != "local" {
		final = "remote"
	}
	dns.Set("final", final)
	if !s.General.IPv6 {
		dns.Set("strategy", "ipv4_only")
	}
	return dns, nil
}

// sbLocalHosts строит DNS-правила для локальных доменов. Точные имена отдаёт сервер hosts,
// имена с поддоменами — правила predefined по типу запроса (A/AAAA).
func sbLocalHosts(s *model.Settings) ([]any, *O) {
	var exact []string
	predefined := NewO()
	var rules []any
	for _, h := range s.DNS.Hosts {
		if h.Disabled || h.Name == "" || len(h.IPs) == 0 {
			continue
		}
		var v4, v6 []string
		for _, ip := range h.IPs {
			if a := net.ParseIP(ip); a != nil {
				if a.To4() != nil {
					v4 = append(v4, a.String())
				} else {
					v6 = append(v6, a.String())
				}
			}
		}
		if !h.Subdomains {
			exact = append(exact, h.Name)
			predefined.Set(h.Name, h.IPs)
			continue
		}
		for _, t := range []struct {
			q   string
			ips []string
		}{{"A", v4}, {"AAAA", v6}} {
			r := NewO().Set("domain_suffix", []string{h.Name}).Set("query_type", []string{t.q}).
				Set("action", "predefined")
			if len(t.ips) == 0 {
				r.Set("rcode", "NOERROR")
			} else {
				var ans []string
				for _, ip := range t.ips {
					ans = append(ans, fmt.Sprintf("%s. 60 IN %s %s", h.Name, t.q, ip))
				}
				r.Set("answer", ans)
			}
			rules = append(rules, r)
		}
	}
	var srv *O
	if len(exact) > 0 {
		srv = NewO().Set("type", "hosts").Set("tag", "local-hosts").Set("predefined", predefined)
		rules = append([]any{NewO().Set("domain", exact).Set("action", "route").Set("server", "local-hosts")}, rules...)
	}
	return rules, srv
}

// sbMatch превращает правило в условие sing-box (без действия).
func sbMatch(r prule) *O {
	o := NewO()
	switch r.Type {
	case "domain", "domain_suffix", "domain_keyword":
		o.Set(r.Type, r.Values)
	case "domain_regex":
		o.Set("domain_regex", r.Values)
	case "ip_cidr":
		o.Set("ip_cidr", r.Values)
	case "src_ip_cidr":
		o.Set("source_ip_cidr", r.Values)
	case "rule_set":
		o.Set("rule_set", r.Values)
	case "port":
		o.Set("port", toInts(r.Values))
	case "port_range":
		o.Set("port_range", r.Values)
	case "network":
		o.Set("network", r.Values)
	case "protocol":
		o.Set("protocol", r.Values)
	default:
		return nil
	}
	return o
}

func toInts(v []string) []int {
	var out []int
	for _, s := range v {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// ---------- входящие ----------

func sbInbounds(s *model.Settings) []any {
	var in []any
	l := listen(s)
	if s.Inbounds.MixedPort > 0 {
		in = append(in, NewO().Set("type", "mixed").Set("tag", "mixed-in").Set("listen", l).Set("listen_port", s.Inbounds.MixedPort))
	}
	if s.Firewall.Mode == "redirect" && s.Inbounds.RedirPort > 0 {
		in = append(in, NewO().Set("type", "redirect").Set("tag", "redirect-in").Set("listen", "0.0.0.0").Set("listen_port", s.Inbounds.RedirPort))
	}
	if s.Firewall.Mode == "tproxy" && s.Inbounds.TProxyPort > 0 {
		in = append(in, NewO().Set("type", "tproxy").Set("tag", "tproxy-in").Set("listen", "0.0.0.0").Set("listen_port", s.Inbounds.TProxyPort))
	}
	if s.DNS.Enabled && s.DNS.Listen != "" {
		h, port := hostPort(s.DNS.Listen)
		if port > 0 {
			in = append(in, NewO().Set("type", "direct").Set("tag", "dns-in").Set("listen", h).Set("listen_port", port))
		}
	}
	if s.Tun.Enabled {
		t := NewO().Set("type", "tun").Set("tag", "tun-in").
			Set("interface_name", s.Tun.Name).
			Set("address", []string{s.Tun.Address}).
			Opt("mtu", s.Tun.MTU).
			Set("auto_route", s.Tun.AutoRoute).
			Opt("strict_route", s.Tun.StrictRoute).
			Opt("auto_redirect", s.Tun.AutoRedirect && s.Tun.AutoRoute)
		// amnezia-box (sing-box 1.15): параметр stack устарел, и его сборка идёт без gVisor —
		// «mixed»/«gvisor» уронили бы ядро. Не выводим stack: ядро использует стек по умолчанию.
		if s.Core != model.CoreAmnezia {
			t.Set("stack", s.Tun.Stack)
		}
		if len(s.Tun.ExcludeCIDR) > 0 {
			t.Set("route_exclude_address", s.Tun.ExcludeCIDR)
		}
		in = append(in, t)
	}
	for _, f := range s.Ifaces {
		if f.Disabled || f.Name == "" || f.Address == "" {
			continue
		}
		t := NewO().Set("type", "tun").Set("tag", ifaceTag(f.Name)).
			Set("interface_name", f.Name).
			Set("address", []string{strings.TrimSpace(f.Address)}).
			Opt("mtu", f.MTU).
			Set("auto_route", false)
		if s.Core != model.CoreAmnezia { // amnezia-box: stack не выводим (см. выше)
			t.Set("stack", s.Tun.Stack)
		}
		in = append(in, t)
	}
	return in
}

func ifaceTag(name string) string { return "if-" + name }

// ---------- исходящие ----------

func sbOutbounds(s *model.Settings, p *plan) ([]any, []any) {
	outs := []any{NewO().Set("type", "direct").Set("tag", "direct")}
	var eps []any
	active := map[string]bool{"direct": true}

	for _, n := range activeNodes(s) {
		active[n.Name] = true
		if n.Type == "wireguard" {
			eps = append(eps, sbWireGuard(s, n))
			continue
		}
		if n.Type == "awg" {
			eps = append(eps, sbAWG(s, n))
			continue
		}
		if o := sbNode(s, p, n); o != nil {
			outs = append(outs, o)
		}
	}
	for _, g := range s.Groups {
		active[g.Name] = true
	}
	for _, g := range s.Groups {
		var members []string
		for _, m := range g.Members {
			switch {
			case m == "block":
				p.warn("группа «%s»: «block» не может быть участником группы в sing-box — пропущено", g.Name)
			case !active[m]:
				p.warn("группа «%s»: участник «%s» отключён или не существует — пропущен", g.Name, m)
			default:
				members = append(members, m)
			}
		}
		if len(members) == 0 {
			members = []string{"direct"}
			p.warn("группа «%s» осталась пустой — добавлен direct", g.Name)
		}
		o := NewO().Set("tag", g.Name)
		switch g.Type {
		case "selector":
			o.Set("type", "selector").Set("outbounds", members)
			for _, member := range members {
				if g.Default == member {
					o.Set("default", g.Default)
					break
				}
			}
			o.Opt("interrupt_exist_connections", g.InterruptExistingConnections)
		case "fallback":
			// В sing-box нет отдельного fallback. urltest держит выбранный узел, пока его проверка
			// проходит, если другой не быстрее больше чем на tolerance; при огромном допуске он
			// никогда не переходит «на более быстрый», а после сбоя берёт первый живой узел по порядку.
			// 60000 мс, а не 65535: ядро складывает задержку и допуск в uint16 — переполнение сломало бы условие.
			iv := g.Interval
			if iv <= 0 {
				iv = 60
			}
			o.Set("type", "urltest").Set("outbounds", members).
				Set("url", orDefault(g.URL, "https://www.gstatic.com/generate_204")).
				Set("interval", durStr(iv)).
				Set("tolerance", fallbackTolerance)
			if g.IdleTimeout > 0 {
				o.Set("idle_timeout", durStr(g.IdleTimeout))
			}
			o.Opt("interrupt_exist_connections", g.InterruptExistingConnections)
		case "loadbalance", "urltest", "urltest_stable":
			if g.Type == "loadbalance" {
				p.warn("группа «%s»: тип %s в sing-box отсутствует — использован urltest", g.Name, g.Type)
			}
			tolerance := g.Tolerance
			if g.Type == "urltest_stable" && tolerance == 0 {
				tolerance = 500
			}
			o.Set("type", "urltest").Set("outbounds", members).
				Set("url", orDefault(g.URL, "https://www.gstatic.com/generate_204")).
				Set("interval", durStr(g.Interval)).
				Opt("tolerance", minInt(tolerance, fallbackTolerance))
			if g.IdleTimeout > 0 {
				o.Set("idle_timeout", durStr(g.IdleTimeout))
			}
			o.Opt("interrupt_exist_connections", g.InterruptExistingConnections)
		default:
			o.Set("type", "selector").Set("outbounds", members)
			p.warn("группа «%s»: неизвестный тип «%s», использован selector", g.Name, g.Type)
		}
		outs = append(outs, o)
	}
	return outs, eps
}

// fallbackTolerance — допуск urltest (мс), при котором выбранный узел не меняется на «более быстрый».
const fallbackTolerance = 60000

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func sbTLS(n model.Node) *O {
	if !hasTLS(n) {
		return nil
	}
	t := NewO().Set("enabled", true).Opt("server_name", orDefault(n.SNI, hostForSNI(n))).Opt("insecure", n.Insecure)
	alpn := n.ALPN
	if len(alpn) == 0 && (n.Type == "hysteria2" || n.Type == "tuic") {
		alpn = []string{"h3"}
	}
	t.Opt("alpn", alpn)
	if n.Type == "vless" || n.Type == "vmess" || n.Type == "trojan" {
		fp := n.Fingerprint
		if fp == "" && n.Reality {
			fp = "chrome"
		}
		if fp != "" {
			t.Set("utls", NewO().Set("enabled", true).Set("fingerprint", fp))
		}
	}
	if n.Reality {
		t.Set("reality", NewO().Set("enabled", true).Set("public_key", n.PublicKey).Opt("short_id", n.ShortID))
	}
	return t
}

func hostForSNI(n model.Node) string {
	if n.Host != "" && !isIP(n.Host) {
		return n.Host
	}
	if n.Server != "" && !isIP(n.Server) {
		return n.Server
	}
	return ""
}

func sbTransport(n model.Node) *O {
	switch n.Network {
	case "ws":
		t := NewO().Set("type", "ws").Opt("path", n.Path)
		if n.Host != "" {
			t.Set("headers", NewO().Set("Host", n.Host))
		}
		return t
	case "grpc":
		return NewO().Set("type", "grpc").Opt("service_name", n.ServiceName)
	case "h2":
		t := NewO().Set("type", "http")
		if n.Host != "" {
			t.Set("host", []string{n.Host})
		}
		return t.Opt("path", n.Path)
	case "httpupgrade":
		return NewO().Set("type", "httpupgrade").Opt("host", n.Host).Opt("path", n.Path)
	}
	return nil
}

func sbDial(o *O, s *model.Settings, n model.Node) {
	o.Opt("detour", n.Detour)
	o.Opt("bind_interface", n.BindInterface)
	// Метку (routing_mark) для узлов-интерфейсов не ставим: при TUN с автоперенаправлением ядро узнаёт свои
	// пакеты по собственной метке, и чужая метка отправляет их обратно в ядро. Маршрут выбирает правило по интерфейсу.
	o.Opt("tcp_fast_open", s.General.TCPFastOpen)
}

func sbNode(s *model.Settings, p *plan, n model.Node) *O {
	o := NewO().Set("tag", n.Name)
	srv := func() { o.Set("server", n.Server).Set("server_port", n.Port) }
	switch n.Type {
	case "vless":
		o.Set("type", "vless")
		srv()
		o.Set("uuid", n.UUID).Opt("flow", n.Flow).Set("packet_encoding", "xudp")
		o.Opt("tls", sbTLS(n)).Opt("transport", sbTransport(n))
	case "vmess":
		o.Set("type", "vmess")
		srv()
		o.Set("uuid", n.UUID).Set("security", orDefault(n.Method, "auto")).Set("alter_id", n.AlterID).Set("packet_encoding", "xudp")
		o.Opt("tls", sbTLS(n)).Opt("transport", sbTransport(n))
	case "trojan":
		o.Set("type", "trojan")
		srv()
		o.Set("password", n.Password).Opt("tls", sbTLS(n)).Opt("transport", sbTransport(n))
	case "shadowsocks":
		o.Set("type", "shadowsocks")
		srv()
		o.Set("method", n.Method).Set("password", n.Password)
	case "hysteria2":
		o.Set("type", "hysteria2")
		srv()
		o.Set("password", n.Password).Opt("up_mbps", n.UpMbps).Opt("down_mbps", n.DownMbps)
		if n.Obfs != "" {
			o.Set("obfs", NewO().Set("type", n.Obfs).Set("password", n.ObfsPassword))
		}
		o.Set("tls", sbTLS(n))
	case "tuic":
		o.Set("type", "tuic")
		srv()
		o.Set("uuid", n.UUID).Opt("password", n.Password).
			Set("congestion_control", orDefault(n.Congestion, "bbr")).
			Set("udp_relay_mode", orDefault(n.UDPRelayMode, "native")).
			Set("tls", sbTLS(n))
	case "socks":
		o.Set("type", "socks")
		srv()
		o.Set("version", "5").Opt("username", n.Username).Opt("password", n.Password)
	case "http":
		o.Set("type", "http")
		srv()
		o.Opt("username", n.Username).Opt("password", n.Password).Opt("tls", sbTLS(n))
	case "iface":
		// готовый туннель из системы (sstp, l2tp, pptp, openvpn, wg…): трафик выпускается через его интерфейс
		o.Set("type", "direct")
	default:
		p.warn("узел «%s»: тип «%s» не поддерживается генератором", n.Name, n.Type)
		return nil
	}
	sbDial(o, s, n)
	return o
}

func sbWireGuard(s *model.Settings, n model.Node) *O {
	allowed := n.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	peer := NewO().Set("address", n.Server).Set("port", n.Port).Set("public_key", n.PeerPublicKey).
		Opt("pre_shared_key", n.PreSharedKey).Set("allowed_ips", allowed).Opt("reserved", n.Reserved)
	o := NewO().Set("type", "wireguard").Set("tag", n.Name).
		Set("system", n.System).Opt("mtu", n.MTU).
		Set("address", n.Addresses).Set("private_key", n.PrivateKey).Set("peers", []any{peer})
	if n.System {
		o.Set("name", wgIfaceName(n.Name))
	}
	sbDial(o, s, n)
	return o
}

// sbAWG — endpoint AmneziaWG для amnezia-box (форк sing-box). Нулевые параметры
// обфускации не передаются, поэтому без них туннель совместим с обычным WireGuard.
func sbAWG(s *model.Settings, n model.Node) *O {
	allowed := n.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	// поля — по схеме amnezia-box: preshared_key (без подчёркивания), useIntegratedTun вместо system
	var keepalive any = 25
	if k := strings.TrimSpace(n.Keepalive); k != "" {
		keepalive = k
	}
	peer := NewO().Set("address", n.Server).Set("port", n.Port).Set("public_key", n.PeerPublicKey).
		Opt("preshared_key", n.PreSharedKey).Set("allowed_ips", allowed).Set("persistent_keepalive_interval", keepalive)
	o := NewO().Set("type", "awg").Set("tag", n.Name).
		Opt("useIntegratedTun", n.System).Opt("mtu", n.MTU).
		Set("address", n.Addresses).Set("private_key", n.PrivateKey)
	if a := n.AWG; a != nil {
		o.Opt("jc", a.Jc).Opt("jmin", a.Jmin).Opt("jmax", a.Jmax).
			Opt("s1", a.S1).Opt("s2", a.S2).Opt("s3", a.S3).Opt("s4", a.S4).
			Opt("h1", a.H1).Opt("h2", a.H2).Opt("h3", a.H3).Opt("h4", a.H4).
			Opt("i1", a.I1).Opt("i2", a.I2).Opt("i3", a.I3).Opt("i4", a.I4).Opt("i5", a.I5).
			Opt("header_protection_key", a.HeaderProtectionKey).Opt("content_padding_addition", a.ContentPadding).
			Opt("disable_cookies", a.DisableCookies).Opt("random_trailers", a.RandomTrailers).
			Opt("keepalive_timeout", a.KeepaliveTimeout).Opt("max_handshake_attempts", a.MaxHandshakeAttempts).
			Opt("reject_after_time", a.RejectAfterTime).Opt("rekey_after_time", a.RekeyAfterTime).Opt("rekey_timeout", a.RekeyTimeout)
	}
	o.Set("peers", []any{peer})
	sbDial(o, s, n)
	return o
}

func wgIfaceName(n string) string {
	out := unsafeName.ReplaceAllString(strings.ToLower(n), "")
	if len(out) > 15 {
		out = out[:15]
	}
	if out == "" {
		out = "wg0"
	}
	return out
}

// ---------- маршрутизация ----------

func sbRoute(s *model.Settings, p *plan) *O {
	r := NewO()
	var rules []any

	if s.DNS.Enabled && s.DNS.Listen != "" {
		if _, port := hostPort(s.DNS.Listen); port > 0 {
			rules = append(rules, NewO().Set("inbound", []string{"dns-in"}).Set("action", "hijack-dns"))
		}
	}
	// QUIC (UDP/443) отклоняем до sniff: разбор QUIC дорог для процессора роутера, а клиенты сами откатятся на TCP.
	if s.General.BlockQUIC {
		rules = append(rules, NewO().Set("network", []string{"udp"}).Set("port", []int{443}).Set("action", "reject"))
	}
	if s.General.Sniff {
		rules = append(rules, NewO().Set("action", "sniff"))
	}
	if s.DNS.Enabled { // при выключенном DNS ядро чужие DNS-запросы не перехватывает
		rules = append(rules, NewO().Set("protocol", "dns").Set("action", "hijack-dns"))
	}
	known := map[string]bool{"direct": true, "block": true}
	for _, n := range activeNodes(s) {
		known[n.Name] = true
	}
	for _, g := range s.Groups {
		known[g.Name] = true
	}
	for _, f := range s.Ifaces { // свой интерфейс: весь его трафик — в выбранный выход, раньше остальных правил
		if f.Disabled || f.Name == "" || f.Address == "" {
			continue
		}
		if !known[f.Outbound] {
			p.warn("интерфейс «%s»: выход «%s» отключён или не существует — интерфейс создан, но трафик пойдёт по общим правилам", f.Name, f.Outbound)
			continue
		}
		m := NewO().Set("inbound", []string{ifaceTag(f.Name)})
		if f.Outbound == "block" {
			m.Set("action", "reject")
		} else {
			m.Set("action", "route").Set("outbound", f.Outbound)
		}
		rules = append(rules, m)
	}
	// Загрузки самой панели (наборы правил, обновления) идут на mixed-порт ядра; без правила они попали бы
	// под Final. Если выбрано «Скачивать наборы правил через» — отправляем адреса источников туда же.
	if v := s.General.UpdateVia; v != "" && v != "direct" && v != "block" && known[v] {
		if hosts := updateHosts(s, p); len(hosts) > 0 {
			rules = append(rules, NewO().Set("domain", hosts).Set("action", "route").Set("outbound", v))
		}
	}
	if s.General.BypassLAN {
		rules = append(rules, NewO().Set("ip_is_private", true).Set("action", "route").Set("outbound", "direct"))
	}
	for _, pr := range p.rules {
		m := sbMatch(pr)
		if m == nil {
			continue
		}
		if pr.Outbound == "block" {
			m.Set("action", "reject")
		} else {
			m.Set("action", "route").Set("outbound", pr.Outbound)
		}
		rules = append(rules, m)
	}
	if s.Final == "block" {
		rules = append(rules, NewO().Set("network", []string{"tcp", "udp"}).Set("action", "reject"))
	}
	r.Set("rules", rules)

	if len(p.sets) > 0 {
		var sets []any
		for _, sp := range p.sets {
			if sp.Path != "" || sp.Format == "text" || sp.Interval <= 0 {
				localPath := sp.Path
				if localPath == "" {
					localPath = rulesets.LocalFile(model.RuleSet{Name: sp.Name, URL: sp.URL, Format: sp.Format})
				}
				localFormat := "binary"
				if sp.Format == "text" || sp.Format == "json" {
					localFormat = "source"
				}
				sets = append(sets, NewO().Set("type", "local").Set("tag", sp.Name).
					Set("format", localFormat).Set("path", localPath))
				continue
			}
			o := NewO().Set("type", "remote").Set("tag", sp.Name)
			if sp.Format == "json" {
				o.Set("format", "source")
			} else {
				o.Set("format", "binary")
			}
			o.Set("url", sp.URL)
			if s.Core == model.CoreAmnezia {
				o.Set("http_client", "rules-dl")
			} else if v := s.General.UpdateVia; v != "" && v != "direct" && v != "block" {
				o.Set("download_detour", v)
			}
			o.Set("update_interval", durStr(sp.Interval))
			sets = append(sets, o)
		}
		r.Set("rule_set", sets)
	}

	if s.Final != "block" {
		r.Set("final", s.Final)
	}
	if s.General.DefaultInterface != "" {
		r.Set("default_interface", s.General.DefaultInterface)
	} else if s.Tun.Enabled {
		r.Set("auto_detect_interface", true)
	}
	r.Set("default_domain_resolver", "bootstrap")
	return r
}

// updateHosts — адреса, с которых панель скачивает наборы правил, и зеркала GitHub.
func updateHosts(s *model.Settings, p *plan) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Hostname() == "" {
			return
		}
		h := strings.ToLower(u.Hostname())
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	for _, sp := range p.sets {
		add(sp.URL)
	}
	for _, m := range s.Download.Mirrors {
		add(m)
	}
	return out
}
