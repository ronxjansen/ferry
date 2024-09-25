package ferry

import (
	"fmt"
	"net"
	"os"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func NewSSHClient(server Server) (*ssh.Client, error) {
	authMethod := []ssh.AuthMethod{}

	// Add ssh-agent support
	agentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		logger.Error("Error connecting to ssh-agent")
		panic(err)
	}

	// Add private key support or use ssh-agent
	if server.KeyFile != "" {
		logger.Debug("Using private key for authentication")
		key, err := os.ReadFile(server.KeyFile)
		if err != nil {
			logger.Error("Error reading SSH key file", zap.Error(err))
			return nil, err
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			logger.Error("Error parsing SSH key file", zap.Error(err))
			return nil, err
		}
		authMethod = append(authMethod, ssh.PublicKeys(signer))
	} else {
		logger.Debug("Using ssh-agent for authentication")
		agentClient := agent.NewClient(agentConn)
		authMethod = append(authMethod, ssh.PublicKeysCallback(agentClient.Signers))
	}

	// Connect to the server
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", server.Host, server.Port), &ssh.ClientConfig{
		User:            server.User,
		Auth:            authMethod,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // You should replace this with a proper host key callback
	})
	if err != nil {
		logger.Error("Error connecting to server", zap.Error(err))
		return nil, err
	}

	logger.Info("SSH client created", zap.String("host", server.Host), zap.Int("port", server.Port), zap.String("user", server.User))

	return client, nil
}
