package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"corepanel/internal/platform"
)

var linuxIfaceName = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,15}$`)
var netifdName = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,64}$`)

type netifdInterface struct {
	Name     string `json:"interface"`
	Device   string `json:"device"`
	L3Device string `json:"l3_device"`
	Proto    string `json:"proto"`
}

// restartInterface restarts the netifd logical interface associated with a saved
// system-VPN node's Linux device. It never restarts the panel or amnezia-box.
func (s *Server) restartInterface(w http.ResponseWriter, r *http.Request) {
	if s.Info.OS != platform.OpenWrt {
		fail(w, 400, "перезапуск интерфейса через netifd доступен только в OpenWrt")
		return
	}
	var in struct {
		Interface string `json:"interface"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !linuxIfaceName.MatchString(in.Interface) {
		fail(w, 400, "неверное имя Linux-интерфейса")
		return
	}

	allowed := false
	for _, node := range s.St.Get().Nodes {
		if node.Type == "iface" && node.BindInterface == in.Interface {
			allowed = true
			break
		}
	}
	if !allowed {
		fail(w, 403, "интерфейс не привязан к сохранённому узлу системного VPN")
		return
	}

	// Не привязываем команды netifd к HTTP-контексту: связь с роутером может
	// пропасть после down, но команда up всё равно должна выполниться.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ubus, err := findUbus()
	if err != nil {
		fail(w, 500, "не найден ubus OpenWrt: "+err.Error())
		return
	}
	logical, err := netifdLogicalInterface(ctx, ubus, in.Interface)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if !netifdName.MatchString(logical) {
		fail(w, 500, "netifd вернул некорректное имя логического интерфейса")
		return
	}

	object := "network.interface." + logical
	_, downErr := callUbus(ctx, ubus, object, "down")
	time.Sleep(time.Second)
	// Команду up выполняем в отдельном контексте: даже если down занял
	// большую часть общего лимита, интерфейс всё равно нужно поднять обратно.
	upCtx, cancelUp := context.WithTimeout(context.Background(), 25*time.Second)
	_, upErr := callUbus(upCtx, ubus, object, "up")
	cancelUp()
	if downErr != nil {
		msg := "не удалось отключить интерфейс OpenWrt: " + downErr.Error()
		if upErr == nil {
			msg += "; команда включения выполнена, проверьте состояние интерфейса"
		} else {
			msg += "; не удалось включить интерфейс повторно: " + upErr.Error()
		}
		fail(w, 500, msg)
		return
	}
	if upErr != nil {
		fail(w, 500, "не удалось включить интерфейс OpenWrt: "+upErr.Error())
		return
	}
	ok(w, map[string]any{"restarted": true, "interface": in.Interface, "logical_interface": logical})
}

func findUbus() (string, error) {
	if path, err := exec.LookPath("ubus"); err == nil {
		return path, nil
	}
	for _, path := range []string{"/sbin/ubus", "/usr/bin/ubus"} {
		if _, err := exec.LookPath(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("установите пакет ubus")
}

func netifdLogicalInterface(ctx context.Context, ubus, device string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, ubus, "call", "network.interface", "dump", "{}").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("не удалось получить список интерфейсов netifd: %s", commandError(err, out))
	}
	var dump struct {
		Interfaces []netifdInterface `json:"interface"`
	}
	if err := json.Unmarshal(out, &dump); err != nil {
		return "", fmt.Errorf("не удалось прочитать список интерфейсов netifd: %w", err)
	}
	return resolveNetifdInterface(dump.Interfaces, device)
}

func resolveNetifdInterface(interfaces []netifdInterface, device string) (string, error) {
	// netifd публикует l3_device только пока подключение поднято. Для PPPoE
	// имя обычно строится как "pppoe-<имя профиля>", поэтому при отключённом
	// туннеле сопоставляем его по proto и имени логического интерфейса.
	var l3Matches, generatedMatches, deviceMatches []string
	for _, item := range interfaces {
		if item.Name == "" {
			continue
		}
		if item.L3Device == device {
			l3Matches = appendUnique(l3Matches, item.Name)
		}
		if item.Proto != "" && item.Proto+"-"+item.Name == device {
			generatedMatches = appendUnique(generatedMatches, item.Name)
		}
		if item.Device == device || item.Name == device {
			deviceMatches = appendUnique(deviceMatches, item.Name)
		}
	}
	// Один L3-интерфейс может обслуживать несколько секций (например, wan и
	// wan6). Совпадение proto-section указывает на профиль, создавший устройство.
	if name, err := uniqueNetifdMatch(device, generatedMatches); err != nil || name != "" {
		return name, err
	}
	if name, err := uniqueNetifdMatch(device, l3Matches); err != nil || name != "" {
		return name, err
	}
	if name, err := uniqueNetifdMatch(device, deviceMatches); err != nil || name != "" {
		return name, err
	}
	return "", fmt.Errorf("для Linux-интерфейса %q не найдено подключение netifd; перезапуск доступен только для интерфейсов, которыми управляет OpenWrt", device)
}

func uniqueNetifdMatch(device string, names []string) (string, error) {
	if len(names) == 1 {
		return names[0], nil
	}
	if len(names) > 1 {
		return "", fmt.Errorf("Linux-интерфейс %q связан с несколькими подключениями OpenWrt: %s", device, strings.Join(names, ", "))
	}
	return "", nil
}

func appendUnique(names []string, name string) []string {
	if !contains(names, name) {
		return append(names, name)
	}
	return names
}

func callUbus(ctx context.Context, ubus, object, method string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, ubus, "call", object, method, "{}").CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("ubus %s: %s", method, commandError(err, out))
	}
	return out, nil
}

func commandError(err error, out []byte) string {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err.Error()
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
