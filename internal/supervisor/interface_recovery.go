package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"corepanel/internal/model"
)

const interfaceRecoveryProbeURL = "https://www.gstatic.com/generate_204"

func findEnabledInterfaceNode(set *model.Settings, name, iface string) (model.Node, bool) {
	for _, node := range set.Nodes {
		if node.Name == name && node.Type == "iface" && node.BindInterface == iface && !node.Disabled {
			return node, true
		}
	}
	return model.Node{}, false
}

// probeInterfaceNode uses the same Clash API delay check as the node list in the UI.
// Any response with a positive delay means the system VPN can reach its test address.
func (s *Supervisor) probeInterfaceNode(parent context.Context, set *model.Settings, node model.Node) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(set.General.Controller))
	if err != nil {
		return fmt.Errorf("неверный адрес контроллера: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	testURL, timeoutMS := interfaceRecoveryProbeConfig(set, node.Name)
	probeURL := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}
	probeURL.Path = "/proxies/" + node.Name + "/delay"
	probeURL.RawPath = "/proxies/" + url.PathEscape(node.Name) + "/delay"
	query := url.Values{}
	query.Set("url", testURL)
	query.Set("timeout", fmt.Sprint(timeoutMS))
	probeURL.RawQuery = query.Encode()

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutMS+5000)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL.String(), nil)
	if err != nil {
		return err
	}
	if secret := set.General.Secret; secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Clash API вернул HTTP %d", resp.StatusCode)
	}
	var result struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("не удалось прочитать результат проверки: %w", err)
	}
	if result.Delay <= 0 {
		return fmt.Errorf("проверка не получила ответ")
	}
	return nil
}

func interfaceRecoveryProbeConfig(set *model.Settings, node string) (string, int) {
	for _, group := range set.Groups {
		if group.Type == "" || group.Type == "selector" {
			continue
		}
		for _, member := range group.Members {
			if member != node {
				continue
			}
			testURL := group.URL
			if testURL == "" {
				testURL = interfaceRecoveryProbeURL
			}
			timeoutMS := group.Timeout
			if timeoutMS < model.DelayTimeoutMin {
				timeoutMS = model.DelayTimeoutDefault
			}
			if timeoutMS > model.DelayTimeoutMax {
				timeoutMS = model.DelayTimeoutMax
			}
			return testURL, timeoutMS
		}
	}
	return interfaceRecoveryProbeURL, model.DelayTimeoutDefault
}
