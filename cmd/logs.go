package cmd

import (
	"bufio"
	"fmt"
	osexec "os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/deploy"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
)

var (
	logsFollow bool
	logsTail   int
	logsSince  string
	logsServer string
)

var logsCmd = &cobra.Command{
	Use:   "logs <service>",
	Short: "View service logs (interleaved with a host prefix on multiple servers)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		t, err := p.Target(args[0])
		if err != nil {
			return err
		}

		servers := t.Servers
		if logsServer != "" {
			s, err := p.Config.GetServer(logsServer)
			if err != nil {
				return err
			}
			if !t.RunsOn(s) {
				return fmt.Errorf("service %s does not run on server %s", t.Name, s.Name)
			}
			servers = []*config.Server{s}
		}

		d := newDeployer(p, "", time.Minute)

		logsArgs := func(container string) []string {
			a := []string{"logs", "--tail", fmt.Sprint(logsTail)}
			if logsSince != "" {
				a = append(a, "--since", logsSince)
			}
			if logsFollow {
				a = append(a, "--follow")
			}
			return append(a, container)
		}

		if len(servers) == 1 {
			dk, container, err := findContainer(d, t.Name, servers[0])
			if err != nil {
				return err
			}
			return dk.Interactive(logsArgs(container)...)
		}

		// Multiple servers: interleave with a host prefix.
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, s := range servers {
			dk, container, err := findContainer(d, t.Name, s)
			if err != nil {
				infof("%s: %v", s.Name, err)
				continue
			}
			wg.Add(1)
			go func(s *config.Server) {
				defer wg.Done()
				full := dk.Args(logsArgs(container)...)
				c := osexec.Command(full[0], full[1:]...)
				stdout, err := c.StdoutPipe()
				if err != nil {
					return
				}
				c.Stderr = c.Stdout
				if err := c.Start(); err != nil {
					return
				}
				scanner := bufio.NewScanner(stdout)
				scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
				for scanner.Scan() {
					mu.Lock()
					fmt.Printf("%s | %s\n", s.Name, scanner.Text())
					mu.Unlock()
				}
				c.Wait()
			}(s)
		}
		wg.Wait()
		return nil
	},
}

// findContainer locates the running container for a service on a server.
func findContainer(d *deploy.Deployer, service string, s *config.Server) (*exec.Docker, string, error) {
	dk, err := d.Docker(s)
	if err != nil {
		return nil, "", err
	}
	out, err := dk.Run("ps",
		"--filter", "label=ferry.service="+service,
		"--format", "{{.Names}}\t{{.Label \"ferry.preview\"}}")
	if err != nil {
		return nil, "", err
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) >= 1 && parts[0] != "" && (len(parts) < 2 || parts[1] == "") {
			return dk, parts[0], nil
		}
	}
	return nil, "", fmt.Errorf("no running container for %s on %s", service, s.Name)
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVar(&logsTail, "tail", 100, "Lines to show from the end")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show logs since (e.g. 1h, 2006-01-02T15:04:05)")
	logsCmd.Flags().StringVar(&logsServer, "server", "", "Only this server")
	rootCmd.AddCommand(logsCmd)
}
