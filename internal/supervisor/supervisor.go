// Package supervisor запускает ядро (amnezia-box) и следит за ним.
package supervisor

import (
	"context"
	"corepanel/internal/kmods"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"corepanel/internal/firewall"
	"corepanel/internal/gen"
	"corepanel/internal/installer"
	"corepanel/internal/mirror"
	"corepanel/internal/model"
	"corepanel/internal/platform"
	"corepanel/internal/rulesets"
)

// LogBuffer — кольцевой буфер строк лога с подпиской.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
	subs  map[chan string]struct{}
	hooks []func(string)
}

// Hook регистрирует обработчик каждой новой строки (вызывается синхронно, должен быть быстрым).
func (l *LogBuffer) Hook(f func(string)) {
	l.mu.Lock()
	l.hooks = append(l.hooks, f)
	l.mu.Unlock()
}

func NewLogBuffer(max int) *LogBuffer {
	return &LogBuffer{max: max, subs: map[chan string]struct{}{}}
}

func (l *LogBuffer) Add(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
	for ch := range l.subs {
		select {
		case ch <- line:
		default: // медленный подписчик не должен тормозить ядро
		}
	}
	hooks := l.hooks
	l.mu.Unlock()
	for _, f := range hooks {
		f(line)
	}
}

func (l *LogBuffer) Tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.lines) {
		n = len(l.lines)
	}
	return append([]string(nil), l.lines[len(l.lines)-n:]...)
}

func (l *LogBuffer) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 256)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
}

// Status — состояние сервиса.
type Status struct {
	State         string   `json:"state"` // stopped | running | failed
	Core          string   `json:"core"`
	PID           int      `json:"pid"`
	UptimeSec     int64    `json:"uptime_sec"`
	MemMB         int      `json:"mem_mb,omitempty"` // реально занятая память ядра (VmRSS из /proc)
	Error         string   `json:"error,omitempty"`
	FirewallError string   `json:"firewall_error,omitempty"`
	Restarts      int      `json:"restarts"`
	Version       string   `json:"version,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Notice        string   `json:"notice,omitempty"` // не ошибка: например, ожидание сети
}

// Supervisor управляет процессом ядра.
type Supervisor struct {
	Info platform.Info
	Inst *installer.Installer
	Logs *LogBuffer
	DNS  *DNSLog // журнал DNS-запросов, собранный из журнала ядра

	mu                 sync.Mutex
	rulesetMu          sync.Mutex
	cmd                *exec.Cmd
	done               chan struct{}
	core               string
	startedAt          time.Time
	lastErr            string
	fwErr              string
	fw                 *firewall.Scripts
	ifw                *firewall.Scripts // маршруты устройств в свои интерфейсы
	nfw                *firewall.Scripts // маршруты узлов «Системный VPN (интерфейс)»
	v6                 *firewall.Scripts // блокировка IPv6 на время работы ядра
	desired            bool
	restarts           int
	waitNet            bool // ядро не стартовало из-за отсутствия сети: повторяем запуск без красной ошибки
	netLoop            bool
	waitSince          time.Time
	waitWhy            string
	warnings           []string
	version            string
	getSet             func() *model.Settings
	interfaceRestarter func(context.Context, string) error
	bin                string // путь к запущенному ядру (для передачи новой панели)
	logFile            string // файл вывода ядра

	// автовосстановление при затяжном сбое рукопожатий (см. recovery.go)
	recovering    bool           // перезапуск уже идёт: второй не начинаем
	recoverTries  int            // сколько автоперезапусков подряд уже сделано
	recoverNote   string         // предупреждение автовосстановления (показывается в статусе)
	awgFails      map[string]int // неудачные рукопожатия подряд по узлам endpoint/awg[...]
	ifaceFails    map[string]int // последовательные проверки интерфейса или ответа системного VPN
	ifaceRestarts map[string]int // неудачные переподключения системного VPN по интерфейсам
}

// New создаёт супервизор. getSettings вызывается при автоперезапуске.
func New(info platform.Info, inst *installer.Installer, getSettings func() *model.Settings) *Supervisor {
	s := &Supervisor{Info: info, Inst: inst, Logs: NewLogBuffer(2000), DNS: NewDNSLog(3000), getSet: getSettings}
	s.Logs.Hook(func(line string) {
		if !strings.Contains(line, "dns: ") && !strings.Contains(line, "DNS packet from") {
			return // быстрый отсев: остальные строки журнала не разбираются
		}
		if set := s.getSet(); set != nil && set.General.DNSLog {
			s.DNS.Add(line)
		}
	})
	go s.watchHandshakes()
	return s
}

// SetInterfaceRestarter задаёт системный способ переподключения VPN-интерфейса.
// В OpenWrt сервер подключает сюда netifd; ядро Amnezia-box при этом не перезапускается.
func (s *Supervisor) SetInterfaceRestarter(restart func(context.Context, string) error) {
	s.mu.Lock()
	s.interfaceRestarter = restart
	s.mu.Unlock()
}

// RuntimeDir — рабочий каталог ядра.
func (s *Supervisor) RuntimeDir(core string) string {
	return filepath.Join(s.Info.DataDir, "runtime", core)
}

// RuleSetsRAMDir — временный каталог наборов. На роутерах /tmp находится в RAM.
func (s *Supervisor) RuleSetsRAMDir(core string) string {
	return filepath.Join(os.TempDir(), "corepanel", "rulesets", core)
}

func (s *Supervisor) ruleSetsRAMDir(set *model.Settings, core string) string {
	if set == nil || !set.General.RuleSetsRAM {
		return ""
	}
	return s.RuleSetsRAMDir(core)
}

func settingsWithRuleSetPaths(set *model.Settings, ramDir string) *model.Settings {
	copy := *set
	copy.RuleSets = append([]model.RuleSet(nil), set.RuleSets...)
	for i := range copy.RuleSets {
		rs := &copy.RuleSets[i]
		if (rulesets.IsText(*rs) && ramDir != "") || rs.Interval <= 0 {
			rs.LocalPath = rulesets.LocalFile(*rs)
			if ramDir != "" {
				rs.LocalPath = filepath.Join(ramDir, filepath.FromSlash(rulesets.LocalFile(*rs)))
			}
		}
	}
	return &copy
}

// Render генерирует конфиг для выбранного ядра.
func Render(set *model.Settings) (*gen.Result, error) {
	ramDir := ""
	if set.General.RuleSetsRAM {
		ramDir = filepath.Join(os.TempDir(), "corepanel", "rulesets", set.Core)
	}
	set = settingsWithRuleSetPaths(set, ramDir)
	if issues := set.Validate(); hasError(issues) {
		var msgs []string
		for _, i := range issues {
			if i.Level == "error" {
				msgs = append(msgs, i.Message)
			}
		}
		return nil, fmt.Errorf("в настройках есть ошибки: %s", strings.Join(msgs, "; "))
	}
	note := ""
	if set.Tun.Enabled && set.Tun.AutoRedirect && !queueAvailable() {
		cp := *set
		cp.Tun.AutoRedirect = false
		set = &cp
		note = "auto_redirect отключён автоматически: в ядре нет модуля nfqueue (установите kmod-nft-queue, чтобы использовать его)"
	}
	res, err := gen.Singbox(set)
	if err == nil && note != "" {
		res.Warnings = append(res.Warnings, note)
	}
	return res, err
}

var queueAvailable = kmods.QueueAvailable

func hasError(is []model.Issue) bool {
	for _, i := range is {
		if i.Level == "error" {
			return true
		}
	}
	return false
}

// Start генерирует конфиг, проверяет его и запускает ядро.
func (s *Supervisor) Start(set *model.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(set, true)
}

func (s *Supervisor) running() bool {
	if s.cmd == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *Supervisor) startLocked(set *model.Settings, userInitiated bool) error {
	if s.running() {
		return errors.New("ядро уже запущено")
	}
	bin := s.Inst.Locate(set.Core, set.Download)
	if bin == "" {
		return fmt.Errorf("ядро %s не установлено — установите его в разделе «Компоненты»", installer.BinName(set.Core))
	}
	// проверка готовности: ждём WAN и интерфейсы системных VPN, а не запускаем ядро «вслепую»
	if reasons, hard := preflight(set); len(reasons) > 0 {
		if !s.waitNet {
			s.waitSince = time.Now()
		}
		limit := 60 * time.Second // системный VPN может не подняться вовсе — не держим ядро вечно
		if hard {
			limit = 10 * time.Minute
		}
		why := strings.Join(reasons, "; ")
		if time.Since(s.waitSince) < limit {
			s.desired, s.lastErr, s.waitNet, s.waitWhy = false, "", true, why
			if !s.netLoop {
				s.Logs.Add("[panel] ждём перед запуском: " + why + " (проверка каждые 10 с)")
				s.netLoop = true
				go s.waitNetwork()
			}
			return nil
		}
		s.Logs.Add("[panel] запускаю, не дожидаясь: " + why)
	}
	needTun := set.Tun.Enabled
	for _, f := range set.Ifaces {
		if !f.Disabled {
			needTun = true
		}
	}
	if needTun {
		if err := ensureTun(); err != nil {
			s.lastErr = err.Error()
			s.Logs.Add("[panel] " + s.lastErr)
			return err
		}
	}
	ramDir := s.ruleSetsRAMDir(set, set.Core)
	if ramDir == "" {
		// При выключении режима убираем оставшуюся RAM-копию, чтобы при повторном включении
		// она не затенила более свежий файл из постоянного каталога.
		_ = os.RemoveAll(s.RuleSetsRAMDir(set.Core))
	}
	res, err := Render(set)
	if err != nil {
		s.lastErr = err.Error()
		return err
	}
	dir := s.RuntimeDir(set.Core)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	s.rulesetMu.Lock()
	ruleSetWarnings := rulesets.PrepareWithRAM(context.Background(), dir, ramDir, set.RuleSets, startupFetcher(s.rulesetFetcher(set, 0)), false)
	s.rulesetMu.Unlock()
	for _, w := range ruleSetWarnings {
		res.Warnings = append(res.Warnings, w)
		s.Logs.Add("[panel] " + w)
	}
	cfgPath := filepath.Join(dir, res.Filename)
	if err := writeAtomic(cfgPath, []byte(res.Config), 0o600); err != nil {
		return err
	}

	if out, err := checkConfig(set.Core, bin, dir, cfgPath); err != nil {
		s.lastErr = "ядро отклонило конфигурацию: " + out
		s.Logs.Add("[panel] проверка конфигурации не пройдена: " + out)
		return errors.New(s.lastErr)
	}

	if msg, err := applyConntrackMax(set.General.ConntrackMax); err != nil {
		// не критично: ядро работает и с прежним пределом, поэтому только предупреждаем
		s.Logs.Add("[panel] предел conntrack не применён: " + err.Error())
		res.Warnings = append(res.Warnings, "предел conntrack не применён: "+err.Error())
	} else if msg != "" {
		s.Logs.Add("[panel] " + msg)
	}

	cmd := exec.Command(bin, "run", "-c", cfgPath, "-D", dir)
	// sing-box 1.14+ (включая amnezia-box на базе 1.15): разрешаем неявный HTTP-клиент для совместимости
	cmd.Env = append(os.Environ(), "ENABLE_DEPRECATED_IMPLICIT_DEFAULT_HTTP_CLIENT=true")
	if mb := set.General.MemLimitMB; mb > 0 {
		// «сколько памяти можно ядру»: мягкий предел Go-рантайма (GOMEMLIMIT) — сборщик мусора держит
		// потребление ниже него, чтобы ядро не выело память роутера.
		cmd.Env = append(cmd.Env, fmt.Sprintf("GOMEMLIMIT=%dMiB", mb))
	}
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Вывод ядра идёт в файл, а не в канал: при обновлении панель заменяет себя (exec), канал бы закрылся,
	// и ядро, оставшееся работать, упало бы на первой записи в лог.
	lf, logFile, err := s.openCoreLog(set.Core)
	if err != nil {
		s.lastErr = err.Error()
		return fmt.Errorf("не удалось открыть файл вывода ядра: %v", err)
	}
	cmd.Stdout, cmd.Stderr = lf, lf
	err = cmd.Start()
	lf.Close() // у ядра своя копия дескриптора
	if err != nil {
		s.lastErr = err.Error()
		return fmt.Errorf("не удалось запустить %s: %v", bin, err)
	}
	s.cmd, s.done, s.core = cmd, make(chan struct{}), set.Core
	s.bin, s.logFile = bin, logFile
	s.startedAt, s.lastErr, s.fwErr, s.desired = time.Now(), "", "", true
	s.awgFails = nil // у нового процесса ядра счётчики рукопожатий с нуля
	s.warnings = res.Warnings
	s.version, _ = installer.Version(context.Background(), set.Core, bin)
	if userInitiated {
		s.restarts = 0
		// ручной запуск: серия автовосстановления начинается заново
		s.recoverTries, s.recoverNote = 0, ""
	}
	s.waitNet = false
	s.Logs.Add(fmt.Sprintf("[panel] запущен %s (pid %d), конфиг %s", filepath.Base(bin), cmd.Process.Pid, cfgPath))

	tl := newTailer(logFile, s.Logs, 0)
	go tl.run()
	done := s.done
	go s.watchConntrack(done)
	go s.refreshRuleSets(set, dir, ramDir, done)
	if len(ruleSetWarnings) > 0 {
		go s.retryRuleSets(set, dir, ramDir, done)
	}
	go func() {
		err := cmd.Wait()
		tl.Stop()   // дочитать последние строки до сообщения об ошибке
		close(done) // раньше onExit: Start держит блокировку, пока ждёт done
		s.onExit(cmd, err)
	}()

	// ядро должно пережить первые секунды, иначе считаем запуск неудачным
	select {
	case <-done:
		tail := strings.Join(s.Logs.Tail(6), "\n")
		if networkNotReady(tail) {
			// интернета (маршрута по умолчанию) пока нет, например сразу после загрузки роутера
			s.desired, s.lastErr, s.waitNet, s.waitWhy = false, "", true, "нет маршрута в интернет"
			s.Logs.Add("[panel] нет маршрута в интернет — это не сбой: запуск будет повторяться каждые 10 с, пока не появится сеть")
			if !s.netLoop {
				s.netLoop = true
				go s.waitNetwork()
			}
			return nil
		}
		s.waitNet = false
		s.lastErr = "ядро завершилось сразу после запуска:\n" + tail + hintFor(tail)
		s.desired = false
		return errors.New(s.lastErr)
	case <-time.After(1500 * time.Millisecond):
	}

	// маршруты: применяем сразу и переприменяем, пока ядро работает, — правило «oif» пропадает или
	// устаревает, когда VPN-интерфейс появляется позже или переподключается, а маршрут в таблице
	// «своего» интерфейса исчезает вместе с устройством
	s.startRouteWatchesLocked(set, done)
	if sc := firewall.BlockIPv6(set); sc != nil {
		if err := firewall.Apply(context.Background(), sc); err != nil {
			s.Logs.Add("[panel] блокировка IPv6: " + err.Error())
		} else {
			s.v6 = sc
			s.Logs.Add("[panel] IPv6 заблокирован на время работы ядра")
		}
	}
	if sp, err := firewall.FromSettings(set, s.Info.FirewallBE); err != nil {
		s.fwErr = err.Error()
		s.Logs.Add("[panel] брандмауэр: " + err.Error())
	} else if sp != nil {
		sc, err := firewall.Generate(sp)
		if err == nil {
			err = firewall.Apply(context.Background(), sc)
		}
		if err != nil {
			s.fwErr = err.Error()
			s.Logs.Add("[panel] брандмауэр: " + err.Error())
		} else {
			s.fw = sc
			s.Logs.Add("[panel] правила прозрачного проксирования применены (" + sp.Mode + ", " + sp.Backend + ")")
		}
	}
	// состояние нужно, чтобы новая панель (после обновления или аварийного перезапуска) подхватила ядро
	if err := s.saveStateLocked(); err != nil {
		s.Logs.Add("[panel] состояние ядра не сохранено: " + err.Error())
	}
	return nil
}

// startRouteWatchesLocked применяет маршруты узлов-интерфейсов и «своих» интерфейсов панели и ставит оба
// набора на периодическую проверку. done закрывается при остановке ядра. Вызывать под s.mu.
func (s *Supervisor) startRouteWatchesLocked(set *model.Settings, done <-chan struct{}) {
	if sc := firewall.NodeIfaceRoutes(set); sc != nil {
		s.nfw = sc
		s.watchRoutesLocked(sc, done, "маршруты узлов-интерфейсов")
	}
	if sc := firewall.IfaceRoutes(set); sc != nil {
		s.ifw = sc
		if s.watchRoutesLocked(sc, done, "маршруты устройств в свои интерфейсы") == nil {
			s.Logs.Add("[panel] устройства направлены в свои интерфейсы")
		}
	}
	go s.watchInterfaceRecovery(done)
}

// watchInterfaceRecovery перезапускает системный VPN через netifd, если интерфейс отсутствует,
// выключен или не проходит проверку доступности через Clash API пять раз подряд.
func (s *Supervisor) watchInterfaceRecovery(done <-chan struct{}) {
	// Проверяем настройки раз в секунду, чтобы изменение интервала применялось без перезапуска панели.
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var checkInterval time.Duration
	var nextCheck time.Time
	for {
		select {
		case <-done:
			return
		case <-t.C:
			set := s.getSet()
			if set == nil || !set.Recovery.InterfaceRestartEnabled || s.Info.OS != platform.OpenWrt {
				s.mu.Lock()
				s.ifaceFails = nil
				s.ifaceRestarts = nil
				s.mu.Unlock()
				checkInterval = 0
				nextCheck = time.Time{}
				continue
			}
			configuredInterval := time.Duration(set.Recovery.InterfaceCheckIntervalSec) * time.Second
			now := time.Now()
			if configuredInterval != checkInterval {
				checkInterval = configuredInterval
				nextCheck = now.Add(checkInterval)
				continue
			}
			if now.Before(nextCheck) {
				continue
			}
			nextCheck = now.Add(checkInterval)
			checks := firewall.CheckNodeIfaceLinks(set)
			byIface := make(map[string][]firewall.NodeIfaceLink, len(checks))
			for _, check := range checks {
				byIface[check.Iface] = append(byIface[check.Iface], check)
			}
			seen := make(map[string]bool, len(byIface))
			for iface, nodes := range byIface {
				seen[iface] = true
				if len(nodes) == 0 {
					continue
				}
				if !nodes[0].Active {
					s.noteInterfaceFailure(set, nodes[0].Node, iface, "интерфейс отсутствует или выключен")
					continue
				}
				ok, skipped := false, false
				probeErr := errors.New("нет узла для проверки")
				for _, link := range nodes {
					nd, exists := findEnabledInterfaceNode(set, link.Node, iface)
					if !exists {
						continue
					}
					probeErr = s.probeInterfaceNode(context.Background(), set, nd)
					if probeErr == nil {
						ok = true
						break
					}
					if errors.Is(probeErr, errProbeSkipped) {
						skipped = true
					}
				}
				if !ok && skipped {
					continue
				}
				if ok {
					s.noteInterfaceHealthy(iface)
				} else {
					s.noteInterfaceFailure(set, nodes[0].Node, iface, "узел не отвечает на проверку задержки: "+probeErr.Error())
				}
			}
			s.mu.Lock()
			for iface := range s.ifaceFails {
				if !seen[iface] { // удалённый из настроек интерфейс больше не должен влиять на счётчик
					delete(s.ifaceFails, iface)
				}
			}
			for iface := range s.ifaceRestarts {
				if !seen[iface] {
					delete(s.ifaceRestarts, iface)
				}
			}
			s.mu.Unlock()
		}
	}
}

// watchRoutesLocked применяет скрипты маршрутов и повторяет их, пока ядро работает: правило «oif» пропадает,
// когда VPN-интерфейс исчезает, и устаревает, когда он пересоздаётся с новым индексом; маршрут в таблице
// «своего» интерфейса пропадает вместе с устройством. Ошибки применения не считаются сбоем — следующая
// проверка восстановит правила. Вызывать под s.mu.
func (s *Supervisor) watchRoutesLocked(sc *firewall.Scripts, done <-chan struct{}, label string) error {
	err := firewall.Apply(context.Background(), sc)
	if err != nil {
		s.Logs.Add("[panel] " + label + ": " + err.Error() + " (повторим автоматически)")
	}
	if sc.Refresh == "" {
		return err
	}
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				sc.RunRefresh(context.Background())
			}
		}
	}()
	return err
}

// rulesetFetcher скачивает текстовые наборы: напрямую (с зеркалом, если оно включено), затем через прокси ядра.
func (s *Supervisor) rulesetFetcher(set *model.Settings, proxyPort int) rulesets.Fetcher {
	var mirrors []string
	if v := set.General.UpdateVia; v == "" || v == "direct" {
		mirrors = mirror.List(set.Download)
	}
	return rulesets.NewFetcher(mirrors, proxyPort, set.Download)
}

// RefreshRuleSets вручную проверяет и обновляет наборы, которые скачивает панель.
// В отличие от фонового обновления, force также игнорирует интервал 0.
func (s *Supervisor) RefreshRuleSets(set *model.Settings) (int, int, []string) {
	if set == nil {
		return 0, 0, []string{"настройки не заданы"}
	}
	var updateSets []model.RuleSet
	managedByCore := 0
	for _, rs := range set.RuleSets {
		if rs.URL == "" {
			continue
		}
		if rulesets.IsText(rs) || rs.Interval <= 0 {
			updateSets = append(updateSets, rs)
		} else {
			managedByCore++
		}
	}
	if len(updateSets) == 0 {
		return 0, managedByCore, nil
	}
	proxyPort := 0
	status := s.Status()
	if status.State == "running" && status.Core == set.Core {
		proxyPort = set.Inbounds.MixedPort
	}
	dir := s.RuntimeDir(set.Core)
	ramDir := s.ruleSetsRAMDir(set, set.Core)
	s.rulesetMu.Lock()
	warnings := rulesets.PrepareWithRAM(context.Background(), dir, ramDir, updateSets, s.rulesetFetcher(set, proxyPort), true)
	s.rulesetMu.Unlock()
	for _, warning := range warnings {
		s.Logs.Add("[panel] " + warning)
	}
	return len(updateSets), managedByCore, warnings
}

// startupFetcher ограничивает скачивание при запуске ядра: на один адрес — не больше 15 секунд, а после
// двух неудач подряд остальные пропускаются (сеть до запуска ядра, видимо, недоступна). Иначе несколько
// недоступных наборов по минуте каждый задерживали бы запуск на несколько минут. Пропущенные наборы
// докачивает retryRuleSets уже через работающее ядро.
func startupFetcher(f rulesets.Fetcher) rulesets.Fetcher {
	fails := 0
	return func(ctx context.Context, raw string) ([]byte, error) {
		if fails >= 2 {
			return nil, errors.New("пропущено: сеть недоступна до запуска ядра, повторим через ядро")
		}
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		b, err := f(c, raw)
		if err != nil {
			fails++
		} else {
			fails = 0
		}
		return b, err
	}
}

// retryRuleSets докачивает наборы, которые не скачались при запуске: теперь запросы идут и напрямую,
// и через mixed-порт работающего ядра (по правилам маршрутизации). Ядро само перечитывает изменённые
// локальные файлы. Попытки: через 20 с, 1, 3 и 10 минут; прекращается, когда всё скачано.
func (s *Supervisor) retryRuleSets(set *model.Settings, dir, ramDir string, done <-chan struct{}) {
	var sets []model.RuleSet
	for _, rs := range set.RuleSets {
		if rs.URL != "" && (rulesets.IsText(rs) || rs.Interval <= 0) {
			sets = append(sets, rs)
		}
	}
	if len(sets) == 0 {
		return
	}
	for _, wait := range []time.Duration{20 * time.Second, time.Minute, 3 * time.Minute, 10 * time.Minute} {
		select {
		case <-done:
			return
		case <-time.After(wait):
		}
		s.rulesetMu.Lock()
		warns := rulesets.PrepareWithRAM(context.Background(), dir, ramDir, sets, s.rulesetFetcher(set, set.Inbounds.MixedPort), false)
		s.rulesetMu.Unlock()
		if len(warns) == 0 {
			s.Logs.Add("[panel] наборы правил докачаны")
			return
		}
		for _, w := range warns {
			s.Logs.Add("[panel] " + w)
		}
	}
}

// refreshRuleSets обновляет текстовые наборы, пока ядро работает; ядро само перечитывает изменённые локальные файлы.
func (s *Supervisor) refreshRuleSets(set *model.Settings, dir, ramDir string, done <-chan struct{}) {
	var updateSets []model.RuleSet
	for _, rs := range set.RuleSets {
		if rulesets.IsText(rs) && rs.Interval > 0 {
			updateSets = append(updateSets, rs)
		}
	}
	if len(updateSets) == 0 {
		return
	}
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	fetch := s.rulesetFetcher(set, set.Inbounds.MixedPort)
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.rulesetMu.Lock()
			warnings := rulesets.PrepareWithRAM(context.Background(), dir, ramDir, updateSets, fetch, false)
			s.rulesetMu.Unlock()
			for _, w := range warnings {
				s.Logs.Add("[panel] " + w)
			}
		}
	}
}

func (s *Supervisor) onExit(cmd *exec.Cmd, err error) {
	s.mu.Lock()
	if s.cmd != cmd {
		s.mu.Unlock()
		return
	}
	uptime := time.Since(s.startedAt)
	desired := s.desired
	msg := "ядро остановлено"
	if err != nil {
		msg = fmt.Sprintf("ядро завершилось: %v", err)
	}
	s.Logs.Add("[panel] " + msg)
	s.clearState()
	if s.fw != nil {
		firewall.Remove(context.Background(), s.fw)
		s.fw = nil
	}
	if s.ifw != nil {
		firewall.Remove(context.Background(), s.ifw)
		s.ifw = nil
	}
	if s.nfw != nil {
		firewall.Remove(context.Background(), s.nfw)
		s.nfw = nil
	}
	if s.v6 != nil {
		firewall.Remove(context.Background(), s.v6)
		s.v6 = nil
	}
	if desired {
		s.lastErr = msg
	}
	restarts := s.restarts
	s.mu.Unlock()

	if !desired || s.getSet == nil {
		return
	}
	// падение из-за отсутствия интернета (типично сразу после загрузки роутера) — не сбой:
	// ждём сеть, как preflight, вместо пяти быстрых автоперезапусков
	if networkNotReady(strings.Join(s.Logs.Tail(6), "\n")) {
		s.mu.Lock()
		s.desired, s.lastErr, s.waitNet, s.waitWhy = false, "", true, "нет маршрута в интернет"
		start := !s.netLoop
		if start {
			s.netLoop = true
		}
		s.Logs.Add("[panel] нет маршрута в интернет — это не сбой: запуск будет повторяться каждые 10 с, пока не появится сеть")
		s.mu.Unlock()
		if start {
			go s.waitNetwork()
		}
		return
	}
	// автоперезапуск с нарастающей паузой; быстрые падения не гоняем бесконечно
	if uptime > 30*time.Second {
		restarts = 0
	}
	if restarts >= 5 {
		s.mu.Lock()
		s.desired = false
		s.lastErr = "ядро падает слишком часто, автоперезапуск отключён: " + msg
		s.Logs.Add("[panel] " + s.lastErr)
		s.mu.Unlock()
		return
	}
	delay := time.Duration(1<<uint(restarts)) * 2 * time.Second
	s.Logs.Add(fmt.Sprintf("[panel] автоперезапуск через %s (попытка %d/5)", delay, restarts+1))
	time.Sleep(delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.desired || s.running() {
		return
	}
	s.restarts = restarts + 1
	if err := s.startLocked(s.getSet(), false); err != nil {
		s.Logs.Add("[panel] автоперезапуск не удался: " + err.Error())
	}
}

// waitNetwork повторяет запуск, пока не появится сеть (до 10 минут).
func (s *Supervisor) waitNetwork() {
	defer func() {
		s.mu.Lock()
		s.netLoop = false
		s.mu.Unlock()
	}()
	for i := 0; i < 60; i++ {
		time.Sleep(10 * time.Second)
		s.mu.Lock()
		if !s.waitNet || s.running() || s.getSet == nil {
			s.mu.Unlock()
			return
		}
		err := s.startLocked(s.getSet(), false)
		waiting := s.waitNet
		s.mu.Unlock()
		if err != nil {
			s.Logs.Add("[panel] повторный запуск не удался: " + err.Error())
			return
		}
		if !waiting {
			return
		}
	}
	s.mu.Lock()
	s.waitNet = false
	s.lastErr = "сеть не появилась за 10 минут: проверьте подключение к интернету и запустите ядро вручную"
	s.mu.Unlock()
}

// Stop останавливает ядро и снимает правила брандмауэра.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	s.desired, s.waitNet = false, false
	cmd, done, fw, ifw, nfw, v6 := s.cmd, s.done, s.fw, s.ifw, s.nfw, s.v6
	s.fw, s.ifw, s.nfw, s.v6 = nil, nil, nil, nil
	s.clearState()
	s.mu.Unlock()

	if fw != nil {
		firewall.Remove(context.Background(), fw)
	}
	if ifw != nil {
		firewall.Remove(context.Background(), ifw)
	}
	if nfw != nil {
		firewall.Remove(context.Background(), nfw)
	}
	if v6 != nil {
		firewall.Remove(context.Background(), v6)
	}
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			return errors.New("процесс ядра не завершился")
		}
	}
	return nil
}

// Restart перезапускает ядро с новыми настройками.
func (s *Supervisor) Restart(set *model.Settings) error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start(set)
}

// Status возвращает текущее состояние.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{State: "stopped", Core: s.core, Error: s.lastErr, FirewallError: s.fwErr,
		Restarts: s.restarts, Version: s.version, Warnings: s.warnings}
	if s.recoverNote != "" {
		st.Warnings = append(append([]string{}, s.warnings...), s.recoverNote)
	}
	if s.running() {
		st.State = "running"
		st.PID = s.cmd.Process.Pid
		st.UptimeSec = int64(time.Since(s.startedAt).Seconds())
		st.MemMB = procRSSMB(s.cmd.Process.Pid)
	} else if s.waitNet {
		st.State = "waiting"
		why := s.waitWhy
		if why == "" {
			why = "нет маршрута по умолчанию"
		}
		st.Notice = "Ждём готовности системы: " + why + ". Ядро запустится само, как только это появится."
	} else if s.lastErr != "" {
		st.State = "failed"
	}
	return st
}

// ---------- вспомогательное ----------

func checkConfig(core, bin, dir, cfg string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "check", "-c", cfg, "-D", dir)
	cmd.Env = append(os.Environ(), "ENABLE_DEPRECATED_IMPLICIT_DEFAULT_HTTP_CLIENT=true")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(stripANSI(string(out))), err
}

// procRSSMB — реально занятая память процесса (VmRSS из /proc/<pid>/status) в МБ.
// 0 — прочитать не удалось: панель просто не показывает память ядра.
func procRSSMB(pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return 0
		}
		kb, err := strconv.Atoi(f[1])
		if err != nil {
			return 0
		}
		return (kb + 512) / 1024
	}
	return 0
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
