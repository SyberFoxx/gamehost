package app

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/syberfoxx/gamehost/internal/service"
	"github.com/syberfoxx/gamehost/internal/tui"
)

// lobbyServiceStatus reads the systemd state of one instance.
func lobbyServiceStatus(name string) (tui.ServiceStatus, error) {
	unitDir, err := resolveDir("")
	if err != nil {
		return tui.ServiceStatus{}, err
	}
	ctl, err := resolveSystemctl("")
	if err != nil {
		return tui.ServiceStatus{}, err
	}

	status := tui.ServiceStatus{
		Unit:    service.UnitName(name),
		Timer:   service.TimerUnitName(name),
		UnitDir: unitDir,
	}
	if _, err := os.Stat(filepath.Join(unitDir, status.Unit)); err == nil {
		status.Installed = true
	}
	if ctl != "" {
		status.Active = runUnitActive(ctl, status.Unit)
	}
	return status, nil
}

// lobbyServiceInstall writes the units, optionally enabling them now.
func lobbyServiceInstall(name string, enable bool) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	opts := serviceInstallOptions{Enable: enable}
	if err := installService(s, inst, opts, &out, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyServiceUninstall removes the units of one instance.
func lobbyServiceUninstall(name string) (string, error) {
	var out bytes.Buffer
	if err := uninstallService("", "", name, &out, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}
