package cmd

import (
	"fmt"
	"os/user"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/deploy"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/git"
	"github.com/ronxjansen/ferry/internal/plan"
)

func hostFor(s *config.Server) *exec.Host {
	return &exec.Host{Server: s}
}

// shellJoin joins argv into one shell command, quoting args with whitespace.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'$&|;<>()*?#~") {
			quoted[i] = fmt.Sprintf("%q", a)
		} else {
			quoted[i] = a
		}
	}
	return strings.Join(quoted, " ")
}

func performer() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

// resolveVersion returns the deploy version: the --version flag if set, else
// the short git SHA (with a -dirty suffix on an unclean tree).
func resolveVersion(p *plan.Plan, flag string) (version string, dirty bool, err error) {
	if flag != "" {
		return flag, false, nil
	}
	sha, dirty, err := git.Version(p.Config.Dir)
	if err != nil {
		return "", false, err
	}
	if dirty {
		sha += "-dirty"
	}
	return sha, dirty, nil
}

func newDeployer(p *plan.Plan, version string, timeout time.Duration) *deploy.Deployer {
	return &deploy.Deployer{
		Plan:      p,
		Version:   version,
		Performer: performer(),
		Timeout:   timeout,
		Log:       infof,
	}
}

func printWarnings(p *plan.Plan) {
	for _, w := range p.Warnings() {
		infof("Warning: %s", w)
	}
}

func fmtDuration(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}
