package supervisor

// Автовосстановление при затяжном сбое рукопожатий AmneziaWG и системного VPN-интерфейса.
//
// Бывает так: сервер перестаёт отвечать на рукопожатия, а ядро продолжает работать с «мёртвым»
// туннелем — панель показывает «нет рукопожатия», трафик не идёт. Перезапуск ядра обычно решает
// такую проблему (свежие сокеты, свежее рукопожатие), поэтому панель следит за журналом ядра и
// перезапускает его сама, когда подряд накопился fail_threshold неудачных рукопожатий.
//
// Ядро пишет события рукопожатия на уровне логов debug («sending handshake initiation»,
// «handshake did not complete after 5 seconds, retrying», «received handshake response»).
//
// Защита от циклического перезапуска (когда дело не в ядре, а, например, в недоступном сервере):
//   - подряд делается не больше max_restarts попыток, дальше панель сдаётся и показывает
//     предупреждение в статусе;
//   - счётчик серии сбрасывается при ручном запуске/перезапуске ядра;
//   - если новая серия сбоев началась после долгой (recoveryHealthyRun) нормальной работы,
//     прошлые попытки больше не считаются «подряд идущими» — счётчик обнуляется.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"corepanel/internal/model"
)

// recoveryHealthyRun — сколько ядро должно проработать, чтобы прошлые автоперезапуски перестали
// считаться «подряд идущими».
const recoveryHealthyRun = 10 * time.Minute

// Шаблоны повторяют разбор в web/static/js/awgstatus.js (бейдж «нет рукопожатия»), чтобы поведение
// бейджа и автовосстановления было согласованным.
var (
	awgTagRe  = regexp.MustCompile(`endpoint/awg\[([^\]]+)\]`)
	awgFailRe = regexp.MustCompile(`handshake did not complete|retrying handshake`)
	awgOkRe   = regexp.MustCompile(`received handshake response|receiving keepalive|sending keepalive`)
)

// watchHandshakes читает поток журнала ядра. Подписка живёт вместе с панелью и не зависит от
// перезапусков ядра: строки и старого, и нового процесса попадают в один LogBuffer.
func (s *Supervisor) watchHandshakes() {
	ch, _ := s.Logs.Subscribe()
	for line := range ch {
		s.noteAwgLine(line)
	}
}

// noteAwgLine обрабатывает одну строку журнала: считает серию неудачных рукопожатий по узлам и,
// когда серия достигает порога, запускает автоперезапуск ядра.
func (s *Supervisor) noteAwgLine(line string) {
	if !strings.Contains(line, "peer(") {
		return
	}
	m := awgTagRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	tag := m[1]
	set := s.getSet()

	s.mu.Lock()
	if set == nil || !set.Recovery.Enabled {
		s.awgFails = nil // слежение выключено: не копим счётчики прошлых сбоев
		s.mu.Unlock()
		return
	}
	if s.awgFails == nil {
		s.awgFails = map[string]int{}
	}
	if awgOkRe.MatchString(line) { // рукопожатие прошло или обмен keepalive: туннель жив
		s.awgFails[tag] = 0
		s.mu.Unlock()
		return
	}
	if !awgFailRe.MatchString(line) {
		s.mu.Unlock()
		return
	}
	s.awgFails[tag]++
	fails := s.awgFails[tag]
	if s.recovering || !s.desired || !s.running() || fails < set.Recovery.FailThreshold {
		s.mu.Unlock()
		return
	}
	if time.Since(s.startedAt) > recoveryHealthyRun { // долгая нормальная работа: считаем серию заново
		s.recoverTries, s.recoverNote = 0, ""
	}
	if s.recoverTries >= set.Recovery.MaxRestarts {
		if s.recoverNote == "" {
			s.recoverNote = fmt.Sprintf("Автовосстановление остановлено: %d перезапусков подряд не помогли — дело не в ядре (проверьте доступность сервера узла, сеть и параметры AmneziaWG).", s.recoverTries)
			s.Logs.Add("[panel] " + s.recoverNote)
		}
		s.mu.Unlock()
		return
	}
	s.recovering = true
	attempt, max := s.recoverTries+1, set.Recovery.MaxRestarts
	s.Logs.Add(fmt.Sprintf("[panel] узел %s: %d подряд неудачных рукопожатий — перезапускаю ядро (автовосстановление, попытка %d/%d)", tag, fails, attempt, max))
	s.mu.Unlock()
	go s.autoRestart(attempt)
}

func (s *Supervisor) noteInterfaceHealthy(iface string) {
	s.mu.Lock()
	if s.ifaceFails != nil {
		s.ifaceFails[iface] = 0
	}
	if s.ifaceRestarts != nil {
		s.ifaceRestarts[iface] = 0
	}
	for _, mark := range []string{"системный VPN-интерфейс " + iface + ":", "системный интерфейс " + iface + " "} {
		if strings.Contains(s.recoverNote, mark) {
			s.recoverNote = ""
		}
	}
	if !s.recovering && time.Since(s.startedAt) > recoveryHealthyRun {
		s.recoverTries, s.recoverNote = 0, ""
	}
	s.mu.Unlock()
}

func (s *Supervisor) noteInterfaceFailure(set *model.Settings, node, iface, reason string) {
	s.mu.Lock()
	if s.recovering || !s.desired || !s.running() {
		s.mu.Unlock()
		return
	}
	if s.ifaceFails == nil {
		s.ifaceFails = map[string]int{}
	}
	s.ifaceFails[iface]++
	fails := s.ifaceFails[iface]
	if fails < 5 {
		s.mu.Unlock()
		return
	}
	if s.ifaceRestarts == nil {
		s.ifaceRestarts = map[string]int{}
	}
	tries := s.ifaceRestarts[iface]
	if tries >= set.Recovery.MaxRestarts {
		note := fmt.Sprintf("Автовосстановление остановлено: %d попыток переподключения не помогли — проверьте системный интерфейс %s и доступность узла.", tries, iface)
		if s.recoverNote != note {
			s.recoverNote = note
			s.Logs.Add("[panel] " + s.recoverNote)
		}
		s.mu.Unlock()
		return
	}
	s.recovering = true
	attempt, max := tries+1, set.Recovery.MaxRestarts
	s.ifaceRestarts[iface] = attempt
	s.ifaceFails[iface] = 0
	s.Logs.Add(fmt.Sprintf("[panel] узел %s: интерфейс %s: %s (5 проверок подряд) — перезапускаю системный VPN через OpenWrt, ядро не трогаю (попытка %d/%d)", node, iface, reason, attempt, max))
	s.mu.Unlock()
	go s.autoRestartInterface(iface)
}

func (s *Supervisor) autoRestartInterface(iface string) {
	s.mu.Lock()
	restart := s.interfaceRestarter
	s.mu.Unlock()

	var err error
	if restart == nil {
		err = errors.New("перезапуск системного VPN доступен только на OpenWrt через netifd")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		err = restart(ctx, iface)
		cancel()
	}

	s.mu.Lock()
	s.recovering = false
	if err != nil {
		s.recoverNote = "Автовосстановление: не удалось перезапустить системный VPN-интерфейс " + iface + ": " + err.Error()
		s.Logs.Add("[panel] " + s.recoverNote)
	} else {
		if strings.Contains(s.recoverNote, "системный VPN-интерфейс "+iface+":") {
			s.recoverNote = ""
		}
		s.Logs.Add("[panel] системный VPN-интерфейс " + iface + " перезапущен через OpenWrt netifd; ядро не перезапускалось")
	}
	s.mu.Unlock()
}

// autoRestart перезапускает ядро по решению наблюдателя. Это не «ручной» запуск: счётчики
// автовосстановления сохраняются, иначе защита от цикла не работала бы.
func (s *Supervisor) autoRestart(attempt int) {
	s.mu.Lock()
	s.recoverTries = attempt
	s.mu.Unlock()

	err := errors.New("настройки недоступны")
	if set := s.getSet(); set != nil {
		if err = s.Stop(); err == nil {
			s.mu.Lock()
			err = s.startLocked(set, false)
			s.mu.Unlock()
		}
	}

	s.mu.Lock()
	s.recovering = false
	if err != nil {
		s.recoverNote = "Автовосстановление: перезапуск ядра не удался: " + err.Error()
		s.Logs.Add("[panel] " + s.recoverNote)
	}
	s.mu.Unlock()
}
