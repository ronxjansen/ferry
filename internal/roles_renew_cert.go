package ferry

import "context"

type RenewCertificateRole struct{}

func (s *RenewCertificateRole) Description() string {
	return "Force renewal of SSL certificates"
}

func (s *RenewCertificateRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		// Backup current acme.json
		NewTask("cp $HOME/ferry/letsencrypt/acme.json $HOME/ferry/letsencrypt/acme.json.backup"),
		// Touch acme.json to trigger check
		NewTask("touch $HOME/ferry/letsencrypt/acme.json"),
		// Restart Traefik
		NewTask("docker restart traefik"),
	}
}
