package auth

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"lantern/internal/model"
)

const UnitName = "lantern-auth.service"

func Unit(cfg *model.Config, configPath string) (string, string, error) {
	cfg.ApplyDefaults()
	if !filepath.IsAbs(cfg.Settings.Auth.BinaryPath) || strings.ContainsAny(cfg.Settings.Auth.BinaryPath, "\r\n%") {
		return "", "", errors.New("settings.auth.binary_path must be an absolute path without control characters or %")
	}
	absolutePath, err := filepath.Abs(configPath)
	if err != nil {
		return "", "", err
	}
	if strings.ContainsAny(absolutePath, "\r\n%") {
		return "", "", errors.New("config path cannot contain control characters or %")
	}
	path := filepath.Join(cfg.Settings.FRP.SystemdDir, UnitName)
	body := fmt.Sprintf(`[Unit]
Description=Lantern authentication gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s auth serve --config %s
Restart=always
RestartSec=3s
NoNewPrivileges=true
PrivateTmp=true
UMask=0077

[Install]
WantedBy=multi-user.target
`, strconv.Quote(cfg.Settings.Auth.BinaryPath), strconv.Quote(absolutePath))
	return path, body, nil
}
