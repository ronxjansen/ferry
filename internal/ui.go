package ferry

// import (
// 	"fmt"
// 	"log"
// 	"os"
// 	"os/exec"

// 	"github.com/charmbracelet/bubbles/list"
// 	tea "github.com/charmbracelet/bubbletea"
// 	"github.com/charmbracelet/lipgloss"
// 	"github.com/spf13/cobra"
// )

// var (
// 	configFile string
// 	config     *Config
// )

// type item struct {
// 	title, desc string
// }

// func (i item) Title() string       { return i.title }
// func (i item) Description() string { return i.desc }
// func (i item) FilterValue() string { return i.title }

// type model struct {
// 	list     list.Model
// 	choice   string
// 	quitting bool
// }

// func initialModel() model {
// 	items := []list.Item{
// 		item{title: "Setup", desc: "Check and install Docker on remote servers"},
// 		item{title: "Deploy", desc: "Deploy Docker image to remote servers"},
// 		item{title: "Env", desc: "Sync environment variables to remote servers"},
// 		item{title: "Logs", desc: "Read logs from remote servers"},
// 		item{title: "Rollback", desc: "Rollback to the previous version on remote servers"},
// 		item{title: "Quit", desc: "Exit the application"},
// 	}

// 	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
// 	l.Title = "Goship"

// 	return model{list: l}
// }

// func (m model) Init() tea.Cmd {
// 	return nil
// }

// func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
// 	switch msg := msg.(type) {
// 	case tea.KeyMsg:
// 		if msg.String() == "ctrl+c" {
// 			m.quitting = true
// 			return m, tea.Quit
// 		}
// 	case tea.WindowSizeMsg:
// 		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
// 		m.list.SetSize(msg.Width-h, msg.Height-v)
// 	}

// 	var cmd tea.Cmd
// 	m.list, cmd = m.list.Update(msg)

// 	if m.list.SelectedItem() != nil {
// 		switch m.list.SelectedItem().(item).title {
// 		case "Setup":
// 			return m, m.runSetup
// 		case "Deploy":
// 			return m, m.runDeploy
// 		case "Env":
// 			return m, m.runEnv
// 		case "Logs":
// 			return m, m.runLogs
// 		case "Rollback":
// 			return m, m.runRollback
// 		case "Quit":
// 			m.quitting = true
// 			return m, tea.Quit
// 		}
// 	}

// 	return m, cmd
// }

// func (m model) View() string {
// 	if m.quitting {
// 		return "Goodbye!\n"
// 	}
// 	return "\n" + m.list.View()
// }

// func (m model) runSetup() tea.Msg {
// 	// for _, server := range config.Servers {
// 	// err := setupDocker(server)
// 	// if err != nil {
// 	// 	return fmt.Sprintf("Error setting up Docker on %s: %v", server.Host, err)
// 	// }
// 	// }
// 	return "Docker setup completed successfully on all servers"
// }

// func (m model) runDeploy() tea.Msg {
// 	for _, server := range config.Servers {
// 		err := deployToServer(config, server)
// 		if err != nil {
// 			return fmt.Sprintf("Error deploying to %s: %v", server.Host, err)
// 		}
// 	}
// 	return "Deployment completed successfully on all servers"
// }

// func (m model) runEnv() tea.Msg {
// 	for _, server := range config.Servers {
// 		err := syncEnvToServer(config, server)
// 		if err != nil {
// 			return fmt.Sprintf("Error syncing env to %s: %v", server.Host, err)
// 		}
// 	}
// 	return "Environment variables synced successfully on all servers"
// }

// func (m model) runLogs() tea.Msg {
// 	logs := ""
// 	for _, server := range config.Servers {
// 		serverLogs, err := getLogsFromServer(config, server)
// 		if err != nil {
// 			return fmt.Sprintf("Error getting logs from %s: %v", server.Host, err)
// 		}
// 		logs += fmt.Sprintf("Logs from %s:\n%s\n", server.Host, serverLogs)
// 	}
// 	return logs
// }

// func (m model) runRollback() tea.Msg {
// 	for _, server := range config.Servers {
// 		err := rollbackOnServer(config, server)
// 		if err != nil {
// 			return fmt.Sprintf("Error rolling back on %s: %v", server.Host, err)
// 		}
// 	}
// 	return "Rollback completed successfully on all servers"
// }

// func run() {
// 	var rootCmd = &cobra.Command{
// 		Use:   "goship",
// 		Short: "Goship - Docker deployment tool",
// 		Run: func(cmd *cobra.Command, args []string) {
// 			runInteractiveMode()
// 		},
// 	}

// 	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "config file path")

// 	setupCmd := &cobra.Command{
// 		Use:   "setup",
// 		Short: "Check and install Docker on remote servers",
// 		Run:   runSetup,
// 	}

// 	deployCmd := &cobra.Command{
// 		Use:   "deploy",
// 		Short: "Deploy Docker image to remote servers",
// 		Run:   runDeploy,
// 	}

// 	envCmd := &cobra.Command{
// 		Use:   "env",
// 		Short: "Sync environment variables to remote servers",
// 		Run:   runEnv,
// 	}

// 	logsCmd := &cobra.Command{
// 		Use:   "logs",
// 		Short: "Read logs from remote servers",
// 		Run:   runLogs,
// 	}

// 	rollbackCmd := &cobra.Command{
// 		Use:   "rollback",
// 		Short: "Rollback to the previous version on remote servers",
// 		Run:   runRollback,
// 	}

// 	rootCmd.AddCommand(setupCmd, deployCmd, envCmd, logsCmd, rollbackCmd)

// 	if err := rootCmd.Execute(); err != nil {
// 		fmt.Println(err)
// 		os.Exit(1)
// 	}
// }

// func runInteractiveMode() {
// 	var err error
// 	config, err = LoadConfig(configFile)
// 	if err != nil {
// 		log.Fatalf("Error loading config: %v", err)
// 	}

// 	p := tea.NewProgram(initialModel())
// 	if _, err := p.Run(); err != nil {
// 		fmt.Printf("Alas, there's been an error: %v", err)
// 		os.Exit(1)
// 	}
// }

// func runSetup(cmd *cobra.Command, args []string) {
// 	config, err := LoadConfig(configFile)
// 	if err != nil {
// 		log.Fatalf("Error loading config: %v", err)
// 	}
// 	for _, server := range config.Servers {
// 		// err := setupDocker(server)
// 		// if err != nil {
// 		// 	log.Printf("Error setting up Docker on %s: %v", server.Host, err)
// 		// } else {
// 		log.Printf("Successfully set up Docker on %s", server.Host)
// 		// }
// 	}
// }

// func runDeploy(cmd *cobra.Command, args []string) {
// 	config, err := LoadConfig(configFile)
// 	if err != nil {
// 		log.Fatalf("Error loading config: %v", err)
// 	}
// 	for _, server := range config.Servers {
// 		err := deployToServer(config, server)
// 		if err != nil {
// 			log.Printf("Error deploying to %s: %v", server.Host, err)
// 		} else {
// 			log.Printf("Successfully deployed to %s", server.Host)
// 		}
// 	}
// }

// func runEnv(cmd *cobra.Command, args []string) {
// 	config, err := LoadConfig(configFile)
// 	if err != nil {
// 		log.Fatalf("Error loading config: %v", err)
// 	}
// 	for _, server := range config.Servers {
// 		err := syncEnvToServer(config, server)
// 		if err != nil {
// 			log.Printf("Error syncing env to %s: %v", server.Host, err)
// 		} else {
// 			log.Printf("Successfully synced env to %s", server.Host)
// 		}
// 	}
// }

// func runLogs(cmd *cobra.Command, args []string) {
// 	config, err := LoadConfig(configFile)
// 	if err != nil {
// 		log.Fatalf("Error loading config: %v", err)
// 	}
// 	for _, server := range config.Servers {
// 		logs, err := getLogsFromServer(config, server)
// 		if err != nil {
// 			log.Printf("Error getting logs from %s: %v", server.Host, err)
// 		} else {
// 			fmt.Printf("Logs from %s:\n%s\n", server.Host, logs)
// 		}
// 	}
// }

// func runRollback(cmd *cobra.Command, args []string) {
// 	config, err := LoadConfig(configFile)
// 	if err != nil {
// 		log.Fatalf("Error loading config: %v", err)
// 	}
// 	for _, server := range config.Servers {
// 		err := rollbackOnServer(config, server)
// 		if err != nil {
// 			log.Printf("Error rolling back on %s: %v", server.Host, err)
// 		} else {
// 			log.Printf("Successfully rolled back on %s", server.Host)
// 		}
// 	}
// }

// func deployToServer(config *Config, server Server) error {
// 	// Save image to tar file
// 	tarFile := fmt.Sprintf("%s.tar", config.Image)
// 	saveCmd := exec.Command("docker", "save", "-o", tarFile, config.Image)
// 	err := saveCmd.Run()
// 	if err != nil {
// 		return fmt.Errorf("error saving Docker image: %v", err)
// 	}
// 	defer os.Remove(tarFile)

// 	// Copy tar file to server
// 	scpCmd := exec.Command("scp", "-i", server.KeyFile, "-P", fmt.Sprintf("%d", server.Port), tarFile, fmt.Sprintf("%s@%s:/tmp/", server.User, server.Host))
// 	err = scpCmd.Run()
// 	if err != nil {
// 		return fmt.Errorf("error copying image to server: %v", err)
// 	}

// 	// Load image on server and run container
// 	sshCmd := fmt.Sprintf("docker load -i /tmp/%s && docker stop %s || true && docker rm %s || true && docker run -d --name %s %s", tarFile, config.ContainerName, config.ContainerName, config.ContainerName, config.Image)
// 	cmd := exec.Command("ssh", "-i", server.KeyFile, "-p", fmt.Sprintf("%d", server.Port), fmt.Sprintf("%s@%s", server.User, server.Host), sshCmd)
// 	err = cmd.Run()
// 	if err != nil {
// 		return fmt.Errorf("error running container on server: %v", err)
// 	}

// 	return nil
// }

// func syncEnvToServer(config *Config, server Server) error {
// 	scpCmd := exec.Command("scp", "-i", server.KeyFile, "-P", fmt.Sprintf("%d", server.Port), config.EnvFile, fmt.Sprintf("%s@%s:/tmp/.env", server.User, server.Host))
// 	err := scpCmd.Run()
// 	if err != nil {
// 		return fmt.Errorf("error copying env file to server: %v", err)
// 	}

// 	sshCmd := fmt.Sprintf("docker stop %s && docker rm %s && docker run -d --name %s --env-file /tmp/.env %s", config.ContainerName, config.ContainerName, config.ContainerName, config.Image)
// 	cmd := exec.Command("ssh", "-i", server.KeyFile, "-p", fmt.Sprintf("%d", server.Port), fmt.Sprintf("%s@%s", server.User, server.Host), sshCmd)
// 	err = cmd.Run()
// 	if err != nil {
// 		return fmt.Errorf("error updating container with new env: %v", err)
// 	}

// 	return nil
// }

// func getLogsFromServer(config *Config, server Server) (string, error) {
// 	sshCmd := fmt.Sprintf("docker logs %s", config.ContainerName)
// 	cmd := exec.Command("ssh", "-i", server.KeyFile, "-p", fmt.Sprintf("%d", server.Port), fmt.Sprintf("%s@%s", server.User, server.Host), sshCmd)
// 	output, err := cmd.CombinedOutput()
// 	if err != nil {
// 		return "", fmt.Errorf("error getting logs: %v", err)
// 	}

// 	return string(output), nil
// }

// func rollbackOnServer(config *Config, server Server) error {
// 	sshCmd := fmt.Sprintf("docker stop %s && docker rm %s && docker run -d --name %s %s:previous", config.ContainerName, config.ContainerName, config.ContainerName, config.Image)
// 	cmd := exec.Command("ssh", "-i", server.KeyFile, "-p", fmt.Sprintf("%d", server.Port), fmt.Sprintf("%s@%s", server.User, server.Host), sshCmd)
// 	err := cmd.Run()
// 	if err != nil {
// 		return fmt.Errorf("error rolling back: %v", err)
// 	}

// 	return nil
// }
