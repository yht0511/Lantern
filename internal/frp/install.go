package frp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"lantern/internal/model"
)

type InstallPlan struct {
	InstallDir string
	ScriptPath string
	Script     string
}

func WriteInstallScript(plan InstallPlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.ScriptPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(plan.ScriptPath, []byte(plan.Script), 0o755)
}

func RunInstallScript(ctx context.Context, plan InstallPlan) error {
	if err := WriteInstallScript(plan); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "sh", plan.ScriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func GenerateInstallPlan(cfg *model.Config, version string) InstallPlan {
	cfg.ApplyDefaults()
	if version == "" {
		version = "latest"
	}
	scriptPath := filepath.Join(cfg.Settings.FRP.ConfigDir, "install-frp.sh")
	return InstallPlan{
		InstallDir: cfg.Settings.FRP.InstallDir,
		ScriptPath: scriptPath,
		Script:     renderInstallScript(cfg, version),
	}
}

func renderInstallScript(cfg *model.Config, version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#!/bin/sh\n")
	fmt.Fprintf(&b, "set -eu\n\n")
	fmt.Fprintf(&b, "# %s\n", cfg.Settings.GeneratedByTag)
	fmt.Fprintf(&b, "# This script installs frpc under %s.\n", cfg.Settings.FRP.InstallDir)
	fmt.Fprintf(&b, "# Review before running on a server.\n\n")
	fmt.Fprintf(&b, "ARCH=\"$(uname -m)\"\n")
	fmt.Fprintf(&b, "OS=\"$(uname -s | tr '[:upper:]' '[:lower:]')\"\n")
	fmt.Fprintf(&b, "case \"$ARCH\" in\n")
	fmt.Fprintf(&b, "  x86_64|amd64) FRP_ARCH=amd64 ;;\n")
	fmt.Fprintf(&b, "  aarch64|arm64) FRP_ARCH=arm64 ;;\n")
	fmt.Fprintf(&b, "  armv7l) FRP_ARCH=arm ;;\n")
	fmt.Fprintf(&b, "  *) echo \"unsupported arch: $ARCH\" >&2; exit 1 ;;\n")
	fmt.Fprintf(&b, "esac\n\n")
	if version == "latest" {
		fmt.Fprintf(&b, "echo \"Set FRP_VERSION explicitly before using this script in production.\" >&2\n")
		fmt.Fprintf(&b, ": \"${FRP_VERSION:?FRP_VERSION is required, for example v0.61.0}\"\n")
	} else {
		fmt.Fprintf(&b, "FRP_VERSION=%q\n", version)
	}
	fmt.Fprintf(&b, "NAME=\"frp_${FRP_VERSION#v}_${OS}_${FRP_ARCH}\"\n")
	fmt.Fprintf(&b, "URL=\"https://github.com/fatedier/frp/releases/download/${FRP_VERSION}/${NAME}.tar.gz\"\n")
	fmt.Fprintf(&b, "TMP=\"$(mktemp -d)\"\n")
	fmt.Fprintf(&b, "trap 'rm -rf \"$TMP\"' EXIT\n\n")
	fmt.Fprintf(&b, "mkdir -p %q\n", cfg.Settings.FRP.InstallDir)
	fmt.Fprintf(&b, "curl -fsSL \"$URL\" -o \"$TMP/frp.tar.gz\"\n")
	fmt.Fprintf(&b, "tar -xzf \"$TMP/frp.tar.gz\" -C \"$TMP\"\n")
	fmt.Fprintf(&b, "install -m 0755 \"$TMP/$NAME/frpc\" %q\n", filepath.Join(cfg.Settings.FRP.InstallDir, "frpc"))
	fmt.Fprintf(&b, "echo \"installed frpc to %s\"\n", filepath.Join(cfg.Settings.FRP.InstallDir, "frpc"))
	return b.String()
}
